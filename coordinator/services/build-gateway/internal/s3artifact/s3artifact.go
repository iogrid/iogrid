// Package s3artifact is the build-gateway's artifact-storage abstraction.
//
// Production wires this against Hetzner Object Storage (S3-compatible) with
// per-workspace buckets, server-side encryption (SSE-C or SSE-KMS depending
// on tier), and a 30-day default lifecycle rule.
//
// The Storage interface lets handlers be tested without standing up a real
// S3 backend — the InMemory implementation in this package gives identical
// semantics for unit tests.
package s3artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Object is the minimum surface the gateway needs to expose an artifact to
// a customer download flow.
type Object struct {
	// Bucket is the bucket name we wrote to.
	Bucket string
	// Key is the canonical S3 key for the object.
	Key string
	// Size is the on-S3 length in bytes.
	Size int64
	// ContentType is the MIME type stored on the object.
	ContentType string
}

// Storage is the seam between the gateway and S3.
type Storage interface {
	// EnsureWorkspaceBucket idempotently creates the per-workspace
	// bucket, applying the configured SSE + lifecycle policy. The bucket
	// name is returned so callers can pin it on the Build record.
	EnsureWorkspaceBucket(ctx context.Context, workspaceID string) (string, error)
	// Put uploads body under key in bucket. Provider artifact uploads use
	// this path.
	Put(ctx context.Context, bucket, key, contentType string, body io.Reader) (Object, error)
	// PresignGet returns a time-limited GET URL the customer can use to
	// download the artifact directly from S3. ttl <= 0 means "service
	// default".
	PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
	// SourcePrefixFor returns the canonical S3 prefix where the gateway
	// expects to find the customer-uploaded source tarball for a given
	// build. Used by workloads-svc dispatch payloads.
	SourcePrefixFor(workspaceID, buildID string) (bucket, key string)
	// ArtifactPrefixFor returns the canonical S3 prefix the provider
	// should upload artifacts under. Mirrored into the IosBuildRequest
	// proto sent to workloads-svc.
	ArtifactPrefixFor(workspaceID, buildID string) (bucket, prefix string)
}

// ErrUnknownObject is returned by InMemory.PresignGet when no such object
// has been Put. Production S3 wouldn't 404 here (it'd happily presign a
// missing key) but tests benefit from the stricter signal.
var ErrUnknownObject = errors.New("s3artifact: unknown object")

// --- InMemory implementation ------------------------------------------------

// InMemory is a goroutine-safe Storage backed by a map. Used in unit tests
// and the early stub deployment.
type InMemory struct {
	mu      sync.RWMutex
	objects map[string]inMemObj
	now     func() time.Time
	// urlBase is what InMemory pretends is the S3 endpoint when minting
	// pre-signed URLs. Defaults to https://s3.example.invalid.
	urlBase string
	// defaultTTL applies when PresignGet is called with ttl<=0.
	defaultTTL time.Duration
}

type inMemObj struct {
	bucket      string
	key         string
	contentType string
	size        int64
	data        []byte
}

// NewInMemory returns a fresh InMemory storage. Pass nil for now to get
// time.Now. urlBase is the synthetic endpoint used in pre-signed URLs (e.g.
// "https://fsn1.your-objectstorage.com" in production); leave empty for the
// test default.
func NewInMemory(now func() time.Time, urlBase string) *InMemory {
	if now == nil {
		now = time.Now
	}
	if urlBase == "" {
		urlBase = "https://s3.example.invalid"
	}
	return &InMemory{
		objects:    make(map[string]inMemObj),
		now:        now,
		urlBase:    strings.TrimRight(urlBase, "/"),
		defaultTTL: 15 * time.Minute,
	}
}

// EnsureWorkspaceBucket implements Storage.
func (s *InMemory) EnsureWorkspaceBucket(_ context.Context, workspaceID string) (string, error) {
	if workspaceID == "" {
		return "", errors.New("s3artifact: workspaceID required")
	}
	return WorkspaceBucketName(workspaceID), nil
}

// Put implements Storage.
func (s *InMemory) Put(_ context.Context, bucket, key, contentType string, body io.Reader) (Object, error) {
	if bucket == "" || key == "" {
		return Object{}, errors.New("s3artifact: bucket+key required")
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return Object{}, fmt.Errorf("s3artifact: read body: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[bucket+"/"+key] = inMemObj{
		bucket:      bucket,
		key:         key,
		contentType: contentType,
		size:        int64(len(data)),
		data:        data,
	}
	return Object{
		Bucket:      bucket,
		Key:         key,
		Size:        int64(len(data)),
		ContentType: contentType,
	}, nil
}

// PresignGet implements Storage. The synthetic URL embeds the canonical
// query params real S3 SDKs emit so downstream tooling sees a recognisable
// shape.
func (s *InMemory) PresignGet(_ context.Context, bucket, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = s.defaultTTL
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.objects[bucket+"/"+key]; !ok {
		return "", ErrUnknownObject
	}
	exp := s.now().Add(ttl).Unix()
	u := fmt.Sprintf("%s/%s/%s?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=%d&X-Amz-Signature=stub-%d",
		s.urlBase, url.PathEscape(bucket), url.PathEscape(key), int(ttl.Seconds()), exp)
	return u, nil
}

// SourcePrefixFor implements Storage.
func (s *InMemory) SourcePrefixFor(workspaceID, buildID string) (string, string) {
	bucket := WorkspaceBucketName(workspaceID)
	return bucket, fmt.Sprintf("source/%s/source.tar.gz", buildID)
}

// ArtifactPrefixFor implements Storage.
func (s *InMemory) ArtifactPrefixFor(workspaceID, buildID string) (string, string) {
	bucket := WorkspaceBucketName(workspaceID)
	return bucket, fmt.Sprintf("artifacts/%s/", buildID)
}

// WorkspaceBucketName is the canonical bucket-naming scheme. Public so the
// production S3 adapter and the in-memory adapter use the same value.
//
// Hetzner Object Storage allows lower-case alphanumerics + dashes, 3-63
// chars. Workspace IDs are UUIDs so we strip dashes and prefix with
// "iogrid-build-" to fit comfortably.
func WorkspaceBucketName(workspaceID string) string {
	cleaned := strings.ToLower(strings.ReplaceAll(workspaceID, "-", ""))
	if len(cleaned) > 36 {
		cleaned = cleaned[:36]
	}
	return "iogrid-build-" + cleaned
}
