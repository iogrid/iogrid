package mail

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRenderMagicLink_SignIn(t *testing.T) {
	subject, html, text, err := RenderMagicLink(LinkData{
		URL:       "https://iogrid.org/v1/auth/magic-link/complete?token=abc",
		ExpiresIn: "10 minutes",
		Intent:    "signin",
	})
	if err != nil {
		t.Fatalf("RenderMagicLink: %v", err)
	}
	if !strings.Contains(subject, "iogrid") {
		t.Errorf("subject missing brand: %q", subject)
	}
	if !strings.Contains(html, "Sign in to iogrid") {
		t.Errorf("html missing sign-in line")
	}
	if !strings.Contains(html, "abc") {
		t.Errorf("html missing token URL")
	}
	if !strings.Contains(text, "10 minutes") {
		t.Errorf("text body missing expiry copy")
	}
}

func TestRenderMagicLink_StepUp_HasConfirmCopy(t *testing.T) {
	_, html, text, err := RenderMagicLink(LinkData{
		URL:       "https://example.org/x",
		ExpiresIn: "10 minutes",
		Intent:    "step_up",
	})
	if err != nil {
		t.Fatalf("RenderMagicLink: %v", err)
	}
	if !strings.Contains(html, "privileged operation") {
		t.Errorf("step-up html missing privileged-op copy")
	}
	if !strings.Contains(text, "privileged operation") {
		t.Errorf("step-up text missing privileged-op copy")
	}
}

func TestRenderMergeNotice_HasBothEmails(t *testing.T) {
	subject, html, _, err := RenderMergeNotice(MergeNoticeData{
		MatchedEmail: "alice@company.com",
		PrimaryEmail: "alice@gmail.com",
	})
	if err != nil {
		t.Fatalf("RenderMergeNotice: %v", err)
	}
	if !strings.Contains(subject, "merged") {
		t.Errorf("subject missing 'merged': %q", subject)
	}
	if !strings.Contains(html, "alice@company.com") || !strings.Contains(html, "alice@gmail.com") {
		t.Errorf("html missing both addresses: %q", html)
	}
}

func TestMemorySender_CapturesAndContains(t *testing.T) {
	s := &MemorySender{}
	if err := s.Send(context.Background(), Message{To: "a@a.a", Subject: "hi", HTMLBody: "<b>hi</b>", TextBody: "hi"}); err != nil {
		t.Fatal(err)
	}
	if s.LastTo() != "a@a.a" {
		t.Errorf("LastTo: %q", s.LastTo())
	}
	if !s.LastContains("hi") {
		t.Errorf("LastContains hi: false")
	}
	if s.LastContains("nope") {
		t.Errorf("LastContains nope: true")
	}
}

func TestMemorySender_PropagatesFailure(t *testing.T) {
	s := &MemorySender{Fail: errors.New("smtp down")}
	if err := s.Send(context.Background(), Message{To: "a@a.a"}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestNewSMTP_RejectsMissingFrom(t *testing.T) {
	_, err := NewSMTP(Config{Host: "x", Port: 25})
	if err == nil {
		t.Fatalf("expected error when From is empty")
	}
}

func TestNewSMTP_RejectsBadFrom(t *testing.T) {
	_, err := NewSMTP(Config{Host: "x", Port: 25, From: "not-an-email"})
	if err == nil {
		t.Fatalf("expected error when From malformed")
	}
}

func TestNewSMTP_AcceptsValidConfig(t *testing.T) {
	s, err := NewSMTP(Config{Host: "mail.example.org", Port: 587, From: "no-reply@example.org", FromName: "Example", StartTLS: true})
	if err != nil {
		t.Fatalf("NewSMTP: %v", err)
	}
	if s == nil {
		t.Fatalf("nil sender")
	}
}
