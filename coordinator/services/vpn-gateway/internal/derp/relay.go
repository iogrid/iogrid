// Package derp implements a DERP-style encrypted-WG-packet relay —
// Phase-4 fallback for customer↔provider pairs that can't establish a
// direct ICE path (typically: both endpoints behind symmetric NATs,
// double-CGNAT carrier setups, or dual-stack mismatches).
//
// What it is + what it isn't
// --------------------------
// The relay is an OPAQUE forwarder. It never decrypts WG payloads — it
// just routes encrypted datagrams between two endpoints registered
// with their static WG public keys. A relay operator with full root
// on the relay box sees ciphertext + traffic timing, never plaintext.
// This is the same threat model Tailscale DERP runs under.
//
// Why not coturn / TURN
// ---------------------
// TURN (RFC 5766) is conceptually similar but the protocol surface is
// 10× larger (allocation lifetimes, permissions, channel data
// framing) and the auth model is username/password with replay
// windows that don't compose cleanly with our Coordinator-issued
// session credentials. A purpose-built relay matched to our wire
// format is ~10% the code surface and avoids dragging coturn into
// the deploy graph.
//
// Why now
// -------
// Phase-4 hardening — issue #521. The Phase-1 ICE flow handles the
// majority of residential NAT configurations directly. Customers that
// fall through (predominantly symmetric-NAT-both-sides) currently see
// "session failed" with no recourse; this module is the fallback that
// makes the session succeed at the cost of latency + relay
// bandwidth-bill. The coordinator decides when to use it (via a
// future flag returned in /v1/vpn/regions); this package provides the
// data-plane piece.
//
// Wire protocol
// -------------
// Length-prefixed binary framing over TLS-terminated TCP. One frame:
//
//   ┌────────┬─────────────────────────┬──────────────┐
//   │ kind   │ 32-byte WG static pubkey│ payload      │
//   │ 1 byte │ peer (to/from)          │ ≤1500 bytes  │
//   └────────┴─────────────────────────┴──────────────┘
//
//   kind = 0x01 → REGISTER  (initial frame; payload is empty;
//                            peer field = sender's own static pubkey)
//   kind = 0x02 → DATA      (forward `payload` to `peer`)
//   kind = 0x03 → PEER_GONE (server→client; peer disconnected;
//                            payload is empty)
//
// All framing is little-endian 16-bit length-prefixed on the wire
// for streaming-friendly parsing. Frames larger than 2 KiB are
// rejected to keep the per-connection buffer small.
//
// Concurrency model
// -----------------
// One goroutine per connection (read side). The registry's RWMutex
// guards a `map[[32]byte]*conn` keyed by peer pubkey. Writes are
// serialised per-conn via `conn.writeMu`. Lookup-then-write is
// holding the registry RLock + the destination's writeMu — this is
// safe because lock order is fixed (registry → conn) and no caller
// takes them in the opposite order.

package derp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
)

const (
	// MaxFrameBytes caps the per-frame payload size. WG datagrams are
	// at most 1500 bytes minus the WG header overhead; 2 KiB is a
	// safe ceiling that catches malformed clients without rejecting
	// legitimate fragmented IPv6 jumbograms (rare on residential).
	MaxFrameBytes = 2048

	// HeaderBytes — kind (1) + pubkey (32) + payload-length (2).
	HeaderBytes = 1 + 32 + 2
)

// Frame kinds.
const (
	KindRegister byte = 0x01
	KindData     byte = 0x02
	KindPeerGone byte = 0x03
)

// ErrPeerNotConnected is returned when a DATA frame targets a peer
// pubkey that isn't currently registered with this relay. Callers
// surface this to the sender so it can retry against a different
// relay or drop back to "session unreachable" reporting.
var ErrPeerNotConnected = errors.New("derp: peer not connected to relay")

// PeerKey is a WG static-public-key as bytes. Equality semantics let
// us use it directly as a map key (32 fixed bytes).
type PeerKey [32]byte

