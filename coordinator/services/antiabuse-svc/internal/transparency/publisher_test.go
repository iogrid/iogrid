package transparency

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeS3 captures puts.
type fakeS3 struct {
	mu       sync.Mutex
	puts     []putRecord
	errOnKey string
}

type putRecord struct {
	Bucket      string
	Key         string
	ContentType string
	Body        []byte
}

func (f *fakeS3) PutObject(_ context.Context, bucket, key string, body []byte, contentType string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errOnKey != "" && key == f.errOnKey {
		return errors.New("simulated s3 error")
	}
	f.puts = append(f.puts, putRecord{Bucket: bucket, Key: key, ContentType: contentType, Body: append([]byte(nil), body...)})
	return nil
}

func TestPublish_S3AndBFF(t *testing.T) {
	bff := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/transparency/publish" {
			t.Errorf("BFF path = %q, want /api/v1/transparency/publish", r.URL.Path)
		}
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("bff decode: %v", err)
		}
		if got["year"].(float64) != 2026 {
			t.Errorf("bff payload year mismatch: %v", got["year"])
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer bff.Close()

	s3 := &fakeS3{}
	p := NewPublisher(Publisher{
		S3:         s3,
		BucketName: "iogrid-transparency",
		BFFURL:     bff.URL,
	})

	src := NewInMemory()
	src.SetChecks(100)
	src.SetAuditRetention(AuditRetentionBlock{RequiredDays: 90, ConfiguredDays: 90, Compliant: true})
	g := NewGenerator(src)
	r, err := g.Generate(context.Background(), 2026, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(context.Background(), r); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(s3.puts) != 2 {
		t.Fatalf("expected 2 s3 puts, got %d", len(s3.puts))
	}
	jsonPut, mdPut := s3.puts[0], s3.puts[1]
	if jsonPut.Key != "2026/Q1.json" {
		t.Errorf("json key = %q", jsonPut.Key)
	}
	if mdPut.Key != "2026/Q1.md" {
		t.Errorf("md key = %q", mdPut.Key)
	}
	if jsonPut.ContentType != "application/json" {
		t.Errorf("json content-type = %q", jsonPut.ContentType)
	}
}

func TestPublish_S3FailureSurfacedButOtherTransportRuns(t *testing.T) {
	bffCalled := false
	bff := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bffCalled = true
		w.WriteHeader(http.StatusAccepted)
	}))
	defer bff.Close()
	s3 := &fakeS3{errOnKey: "2026/Q3.json"}
	p := NewPublisher(Publisher{S3: s3, BucketName: "b", BFFURL: bff.URL})
	src := NewInMemory()
	g := NewGenerator(src)
	r, _ := g.Generate(context.Background(), 2026, 3)
	err := p.Publish(context.Background(), r)
	if err == nil {
		t.Fatalf("Publish must return error when S3 fails")
	}
	if !bffCalled {
		t.Errorf("BFF must still be called even after S3 failure")
	}
}

func TestPublish_NoTransports_NoOp(t *testing.T) {
	p := NewPublisher(Publisher{})
	src := NewInMemory()
	g := NewGenerator(src)
	r, _ := g.Generate(context.Background(), 2026, 4)
	if err := p.Publish(context.Background(), r); err != nil {
		t.Errorf("Publish with no transports must be a no-op success, got %v", err)
	}
}

func TestPublish_BFFAuthHeaderForwarded(t *testing.T) {
	var seen string
	bff := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer bff.Close()
	p := NewPublisher(Publisher{BFFURL: bff.URL, BFFAuthToken: "shh"})
	src := NewInMemory()
	g := NewGenerator(src)
	r, _ := g.Generate(context.Background(), 2026, 1)
	if err := p.Publish(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if seen != "Bearer shh" {
		t.Errorf("Authorization = %q, want Bearer shh", seen)
	}
}

func TestPublish_BFF500_ReturnsError(t *testing.T) {
	bff := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusInternalServerError)
	}))
	defer bff.Close()
	p := NewPublisher(Publisher{BFFURL: bff.URL})
	src := NewInMemory()
	g := NewGenerator(src)
	r, _ := g.Generate(context.Background(), 2026, 1)
	if err := p.Publish(context.Background(), r); err == nil {
		t.Errorf("expected error on bff 500")
	}
}
