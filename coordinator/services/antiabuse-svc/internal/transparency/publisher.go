package transparency

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// publishErrors accumulates per-transport failures so we can return a
// joined error without bailing on the first hiccup.
type publishErrors struct {
	errs []error
}

func (e *publishErrors) add(err error) {
	if err != nil {
		e.errs = append(e.errs, err)
	}
}

func (e *publishErrors) any() bool { return len(e.errs) > 0 }

func (e *publishErrors) err() error {
	if len(e.errs) == 0 {
		return nil
	}
	return errors.Join(e.errs...)
}

// Publisher delivers a generated Report to its public consumers.
//
// Production wiring:
//   - S3Uploader writes the canonical JSON + Markdown to an
//     s3://iogrid-transparency/{year}/Q{quarter}.{json,md} key pair.
//   - GatewayBFFURL is POSTed the JSON so /status/transparency/{year}/{quarter}
//     can serve the latest snapshot from in-memory cache.
//
// All transports are best-effort and independent — a single failure
// MUST NOT prevent the others from succeeding. The cronjob's exit code
// is the boolean OR of "every transport reported success", so an
// outage on one channel will fail the job and Argo / k8s will requeue.
type Publisher struct {
	S3            S3Uploader
	BucketName    string
	BFFURL        string
	BFFAuthToken  string
	HTTPClient    *http.Client
}

// S3Uploader is the minimal interface we need from an S3 client. The
// production wiring is github.com/aws/aws-sdk-go-v2/service/s3 (which
// the iogrid platform already pulls in via build-gateway for tart-VM
// artefact upload); unit tests pass a stub.
type S3Uploader interface {
	PutObject(ctx context.Context, bucket, key string, body []byte, contentType string) error
}

// NewPublisher returns a Publisher with a sane default HTTP client.
func NewPublisher(opts Publisher) *Publisher {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &opts
}

// Publish writes the report to every configured transport.
//
// Returns a multi-error joining whatever transports failed. The caller
// should treat any non-nil error as cronjob-failure-worthy.
func (p *Publisher) Publish(ctx context.Context, r *Report) error {
	jsonBody, err := r.JSON()
	if err != nil {
		return fmt.Errorf("transparency publish: JSON marshal: %w", err)
	}
	mdBody := []byte(r.Markdown())

	var errs publishErrors
	if p.S3 != nil && p.BucketName != "" {
		jsonKey := fmt.Sprintf("%d/Q%d.json", r.Year, r.Quarter)
		mdKey := fmt.Sprintf("%d/Q%d.md", r.Year, r.Quarter)
		if err := p.S3.PutObject(ctx, p.BucketName, jsonKey, jsonBody, "application/json"); err != nil {
			errs.add(fmt.Errorf("s3 json: %w", err))
		}
		if err := p.S3.PutObject(ctx, p.BucketName, mdKey, mdBody, "text/markdown; charset=utf-8"); err != nil {
			errs.add(fmt.Errorf("s3 markdown: %w", err))
		}
	}

	if p.BFFURL != "" {
		if err := p.postToBFF(ctx, jsonBody); err != nil {
			errs.add(fmt.Errorf("bff post: %w", err))
		}
	}

	if errs.any() {
		return errs.err()
	}
	return nil
}

// postToBFF sends the JSON payload to gateway-bff so the public
// /status/transparency/{year}/{quarter} endpoint can serve the most
// recent report without an S3 round-trip.
func (p *Publisher) postToBFF(ctx context.Context, body []byte) error {
	target := strings.TrimSuffix(p.BFFURL, "/") + "/api/v1/transparency/publish"
	if _, err := url.Parse(target); err != nil {
		return fmt.Errorf("invalid BFFURL %q: %w", p.BFFURL, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.BFFAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.BFFAuthToken)
	}
	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("gateway-bff publish: status %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}
