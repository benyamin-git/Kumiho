package upstream

import (
	"crypto/x509"
	_ "embed"
	"sync"
)

// The Fastly MASQUE edges use a GlobalSign certificate chain rooted at
// GlobalSign Root E46 or Root R46. Some Android TV system images (notably the
// 32-bit BRAVIA build on API 31) ship a CA store that predates those roots, so
// the edge TLS handshake fails with "certificate signed by unknown authority"
// even though the server sends the full chain. Anchor both roots in the edge
// pool.
//
// The PEMs are GlobalSign's published roots, copied from the Mozilla CA bundle
// (Debian ca-certificates); SHA-256 fingerprints:
// E46: CB:B9:C4:4D:84:B8:04:3E:10:50:EA:31:A6:9F:51:49:55:D7:BF:D2:E2:C6:B4:93:01:01:9A:D6:1D:9F:50:58
// R46: 4F:A3:12:6D:8D:3A:11:D1:C4:85:5A:4F:80:7C:BA:D6:CF:91:9D:3A:5A:88:B0:3B:EA:2C:63:72:D9:3C:40:C9
//
//go:embed roots/globalsign-root-e46.pem
var edgeRootE46PEM []byte

//go:embed roots/globalsign-root-r46.pem
var edgeRootR46PEM []byte

var edgeRootPEMs = [][]byte{edgeRootE46PEM, edgeRootR46PEM}

var (
	edgeRootsOnce sync.Once
	edgeRoots     *x509.CertPool
)

// EdgeRootCAs returns the platform trust store plus the GlobalSign E46 and R46
// anchors. It is shared by every upstream TLS handshake; the pool is read-only
// after construction.
func EdgeRootCAs() *x509.CertPool {
	edgeRootsOnce.Do(func() {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		// The PEMs are compiled in and covered by fingerprint tests; a false
		// return would mean an embedded asset was corrupted at build time.
		for _, pem := range edgeRootPEMs {
			pool.AppendCertsFromPEM(pem)
		}
		edgeRoots = pool
	})
	return edgeRoots
}
