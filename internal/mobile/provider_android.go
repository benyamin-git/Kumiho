//go:build android

package mobile

import (
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/platform/android"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// newProvider wires the app-private paths and the Kotlin VpnService bridge
// into the Android platform provider. A nil bridge stays nil: proxy-only mode
// works and a full-tunnel connect fails with a clear error.
func newProvider(paths settings.Paths, bridge DeviceBridge) platform.Provider {
	if bridge == nil {
		return android.NewWithPaths(paths, nil)
	}
	return android.NewWithPaths(paths, bridge)
}
