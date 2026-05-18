package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

func TestNewInMemory(t *testing.T) {
	c, err := NewInMemory()
	if err != nil {
		t.Fatalf("NewInMemory: %v", err)
	}
	if c.rootCert == nil || c.rootKey == nil {
		t.Fatalf("expected root cert and key to be populated")
	}
	bundle := c.Bundle()
	if !strings.Contains(string(bundle), "BEGIN CERTIFICATE") {
		t.Fatalf("expected PEM CERTIFICATE block, got %q", string(bundle))
	}
}

func TestIssueDaemonCert_Success(t *testing.T) {
	c, err := NewInMemory()
	if err != nil {
		t.Fatalf("NewInMemory: %v", err)
	}

	daemonKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("daemon key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&daemonKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal pub: %v", err)
	}

	pemBytes, err := c.IssueDaemonCert(IssueRequest{
		ProviderID:      "11111111-1111-1111-1111-111111111111",
		DaemonPublicKey: pubDER,
	})
	if err != nil {
		t.Fatalf("IssueDaemonCert: %v", err)
	}
	blk, _ := pem.Decode(pemBytes)
	if blk == nil {
		t.Fatalf("pem decode failed")
	}
	leaf, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	if leaf.Subject.CommonName != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected CN %q", leaf.Subject.CommonName)
	}
	// Verify the leaf chains to the root.
	pool := x509.NewCertPool()
	pool.AddCert(c.rootCert)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:       pool,
		CurrentTime: time.Now(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("leaf does not verify against CA: %v", err)
	}
	// Default lifetime is ~90 days.
	gotLifetime := leaf.NotAfter.Sub(leaf.NotBefore)
	if gotLifetime < 89*24*time.Hour || gotLifetime > 91*24*time.Hour {
		t.Fatalf("unexpected lifetime %v", gotLifetime)
	}
}

func TestIssueDaemonCert_MissingFields(t *testing.T) {
	c, _ := NewInMemory()
	if _, err := c.IssueDaemonCert(IssueRequest{ProviderID: ""}); err == nil {
		t.Fatalf("expected error for empty provider id")
	}
	if _, err := c.IssueDaemonCert(IssueRequest{ProviderID: "x", DaemonPublicKey: nil}); err == nil {
		t.Fatalf("expected error for empty public key")
	}
}

func TestIssueDaemonCert_BadKey(t *testing.T) {
	c, _ := NewInMemory()
	_, err := c.IssueDaemonCert(IssueRequest{
		ProviderID:      "x",
		DaemonPublicKey: []byte("not-a-key"),
	})
	if err == nil {
		t.Fatalf("expected error for malformed key")
	}
}
