package upstream

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"testing"
)

func TestEdgeRootPEMsMatch(t *testing.T) {
	cases := []struct {
		name        string
		pem         []byte
		commonName  string
		fingerprint string
	}{
		{
			name:        "E46",
			pem:         edgeRootE46PEM,
			commonName:  "GlobalSign Root E46",
			fingerprint: "cbb9c44d84b8043e1050ea31a69f514955d7bfd2e2c6b49301019ad61d9f5058",
		},
		{
			name:        "R46",
			pem:         edgeRootR46PEM,
			commonName:  "GlobalSign Root R46",
			fingerprint: "4fa3126d8d3a11d1c4855a4f807cbad6cf919d3a5a88b03bea2c6372d93c40c9",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block, _ := pem.Decode(tc.pem)
			if block == nil || block.Type != "CERTIFICATE" {
				t.Fatalf("edge root PEM = %q, want one CERTIFICATE block", tc.pem)
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				t.Fatalf("parse edge root: %v", err)
			}
			if got := cert.Subject.CommonName; got != tc.commonName {
				t.Fatalf("edge root CN = %q, want %q", got, tc.commonName)
			}
			sum := sha256.Sum256(cert.Raw)
			if got := hex.EncodeToString(sum[:]); got != tc.fingerprint {
				t.Fatalf("edge root fingerprint = %s, want %s", got, tc.fingerprint)
			}
		})
	}
}

func TestEdgeRootCAsIncludesAnchors(t *testing.T) {
	subjects := EdgeRootCAs().Subjects()
	for _, raw := range [][]byte{edgeRootE46PEM, edgeRootR46PEM} {
		block, _ := pem.Decode(raw)
		if block == nil {
			t.Fatal("edge root PEM has no certificate")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("parse edge root: %v", err)
		}
		found := false
		for _, subject := range subjects {
			if bytes.Equal(subject, cert.RawSubject) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("EdgeRootCAs does not include the %s anchor", cert.Subject.CommonName)
		}
	}
}
