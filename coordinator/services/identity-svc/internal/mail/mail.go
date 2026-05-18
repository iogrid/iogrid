// Package mail sends transactional email via Stalwart SMTP. The whole
// surface is one interface (Sender.Send) so tests substitute a mock and
// production wires the real STARTTLS transport.
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	textTemplate "text/template"
	"time"
)

// Message is the payload Sender accepts.
type Message struct {
	To       string
	Subject  string
	HTMLBody string
	TextBody string
}

// Sender abstracts the SMTP transport so unit tests don't need a network
// connection. The production type is SMTPSender.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// Config configures the SMTP transport.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
	StartTLS bool
	// Timeout caps the total dial+handshake+send cost. Defaults to 10s.
	Timeout time.Duration
}

// SMTPSender talks to a real SMTP server. Compatible with Stalwart at
// mail.openova.io:587 (STARTTLS) but the wire format is plain SMTP so any
// MTA works for local dev (mailhog, mailpit).
type SMTPSender struct {
	cfg Config
}

// NewSMTP constructs a sender. Validate() ensures the From address parses
// as a valid RFC 5322 mailbox; we surface bad config early.
func NewSMTP(cfg Config) (*SMTPSender, error) {
	if cfg.Host == "" {
		return nil, errors.New("mail: SMTP host required")
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	if cfg.From == "" {
		return nil, errors.New("mail: SMTP from address required")
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return nil, fmt.Errorf("mail: parse from %q: %w", cfg.From, err)
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &SMTPSender{cfg: cfg}, nil
}

// Send dials the SMTP server, optionally STARTTLS-upgrades, and submits
// the message. Stalwart's internal-route path requires no AUTH; we keep
// PLAIN AUTH wired for the case where SMTP_USERNAME is set.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if msg.To == "" {
		return errors.New("mail: empty To address")
	}
	if _, err := mail.ParseAddress(msg.To); err != nil {
		return fmt.Errorf("mail: parse to %q: %w", msg.To, err)
	}

	addr := net.JoinHostPort(s.cfg.Host, fmt.Sprintf("%d", s.cfg.Port))
	dialer := &net.Dialer{Timeout: s.cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("mail: dial %s: %w", addr, err)
	}
	defer conn.Close()

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fmt.Errorf("mail: smtp client: %w", err)
	}
	defer func() { _ = c.Quit() }()

	if s.cfg.StartTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return fmt.Errorf("mail: starttls: %w", err)
			}
		}
	}

	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("mail: smtp auth: %w", err)
		}
	}

	fromAddr, _ := mail.ParseAddress(s.cfg.From)
	if err := c.Mail(fromAddr.Address); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	toAddr, _ := mail.ParseAddress(msg.To)
	if err := c.Rcpt(toAddr.Address); err != nil {
		return fmt.Errorf("mail: RCPT TO: %w", err)
	}

	wr, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	body, err := buildMIME(s.cfg, msg)
	if err != nil {
		return err
	}
	if _, err := wr.Write(body); err != nil {
		return fmt.Errorf("mail: write body: %w", err)
	}
	if err := wr.Close(); err != nil {
		return fmt.Errorf("mail: close DATA: %w", err)
	}
	return nil
}

// buildMIME returns a multipart/alternative payload with both plain-text
// and HTML parts. Boundary is fixed-length-random per message so even a
// pathological MTA cannot collapse parts.
func buildMIME(cfg Config, msg Message) ([]byte, error) {
	boundary := fmt.Sprintf("iogrid-%d", time.Now().UnixNano())
	from := cfg.From
	if cfg.FromName != "" {
		from = fmt.Sprintf("%q <%s>", cfg.FromName, cfg.From)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", msg.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", msg.Subject)
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary)
	fmt.Fprintf(&b, "\r\n")

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
	b.WriteString(msg.TextBody)
	fmt.Fprintf(&b, "\r\n")

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/html; charset=\"utf-8\"\r\n\r\n")
	b.WriteString(msg.HTMLBody)
	fmt.Fprintf(&b, "\r\n")

	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes(), nil
}

// --- template rendering ---------------------------------------------------

// LinkData is what the magic-link template needs.
type LinkData struct {
	URL          string
	ExpiresIn    string // human-readable, e.g. "10 minutes"
	Intent       string // signin | step_up | merge
	BrandName    string // "iogrid"
	SupportEmail string
}

const magicLinkHTML = `<!doctype html>
<html>
  <body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#f5f5f7;margin:0;padding:24px;color:#111;">
    <div style="max-width:480px;margin:0 auto;background:#fff;border-radius:12px;padding:32px;">
      <h1 style="margin:0 0 16px 0;font-size:22px;">Sign in to {{.BrandName}}</h1>
      {{if eq .Intent "step_up"}}
      <p>You just initiated a privileged operation (payout change, identity merge or account deletion). To continue, confirm it's really you by clicking the link below.</p>
      {{else if eq .Intent "merge"}}
      <p>You're about to merge this email into your existing {{.BrandName}} account. Click the link below to confirm.</p>
      {{else}}
      <p>Click the link below to sign in. The link expires in {{.ExpiresIn}}.</p>
      {{end}}
      <p style="margin:24px 0;">
        <a href="{{.URL}}" style="display:inline-block;padding:12px 18px;background:#111;color:#fff;border-radius:8px;text-decoration:none;font-weight:600;">Sign in to {{.BrandName}}</a>
      </p>
      <p style="font-size:13px;color:#666;">If you didn't request this, you can ignore this email — nothing will happen.</p>
      <p style="font-size:13px;color:#666;">Need help? <a href="mailto:{{.SupportEmail}}">{{.SupportEmail}}</a></p>
    </div>
  </body>
</html>`

