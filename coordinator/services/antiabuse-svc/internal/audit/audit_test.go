package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestEmit_NATSLessFallback(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	e, err := New(context.Background(), Options{Logger: logger})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer e.Close()
	err = e.Emit(context.Background(), Event{
		CheckType:  "check_url",
		Target:     "https://example/",
		Decision:   "FILTER_DECISION_BLOCK",
		Reason:     "phishtank_listed",
		ProviderID: "prov-1",
		Timestamp:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	full := buf.String()
	if !strings.Contains(full, "antiabuse_audit") {
		t.Fatalf("expected audit log line in output; got %q", full)
	}
	// Find the line containing the audit event (may be the 2nd line
	// after the "audit emitter using slog fallback" startup log).
	var auditLine string
	for _, line := range strings.Split(full, "\n") {
		if strings.Contains(line, "antiabuse_audit") {
			auditLine = line
			break
		}
	}
	if auditLine == "" {
		t.Fatalf("did not find audit line in: %q", full)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(auditLine), &got); err != nil {
		t.Fatalf("audit line not JSON: %v (line=%q)", err, auditLine)
	}
	if got["check_type"] != "check_url" {
		t.Errorf("check_type = %v, want check_url", got["check_type"])
	}
}

func TestSanitiseSubjectToken(t *testing.T) {
	cases := map[string]string{
		"check_url":       "check_url",
		"CheckURL":        "checkurl",
		"check.url":       "check_url",
		"":                "unknown",
		"weird stuff?":    "weird_stuff_",
		"AbC_123":         "abc_123",
	}
	for in, want := range cases {
		if got := sanitiseSubjectToken(in); got != want {
			t.Errorf("sanitiseSubjectToken(%q) = %q, want %q", in, got, want)
		}
	}
}
