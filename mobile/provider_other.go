//go:build !android

package mobile

import (
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/platform/unsupported"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// newProvider returns the fallback provider on non-Android hosts so the
// facade's unit tests can start a real engine. Only proxy-only mode works
// there; the full tunnel fails before opening a device.
func newProvider(paths settings.Paths, _ DeviceBridge) platform.Provider {
	return unsupported.New("unsupported", paths)
}
