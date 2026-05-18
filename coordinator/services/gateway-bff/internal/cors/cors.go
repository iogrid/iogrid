// Package cors emits the headers the Next.js management plane needs
// to talk to the gateway-bff from a browser.
//
// We intentionally implement this in-tree rather than pulling in a
// dependency: the rules are short and the wrong dependency picks up
// "Allow-Origin: *" by default, which is a security regression for a
// credentials-included API.
package cors

import (
	"net/http"
	"strings"
)

// Options configures the middleware.
type Options struct {
	// AllowedOrigins is the exact origin allowlist (scheme+host[:port]).
	// Wildcards are NOT supported — we want explicit choices.
	AllowedOrigins []string
	// AllowedMethods defaults to all standard verbs the BFF exposes.
	AllowedMethods []string
	// AllowedHeaders defaults to the headers our handlers read.
	AllowedHeaders []string
	// ExposedHeaders are returned in Access-Control-Expose-Headers.
	ExposedHeaders []string
	// MaxAge for preflight cache. Defaults to 600 (10 min).
	MaxAgeSeconds int
}

// Middleware applies CORS to every request. Preflight (OPTIONS) is
// short-circuited; non-preflight requests pass through to the next
// handler but with the relevant Access-Control-* headers attached.
func Middleware(opts Options) func(http.Handler) http.Handler {
	if len(opts.AllowedMethods) == 0 {
		opts.AllowedMethods = []string{
			http.MethodGet, http.MethodHead, http.MethodPost,
			http.MethodPut, http.MethodPatch, http.MethodDelete,
			http.MethodOptions,
		}
	}
	if len(opts.AllowedHeaders) == 0 {
		opts.AllowedHeaders = []string{"Authorization", "Content-Type", "X-Requested-With", "Last-Event-ID"}
	}
	if opts.MaxAgeSeconds == 0 {
		opts.MaxAgeSeconds = 600
	}

	allowed := map[string]struct{}{}
	for _, o := range opts.AllowedOrigins {
		allowed[strings.TrimSpace(o)] = struct{}{}
	}
	methods := strings.Join(opts.AllowedMethods, ", ")
	headers := strings.Join(opts.AllowedHeaders, ", ")
	expose := strings.Join(opts.ExposedHeaders, ", ")
	maxAge := strings.Trim(strings.TrimSpace(itoa(opts.MaxAgeSeconds)), " ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				if _, ok := allowed[origin]; ok {
					h := w.Header()
					h.Set("Access-Control-Allow-Origin", origin)
					h.Set("Access-Control-Allow-Credentials", "true")
					h.Set("Vary", "Origin")
					if expose != "" {
						h.Set("Access-Control-Expose-Headers", expose)
					}
				}
			}
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				if _, ok := allowed[origin]; ok {
					h := w.Header()
					h.Set("Access-Control-Allow-Methods", methods)
					h.Set("Access-Control-Allow-Headers", headers)
					h.Set("Access-Control-Max-Age", maxAge)
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// itoa is the smallest possible int-to-string we need; avoids importing
// strconv solely for one call site.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	p := len(buf)
	for i > 0 {
		p--
		buf[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		buf[p] = '-'
	}
	return string(buf[p:])
}