const magicLinkText = `Sign in to {{.BrandName}}

{{if eq .Intent "step_up"}}You just initiated a privileged operation. To continue, open this link:
{{else if eq .Intent "merge"}}You're about to merge this email into your existing {{.BrandName}} account. Open this link to confirm:
{{else}}Click this link to sign in. It expires in {{.ExpiresIn}}.
{{end}}

  {{.URL}}

If you didn't request this, you can ignore this email — nothing will happen.
Need help? {{.SupportEmail}}
`

// RenderMagicLink renders the standard sign-in / step-up / merge email.
func RenderMagicLink(d LinkData) (subject, htmlBody, textBody string, err error) {
	if d.BrandName == "" {
		d.BrandName = "iogrid"
	}
	if d.SupportEmail == "" {
		d.SupportEmail = "support@iogrid.org"
	}
	var hb, tb bytes.Buffer
	if t, err := template.New("h").Parse(magicLinkHTML); err == nil {
		_ = t.Execute(&hb, d)
	} else {
		return "", "", "", err
	}
	if t, err := textTemplate.New("t").Parse(magicLinkText); err == nil {
		_ = t.Execute(&tb, d)
	} else {
		return "", "", "", err
	}
	switch d.Intent {
	case "step_up":
		subject = fmt.Sprintf("Confirm your %s action", d.BrandName)
	case "merge":
		subject = fmt.Sprintf("Confirm merging this email into your %s account", d.BrandName)
	default:
		subject = fmt.Sprintf("Your %s sign-in link", d.BrandName)
	}
	return subject, hb.String(), tb.String(), nil
}

// MergeNoticeData captures the values used in the "your account was
// merged" notification sent to both old + new email after auto-merge.
type MergeNoticeData struct {
	BrandName     string
	OtherEmail    string
	MatchedEmail  string
	PrimaryEmail  string
	SupportEmail  string
}

const mergeNoticeHTML = `<!doctype html>
<html>
  <body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#f5f5f7;margin:0;padding:24px;color:#111;">
    <div style="max-width:480px;margin:0 auto;background:#fff;border-radius:12px;padding:32px;">
      <h1 style="margin:0 0 16px 0;font-size:22px;">Your {{.BrandName}} accounts were merged</h1>
      <p>We detected that <strong>{{.MatchedEmail}}</strong> is a verified email on your Google account. To keep things simple, we merged it into your existing {{.BrandName}} account.</p>
      <p>Your primary email is now <strong>{{.PrimaryEmail}}</strong>. You can sign in with either email — same account.</p>
      <p style="font-size:13px;color:#666;">If you didn't expect this, contact <a href="mailto:{{.SupportEmail}}">{{.SupportEmail}}</a> immediately.</p>
    </div>
  </body>
</html>`

const mergeNoticeText = `Your {{.BrandName}} accounts were merged.

We detected that {{.MatchedEmail}} is a verified email on your Google account.
We merged it into your existing {{.BrandName}} account so you don't need to
juggle two profiles. Your primary email is now {{.PrimaryEmail}}.

If you didn't expect this, contact {{.SupportEmail}} immediately.
`

// RenderMergeNotice renders the post-merge notification email.
func RenderMergeNotice(d MergeNoticeData) (subject, htmlBody, textBody string, err error) {
	if d.BrandName == "" {
		d.BrandName = "iogrid"
	}
	if d.SupportEmail == "" {
		d.SupportEmail = "support@iogrid.org"
	}
	var hb, tb bytes.Buffer
	if t, err := template.New("h").Parse(mergeNoticeHTML); err == nil {
		_ = t.Execute(&hb, d)
	} else {
		return "", "", "", err
	}
	if t, err := textTemplate.New("t").Parse(mergeNoticeText); err == nil {
		_ = t.Execute(&tb, d)
	} else {
		return "", "", "", err
	}
	subject = fmt.Sprintf("Your %s accounts were merged", d.BrandName)
	return subject, hb.String(), tb.String(), nil
}

// --- in-memory sender (tests) --------------------------------------------

// MemorySender captures every Send call in memory. Used by unit tests.
// The Inbox slice retains messages in arrival order.
type MemorySender struct {
	Inbox []Message
	Fail  error
}

// Send implements Sender.
func (m *MemorySender) Send(_ context.Context, msg Message) error {
	if m.Fail != nil {
		return m.Fail
	}
	m.Inbox = append(m.Inbox, msg)
	return nil
}

// LastTo returns the To address of the most recently sent message, or
// "" when the inbox is empty. Convenience for tests.
func (m *MemorySender) LastTo() string {
	if len(m.Inbox) == 0 {
		return ""
	}
	return m.Inbox[len(m.Inbox)-1].To
}

// LastContains reports whether the most recent message's text or HTML
// body contains the substring. Used to assert link inclusion.
func (m *MemorySender) LastContains(needle string) bool {
	if len(m.Inbox) == 0 {
		return false
	}
	last := m.Inbox[len(m.Inbox)-1]
	return strings.Contains(last.TextBody, needle) || strings.Contains(last.HTMLBody, needle)
}
