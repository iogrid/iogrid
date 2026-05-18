// Package ca is the in-service certificate authority used by providers-svc
// to issue short-lived mTLS client certificates to paired daemons.
//
// On first run the service generates a self-signed ECDSA P-256 root CA in
// memory; subsequent restarts re-load it from $PROVIDERS_CA_DIR (defaults
// to /var/lib/iogrid/providers-ca). In test the CA can be constructed via
// NewInMemory which keeps everything ephemeral — no filesystem writes.
//
// The CA exposes two operations:
//
//   - Bundle() returns the PEM-encoded root certificate so daemons can pin
//     it for outbound connections.
//   - IssueDaemonCert(req) signs a client certificate for the daemon's
//     public key with CN=<provider_id>, valid for 90 days.
//
// Keys never leave the service: only the public half travels in over the
// wire (PairDaemonRequest.DaemonPublicKey) and only the signed certificate
// (plus the CA bundle) is returned.
package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"
)

// CA is the in-service certificate authority. Safe for concurrent use.
type CA struct {
	mu          sync.Mutex
	rootCert    *x509.Certificate
	rootCertDER []byte
	rootCertPEM []byte
	rootKey     *ecdsa.PrivateKey
}

// NewInMemory bootstraps a fresh CA without touching the filesystem. The
// returned CA is suitable for tests and ephemeral environments. The root
// certificate is valid for ten years.
func NewInMemory() (*CA, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate root key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "iogrid providers-svc Root CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("create root cert: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse root cert: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	return &CA{
		rootCert:    cert,
		rootCertDER: der,
		rootCertPEM: pemBytes,
		rootKey:     priv,
	}, nil
}

// Bundle returns the PEM-encoded root certificate that paired daemons
// should pin when establishing mTLS connections back to the coordinator.
func (c *CA) Bundle() []byte {
	out := make([]byte, len(c.rootCertPEM))
	copy(out, c.rootCertPEM)
	return out
}

// IssueRequest carries the inputs needed to sign one daemon certificate.
type IssueRequest struct {
	// ProviderID — bound as the certificate CommonName. The dispatch path
	// reads this back to authenticate the connecting daemon.
	ProviderID string
	// DaemonPublicKey is the DER-encoded SubjectPublicKeyInfo the daemon
	// generated at pairing time.
	DaemonPublicKey []byte
	// Lifetime, defaults to 90 days when zero.
	Lifetime time.Duration
}

// IssueDaemonCert signs a client certificate for the daemon's public key.
// Returns the PEM-encoded leaf certificate.
func (c *CA) IssueDaemonCert(req IssueRequest) ([]byte, error) {
	if req.ProviderID == "" {
		return nil, errors.New("ca: provider id required")
	}
	if len(req.DaemonPublicKey) == 0 {
		return nil, errors.New("ca: daemon public key required")
	}

	pub, err := x509.ParsePKIXPublicKey(req.DaemonPublicKey)
	if err != nil {
		return nil, fmt.Errorf("ca: parse daemon public key: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	lifetime := req.Lifetime
	if lifetime == 0 {
		lifetime = 90 * 24 * time.Hour
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   req.ProviderID,
			Organization: []string{"iogrid"},
		},
		NotBefore:   time.Now().Add(-time.Minute),
		NotAfter:    time.Now().Add(lifetime),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.rootCert, pub, c.rootKey)
	if err != nil {
		return nil, fmt.Errorf("ca: sign daemon cert: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// randomSerial draws a positive 159-bit serial number, matching RFC 5280
// guidance and well within the 20-byte ceiling.
func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 159)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("ca: random serial: %w", err)
	}
	return n, nil
}
