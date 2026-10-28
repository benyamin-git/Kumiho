package upstream

import (
	"crypto/x509"
	_ "embed"
	"sync"
)

// The Fastly MASQUE edges use a GlobalSign certificate chain rooted at
// GlobalSign Root E46. Some Android TV system images (notably the 32-bit
// BRAVIA build on API 31) ship a CA store that predates that root, so the edge
// TLS handshake fails with "certificate signed by unknown authority" even
// though the server sends the full chain. Anchor the root in the edge pool.
//
// The PEM is GlobalSign's published root, copied from the Mozilla CA bundle
// (Debian ca-certificates); SHA-256 fingerprint:
// CB:B9:C4:4D:84:B8:04:3E:10:50:EA:31:A6:9F:51:49:55:D7:BF:D2:E2:C6:B4:93:01:01:9A:D6:1D:9F:50:58.
//
//go:embed roots/globalsign-root-e46.pem
var edgeRootPEM []byte

var (
	edgeRootsOnce sync.Once
	edgeRoots     *x509.CertPool
)

// EdgeRootCAs returns the platform trust store plus the GlobalSign Root E46
// anchor. It is shared by every upstream TLS handshake; the pool is read-only
// after construction.
func EdgeRootCAs() *x509.CertPool {
	edgeRootsOnce.Do(func() {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		// The PEM is compiled in and covered by a fingerprint test; a false
		// return would mean the embedded asset was corrupted at build time.
		pool.AppendCertsFromPEM(edgeRootPEM)
		edgeRoots = pool
	})
	return edgeRoots
}
