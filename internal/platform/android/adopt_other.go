//go:build !linux && !android

package android

import "errors"

// adoptFD is a stub: descriptor adoption is a Linux/Android capability. The
// provider only calls it behind the android build tag, so this exists purely
// to keep `go build` and `go vet` green on other hosts (Windows).
func adoptFD(int, string) (*ipv6Filter, error) {
	return nil, errors.New("android: adopting a device fd is not supported on this OS")
}
