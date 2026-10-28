package upstream

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"testing"
)

func TestEdgeRootPEMIsGlobalSignRootE46(t *testing.T) {
	block, _ := pem.Decode(edgeRootPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("edge root PEM = %q, want one CERTIFICATE block", edgeRootPEM)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse edge root: %v", err)
	}
	if got := cert.Subject.CommonName; got != "GlobalSign Root E46" {
		t.Fatalf("edge root CN = %q, want GlobalSign Root E46", got)
	}
	want := "cbb9c44d84b8043e1050ea31a69f514955d7bfd2e2c6b49301019ad61d9f5058"
	sum := sha256.Sum256(cert.Raw)
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("edge root fingerprint = %s, want %s", got, want)
	}
}

func TestEdgeRootCAsIncludesAnchor(t *testing.T) {
	block, _ := pem.Decode(edgeRootPEM)
	if block == nil {
		t.Fatal("edge root PEM has no certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse edge root: %v", err)
	}
	for _, subject := range EdgeRootCAs().Subjects() {
		if bytes.Equal(subject, cert.RawSubject) {
			return
		}
	}
	t.Fatal("EdgeRootCAs does not include the GlobalSign Root E46 anchor")
}
