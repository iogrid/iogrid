package main

import (
	"bufio"
	"crypto/tls"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"time"
)

func main() {
	preferred := flag.String("ipversion", "auto", "IP version preference: auto|ipv4|ipv6")
	flag.Parse()

	apiKey := os.Getenv("IOGRID_API_KEY")
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "ERROR: IOGRID_API_KEY not set\n")
		os.Exit(1)
	}

	var service string
	switch *preferred {
	case "ipv4":
		service = "ipv4.icanhazip.com"
	case "ipv6":
		service = "ident.me"
	case "auto":
		// Try IPv4 first, fall back to IPv6
		ip := tryGetIP("ipv4.icanhazip.com", apiKey)
		if ip != "" {
			fmt.Println(ip)
			return
		}
		ip = tryGetIP("ident.me", apiKey)
		if ip != "" {
			fmt.Println(ip)
			return
		}
		fmt.Fprintf(os.Stderr, "ERROR: Could not retrieve IP from any service\n")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "ERROR: Unknown IP version '%s'. Use: auto|ipv4|ipv6\n", *preferred)
		os.Exit(1)
	}

	ip := tryGetIP(service, apiKey)
	if ip == "" {
		fmt.Fprintf(os.Stderr, "ERROR: Could not retrieve IP from %s\n", service)
		os.Exit(1)
	}
	fmt.Println(ip)
}

func tryGetIP(service string, apiKey string) string {
	tlsConfig := &tls.Config{ServerName: "proxy.iogrid.org"}
	conn, err := tls.Dial("tcp", "45.151.123.50:443", tlsConfig)
	if err != nil {
		return ""
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// SOCKS5 greeting
	conn.Write([]byte{0x05, 0x01, 0x02})
	resp := make([]byte, 2)
	if _, err := io.ReadFull(reader, resp); err != nil {
		return ""
	}

	// RFC 1929 auth
	auth := []byte{0x01, byte(len(apiKey))}
	auth = append(auth, []byte(apiKey)...)
	auth = append(auth, byte(len(apiKey)))
	auth = append(auth, []byte(apiKey)...)
	conn.Write(auth)
	authResp := make([]byte, 2)
	if _, err := io.ReadFull(reader, authResp); err != nil {
		return ""
	}
	if authResp[1] != 0 {
		return ""
	}

	// SOCKS5 CONNECT to service:80
	connectReq := []byte{0x05, 0x01, 0x00, 0x03, byte(len(service))}
	connectReq = append(connectReq, []byte(service)...)
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, 80)
	connectReq = append(connectReq, port...)
	conn.Write(connectReq)

	connResp := make([]byte, 4)
	if _, err := io.ReadFull(reader, connResp); err != nil {
		return ""
	}
	if connResp[1] != 0 {
		return ""
	}

	// Skip SOCKS5 address response
	switch connResp[3] {
	case 1: // IPv4
		io.ReadFull(reader, make([]byte, 6))
	case 4: // IPv6
		io.ReadFull(reader, make([]byte, 18))
	case 3: // Domain name
		lenByte := make([]byte, 1)
		io.ReadFull(reader, lenByte)
		io.ReadFull(reader, make([]byte, lenByte[0]+2))
	}

	// HTTP request
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	httpReq := fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", service)
	conn.Write([]byte(httpReq))

	// Read response
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		return ""
	}

	response := string(buf[:n])

	// Parse out the body from HTTP response
	parts := strings.Split(response, "\r\n\r\n")
	if len(parts) < 2 {
		return ""
	}

	body := strings.TrimSpace(parts[1])

	// Extract IP from response (plain text or HTML)
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Try to parse as plain IP
		if net.ParseIP(line) != nil {
			return line
		}
		// Try to extract from HTML
		re := regexp.MustCompile(`([0-9a-fA-F.:]+)`)
		if matches := re.FindStringSubmatch(line); len(matches) > 0 {
			if net.ParseIP(matches[1]) != nil {
				return matches[1]
			}
		}
	}

	return ""
}
