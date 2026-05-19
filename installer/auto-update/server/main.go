// manifestd — minimal iogrid auto-update manifest + binary server.
//
// Serves:
//
//	GET /manifest.json   → signed manifest (cached in-memory; reloaded
//	                       from disk on SIGHUP or every <refresh> seconds)
//	GET /<release>/<bin> → daemon binaries (proxied from local FS or S3)
//	GET /healthz         → 200 OK for the load balancer
//
// Used in CI integration tests to exercise the daemon's polling loop
// end-to-end. In production, the same binary fronts S3 + Cloudflare for
// updates.iogrid.org.
//
// Configuration via env vars (NEVER flags — easier to bake into k8s
// Deployments and `docker run`):
//
//	IOGRID_MANIFEST_PATH   path to manifest.json on disk         (required)
//	IOGRID_BINARY_DIR      directory containing /<release>/<bin> (required)
//	IOGRID_LISTEN          listen addr                           (default :8088)
//	IOGRID_REFRESH_SECS    in-memory cache TTL                   (default 30)
//
// CORS is wide-open (`Access-Control-Allow-Origin: *`) because the
// daemon polls via simple GET — no cookies, no creds — and the web UI
// at /account/updates may also fetch the manifest as a "last published"
// indicator.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func main() {
	manifestPath := mustEnv("IOGRID_MANIFEST_PATH")
	binaryDir := mustEnv("IOGRID_BINARY_DIR")
	listen := envOr("IOGRID_LISTEN", ":8088")
	refresh := time.Duration(envOrInt("IOGRID_REFRESH_SECS", 30)) * time.Second

	srv := newServer(manifestPath, binaryDir, refresh)
	if err := srv.preload(); err != nil {
		log.Fatalf("manifestd preload: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/manifest.json", srv.handleManifest)
	mux.HandleFunc("/healthz", srv.handleHealth)
	mux.HandleFunc("/", srv.handleBinary)

	log.Printf("manifestd listening on %s (manifest=%s, dir=%s, refresh=%s)",
		listen, manifestPath, binaryDir, refresh)
	if err := http.ListenAndServe(listen, withLogging(mux)); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

type server struct {
	manifestPath string
	binaryDir    string
	refresh      time.Duration

	mu          sync.RWMutex
	cached      []byte
	cachedSHA   string
	cachedMtime time.Time
	lastLoad    time.Time
}

func newServer(manifestPath, binaryDir string, refresh time.Duration) *server {
	return &server{
		manifestPath: manifestPath,
		binaryDir:    binaryDir,
		refresh:      refresh,
	}
}

func (s *server) preload() error {
	return s.reload()
}

func (s *server) reload() error {
	f, err := os.Stat(s.manifestPath)
	if err != nil {
		return fmt.Errorf("stat manifest: %w", err)
	}
	body, err := os.ReadFile(s.manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	sum := sha256.Sum256(body)
	s.mu.Lock()
	s.cached = body
	s.cachedSHA = hex.EncodeToString(sum[:])
	s.cachedMtime = f.ModTime()
	s.lastLoad = time.Now()
	s.mu.Unlock()
	return nil
}

func (s *server) maybeReload() {
	s.mu.RLock()
	stale := time.Since(s.lastLoad) > s.refresh
	s.mu.RUnlock()
	if !stale {
		return
	}
	if err := s.reload(); err != nil {
		log.Printf("manifestd reload (using cached): %v", err)
	}
}

func (s *server) handleManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.maybeReload()
	s.mu.RLock()
	body := s.cached
	sum := s.cachedSHA
	mtime := s.cachedMtime
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=30")
	w.Header().Set("ETag", `"`+sum+`"`)
	w.Header().Set("Last-Modified", mtime.UTC().Format(http.TimeFormat))
	if r.Header.Get("If-None-Match") == `"`+sum+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(body)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, "ok\n")
}

// handleBinary serves /<release>/<binary> from binaryDir. The path
// must contain exactly one slash separator between release and binary
// to prevent traversal.
func (s *server) handleBinary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !validReleasePath(path) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	full := filepath.Join(s.binaryDir, filepath.FromSlash(path))
	abs, err := filepath.Abs(full)
	if err != nil {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	rootAbs, err := filepath.Abs(s.binaryDir)
	if err != nil || !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "io error", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "stat error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300, immutable")
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

// validReleasePath enforces "/<release>/<binary>" with no traversal.
// Both segments must be non-empty and free of "..", "/", and "\\".
func validReleasePath(p string) bool {
	parts := strings.Split(p, "/")
	if len(parts) != 2 {
		return false
	}
	for _, seg := range parts {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
		if strings.ContainsAny(seg, "\\") {
			return false
		}
	}
	return true
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &loggingWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(lw, r)
		log.Printf("%s %s %s %d %s",
			r.RemoteAddr, r.Method, r.URL.Path, lw.status, time.Since(start))
	})
}

type loggingWriter struct {
	http.ResponseWriter
	status int
}

func (lw *loggingWriter) WriteHeader(code int) {
	lw.status = code
	lw.ResponseWriter.WriteHeader(code)
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("required env var %s is unset", k)
	}
	return v
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envOrInt(k string, d int) int {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return d
	}
	return n
}