// Stats is the relay's lifetime counters; exposed via /metrics.
type Stats struct {
	// PeersConnected is the current live registration count.
	PeersConnected atomic.Int64
	// FramesForwarded counts successful DATA-frame forwards.
	FramesForwarded atomic.Uint64
	// FramesDropped counts DATA frames dropped because the destination
	// peer wasn't connected.
	FramesDropped atomic.Uint64
	// BytesForwarded counts payload bytes (not headers) successfully
	// forwarded — used to bill relay traffic against the originator
	// in the future.
	BytesForwarded atomic.Uint64
}

// Relay is the server-side state. One Relay per process; concurrent
// AcceptConn calls are safe.
type Relay struct {
	logger *slog.Logger

	mu      sync.RWMutex
	peers   map[PeerKey]*conn
	stats   Stats
	closing atomic.Bool
}

// New returns an empty Relay ready to accept connections.
func New(logger *slog.Logger) *Relay {
	if logger == nil {
		logger = slog.Default()
	}
	return &Relay{
		logger: logger,
		peers:  make(map[PeerKey]*conn),
	}
}

// StatsSnapshot returns a copy of the live counters. Cheap; called by
// /metrics on each scrape.
func (r *Relay) StatsSnapshot() (peers int64, forwarded uint64, dropped uint64, bytes uint64) {
	return r.stats.PeersConnected.Load(),
		r.stats.FramesForwarded.Load(),
		r.stats.FramesDropped.Load(),
		r.stats.BytesForwarded.Load()
}

// AcceptConn handles one client connection lifecycle. Blocks until the
// connection is closed or `ctx` is cancelled. Caller is responsible
// for terminating TLS before handing the net.Conn in.
//
// The function does its own goroutine bookkeeping — fire-and-forget
// from a TCP listener's Accept loop.
func (r *Relay) AcceptConn(ctx context.Context, nc net.Conn) {
	defer nc.Close()
	c := &conn{
		nc:     nc,
		logger: r.logger,
	}

	// Wait for the REGISTER frame before doing anything else. Clients
	// that send DATA before REGISTER are misbehaving.
	hdr, payload, err := readFrame(nc)
	if err != nil {
		r.logger.Debug("derp: read first frame", slog.String("error", err.Error()))
		return
	}
	if hdr.Kind != KindRegister {
		r.logger.Debug("derp: first frame was not REGISTER", slog.Int("kind", int(hdr.Kind)))
		return
	}
	if len(payload) != 0 {
		r.logger.Debug("derp: REGISTER payload non-empty (rejected)")
		return
	}
	c.peerKey = hdr.Peer

	if !r.register(c) {
		// Same pubkey already connected — reject to keep the routing
		// table 1:1. The earlier connection wins; the new one closes.
		r.logger.Debug("derp: REGISTER duplicate", slog.String("peer", peerKeyString(c.peerKey)))
		return
	}
	r.stats.PeersConnected.Add(1)
	r.logger.Info("derp: peer registered", slog.String("peer", peerKeyString(c.peerKey)))

	defer func() {
		r.unregister(c)
		r.stats.PeersConnected.Add(-1)
		r.logger.Info("derp: peer disconnected", slog.String("peer", peerKeyString(c.peerKey)))
	}()

	for {
		// Per-frame context check — keeps shutdown latency bounded.
		if r.closing.Load() {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}

		hdr, payload, err := readFrame(nc)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				r.logger.Debug("derp: read frame",
					slog.String("peer", peerKeyString(c.peerKey)),
					slog.String("error", err.Error()))
			}
			return
		}

		switch hdr.Kind {
		case KindData:
			if len(payload) == 0 {
				continue // empty DATA frames are no-ops; don't disconnect
			}
			if err := r.forward(hdr.Peer, payload); err != nil {
				r.stats.FramesDropped.Add(1)
				// Tell the sender the peer is gone so it can stop
				// flooding the relay. Best-effort — if the write
				// itself fails the next iteration will see the
				// connection EOF and unregister.
				goneFrame := encodeFrame(KindPeerGone, hdr.Peer, nil)
				c.writeMu.Lock()
				_, _ = nc.Write(goneFrame)
				c.writeMu.Unlock()
				continue
			}
			r.stats.FramesForwarded.Add(1)
			r.stats.BytesForwarded.Add(uint64(len(payload)))
		case KindRegister:
			// Already registered — second REGISTER is a protocol
			// error. Drop the connection.
			return
		default:
			// Unknown kind — log and continue rather than disconnect,
			// so future protocol extensions can roll out without
			// breaking older relays.
			r.logger.Debug("derp: unknown kind", slog.Int("kind", int(hdr.Kind)))
		}
	}
}

