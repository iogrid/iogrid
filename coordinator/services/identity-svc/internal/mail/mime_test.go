package mail

import (
	"bufio"
	"context"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func TestBuildMIME_ContainsExpectedHeaders(t *testing.T) {
	cfg := Config{From: "no-reply@iogrid.org", FromName: "iogrid"}
	body, err := buildMIME(cfg, Message{To: "alice@example.com", Subject: "hi", HTMLBody: "<b>hi</b>", TextBody: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"From:", "To: alice@example.com", "Subject: hi",
		"MIME-Version: 1.0", "multipart/alternative",
		"text/plain", "text/html", "<b>hi</b>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("body missing %q\n--\n%s", want, s)
		}
	}
}

func TestBuildMIME_BoundaryClosesCorrectly(t *testing.T) {
	body, err := buildMIME(Config{From: "x@x.x"}, Message{To: "y@y.y"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	// Final boundary terminator is --<boundary>--
	if !strings.Contains(s, "--\r\n") {
		t.Errorf("missing closing terminator: %s", s)
	}
}

// TestSMTPSender_Send_RoundTripWithFakeServer pretends to be an SMTP
// server (just enough commands to satisfy net/smtp), captures the message
// the sender wrote, and asserts it round-trips.
func TestSMTPSender_Send_RoundTripWithFakeServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	gotBody := make(chan string, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		c := textproto.NewConn(conn)
		_ = c.PrintfLine("220 test ready")
		for {
			line, err := c.ReadLine()
			if err != nil {
				return
			}
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO"):
				_ = c.PrintfLine("250-test\r\n250 SIZE 1048576")
			case strings.HasPrefix(upper, "MAIL FROM"):
				_ = c.PrintfLine("250 OK")
			case strings.HasPrefix(upper, "RCPT TO"):
				_ = c.PrintfLine("250 OK")
			case strings.HasPrefix(upper, "DATA"):
				_ = c.PrintfLine("354 Send")
				var sb strings.Builder
				br := bufio.NewReader(conn)
				for {
					l, err := br.ReadString('\n')
					if err != nil {
						return
					}
					if strings.TrimRight(l, "\r\n") == "." {
						break
					}
					sb.WriteString(l)
				}
				_ = c.PrintfLine("250 OK")
				gotBody <- sb.String()
			case strings.HasPrefix(upper, "QUIT"):
				_ = c.PrintfLine("221 Bye")
				return
			default:
				_ = c.PrintfLine("250 OK")
			}
		}
	}()

	host, port, _ := net.SplitHostPort(listener.Addr().String())
	var portInt int
	for _, c := range port {
		portInt = portInt*10 + int(c-'0')
	}
	sender, err := NewSMTP(Config{
		Host: host, Port: portInt,
		From: "no-reply@iogrid.org", FromName: "iogrid",
		StartTLS: false,
		Timeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sender.Send(ctx, Message{
		To: "alice@example.com", Subject: "hi", HTMLBody: "<b>hi</b>", TextBody: "hi",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case body := <-gotBody:
		if !strings.Contains(body, "To: alice@example.com") {
			t.Errorf("body missing To:\n%s", body)
		}
		if !strings.Contains(body, "Subject: hi") {
			t.Errorf("body missing Subject:\n%s", body)
		}
		if !strings.Contains(body, "<b>hi</b>") {
			t.Errorf("body missing html:\n%s", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("server never received body")
	}
}

