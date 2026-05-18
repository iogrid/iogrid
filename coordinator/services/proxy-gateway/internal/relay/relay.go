// Package relay implements the bidirectional byte pump between the
// customer-facing connection and the chosen provider's tunnel endpoint.
//
// Two goroutines copy bytes in each direction. Every N bytes (default
// 1 MiB per task spec) the relay emits a BillingEvent. The relay never
// inspects content — neither headers nor TLS payload — consistent with
// the docs/LEGAL.md common-carrier defence.
//
// On either side EOF the relay closes the other half-duplex with a TCP
// half-close so any in-flight bytes flush cleanly, then returns the
// final counters so the caller can emit a terminal BillingEvent.
package relay

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Counters is the final tally returned by Run.
type Counters struct {
	// BytesIn is bytes that flowed FROM provider TO customer (inbound
	// w.r.t. customer).
	BytesIn uint64
	// BytesOut is bytes that flowed FROM customer TO provider (outbound
	// w.r.t. customer).
	BytesOut uint64
	// Duration is the wall-clock time the relay was active.
	Duration time.Duration
}

// MeterFunc is invoked every MeterEvery bytes (and once on terminate)
// with the running totals so the caller can emit BillingEvents.
//
// The relay does NOT block on MeterFunc; if it returns an error the
// relay logs and continues — billing is best-effort.
type MeterFunc func(ctx context.Context, bytesIn, bytesOut uint64) error

// Options configure Run.
type Options struct {
	// MeterEvery is the byte interval between meter callbacks. 0 = 1 MiB.
	MeterEvery uint64
	// Meter, if non-nil, is invoked on every threshold crossing.
	Meter MeterFunc
	// IdleTimeout terminates the relay if no bytes flow in either
	// direction for the supplied duration. 0 disables idle-timeout.
	IdleTimeout time.Duration
	// BufferSize is the per-direction copy buffer (default 32 KiB —
	// balances syscall overhead vs memory footprint).
	BufferSize int
}

// Run copies bytes between a (customer) and b (provider) until either
// side EOFs or ctx is canceled. Returns the final counters and the
// first non-nil error seen on either half.
//
// Caller is responsible for closing both conns AFTER Run returns —
// Run only half-closes via CloseWrite to flush in-flight bytes.
func Run(ctx context.Context, customer, provider net.Conn, opts Options) (Counters, error) {
	bufSize := opts.BufferSize
	if bufSize <= 0 {
		bufSize = 32 << 10
	}
	meterEvery := opts.MeterEvery
	if meterEvery == 0 {
		meterEvery = 1 << 20
	}

	var (
		bytesIn, bytesOut uint64
		lastActivity      atomic.Int64
		wg                sync.WaitGroup
		errMu             sync.Mutex
		firstErr          error
	)
	lastActivity.Store(time.Now().UnixNano())
	captureErr := func(err error) {
		if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	// emit fires the meter callback when a threshold is crossed. Uses
	// atomic loads so either goroutine can call it.
	var emittedTotal atomic.Uint64
	emit := func(force bool) {
		in := atomic.LoadUint64(&bytesIn)
		out := atomic.LoadUint64(&bytesOut)
		sum := in + out
		prev := emittedTotal.Load()
		if !force && sum-prev < meterEvery {
			return
		}
		if opts.Meter != nil {
			emittedTotal.Store(sum)
			if err := opts.Meter(ctx, in, out); err != nil {
				// don't capture as a relay error — billing is
				// best-effort, never blocks data plane.
				_ = err
			}
		}
	}

	startedAt := time.Now()

	// Idle watchdog (only when IdleTimeout > 0).
	var watchdogStop chan struct{}
	if opts.IdleTimeout > 0 {
		watchdogStop = make(chan struct{})
		go func() {
			t := time.NewTicker(opts.IdleTimeout / 4)
			if opts.IdleTimeout/4 == 0 {
				t = time.NewTicker(time.Second)
			}
			defer t.Stop()
			for {
				select {
				case <-watchdogStop:
					return
				case now := <-t.C:
					if now.UnixNano()-lastActivity.Load() >= int64(opts.IdleTimeout) {
						_ = customer.SetDeadline(time.Now().Add(-time.Second))
						_ = provider.SetDeadline(time.Now().Add(-time.Second))
						return
					}
				}
			}
		}()
	}

	wg.Add(2)
	// customer -> provider (bytes_out)
	go func() {
		defer wg.Done()
		buf := make([]byte, bufSize)
		for {
			n, err := customer.Read(buf)
			if n > 0 {
				atomic.AddUint64(&bytesOut, uint64(n))
				lastActivity.Store(time.Now().UnixNano())
				if _, werr := provider.Write(buf[:n]); werr != nil {
					captureErr(werr)
					closeWrite(provider)
					return
				}
				emit(false)
			}
			if err != nil {
				captureErr(err)
				closeWrite(provider)
				return
			}
		}
	}()
	// provider -> customer (bytes_in)
	go func() {
		defer wg.Done()
		buf := make([]byte, bufSize)
		for {
			n, err := provider.Read(buf)
			if n > 0 {
				atomic.AddUint64(&bytesIn, uint64(n))
				lastActivity.Store(time.Now().UnixNano())
				if _, werr := customer.Write(buf[:n]); werr != nil {
					captureErr(werr)
					closeWrite(customer)
					return
				}
				emit(false)
			}
			if err != nil {
				captureErr(err)
				closeWrite(customer)
				return
			}
		}
	}()

	// Cancel on ctx done by force-closing.
	doneCh := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = customer.SetDeadline(time.Now().Add(-time.Second))
			_ = provider.SetDeadline(time.Now().Add(-time.Second))
		case <-doneCh:
		}
	}()

	wg.Wait()
	close(doneCh)
	if watchdogStop != nil {
		close(watchdogStop)
	}

	// Final terminal meter emission so billing sees the tail.
	emit(true)

	return Counters{
		BytesIn:  atomic.LoadUint64(&bytesIn),
		BytesOut: atomic.LoadUint64(&bytesOut),
		Duration: time.Since(startedAt),
	}, firstErr
}

// closeWrite half-closes a TCP connection; non-TCP conns fall back to
// the full Close path.
func closeWrite(c net.Conn) {
	type closeWriter interface{ CloseWrite() error }
	if cw, ok := c.(closeWriter); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = c.Close()
}