// register inserts c into the peer map. Returns false if the peer key
// is already claimed by a live connection.
func (r *Relay) register(c *conn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.peers[c.peerKey]; exists {
		return false
	}
	r.peers[c.peerKey] = c
	return true
}

// unregister removes c IFF the current registration is c (not a later
// reconnection of the same peer key — that one stays).
func (r *Relay) unregister(c *conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.peers[c.peerKey]
	if ok && cur == c {
		delete(r.peers, c.peerKey)
	}
}

// forward writes a DATA frame to the destination peer. Returns
// ErrPeerNotConnected if no such peer is currently registered.
func (r *Relay) forward(to PeerKey, payload []byte) error {
	r.mu.RLock()
	dst, ok := r.peers[to]
	r.mu.RUnlock()
	if !ok {
		return ErrPeerNotConnected
	}
	frame := encodeFrame(KindData, to, payload)
	dst.writeMu.Lock()
	defer dst.writeMu.Unlock()
	_, err := dst.nc.Write(frame)
	return err
}

// Close marks the relay as draining. Existing AcceptConn loops will
// exit at their next per-frame check.
func (r *Relay) Close() {
	r.closing.Store(true)
}

// conn is the per-client state. Created on connect, lives until
// AcceptConn returns.
type conn struct {
	nc      net.Conn
	logger  *slog.Logger
	peerKey PeerKey
	writeMu sync.Mutex
}

// frameHeader is the parsed prefix of one wire frame.
type frameHeader struct {
	Kind       byte
	Peer       PeerKey
	PayloadLen uint16
}

// readFrame reads one length-prefixed frame from r. Returns io.EOF on
// clean close + a wrapped error on malformed input.
func readFrame(r io.Reader) (frameHeader, []byte, error) {
	var hdrBytes [HeaderBytes]byte
	if _, err := io.ReadFull(r, hdrBytes[:]); err != nil {
		return frameHeader{}, nil, err
	}
	hdr := frameHeader{
		Kind:       hdrBytes[0],
		PayloadLen: binary.LittleEndian.Uint16(hdrBytes[33:35]),
	}
	copy(hdr.Peer[:], hdrBytes[1:33])
	if hdr.PayloadLen > MaxFrameBytes {
		return hdr, nil, fmt.Errorf("frame payload %d exceeds max %d", hdr.PayloadLen, MaxFrameBytes)
	}
	if hdr.PayloadLen == 0 {
		return hdr, nil, nil
	}
	payload := make([]byte, hdr.PayloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return hdr, nil, err
	}
	return hdr, payload, nil
}

// encodeFrame builds a wire frame. `peer` semantics depend on `kind`:
// for REGISTER it's the sender's own pubkey; for DATA it's the
// destination; for PEER_GONE it's the peer that just disconnected.
func encodeFrame(kind byte, peer PeerKey, payload []byte) []byte {
	out := make([]byte, HeaderBytes+len(payload))
	out[0] = kind
	copy(out[1:33], peer[:])
	binary.LittleEndian.PutUint16(out[33:35], uint16(len(payload)))
	if len(payload) > 0 {
		copy(out[HeaderBytes:], payload)
	}
	return out
}

// peerKeyString returns a short prefix of the base64 form for logs.
// Full pubkeys clutter log lines; the first 8 chars uniquely identify
// a peer in a deployment with < ~100k registrations.
func peerKeyString(k PeerKey) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	// 8 chars = 6 raw bytes ≈ 48 bits of entropy, sufficient.
	var out [8]byte
	for i := 0; i < 8; i++ {
		// Pick byte i*4 from the key, map into the 64-char alphabet.
		idx := int(k[i*4%32]) & 0x3f
		out[i] = charset[idx]
	}
	return string(out[:])
}
