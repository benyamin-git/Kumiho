package android

import (
	"context"

	"github.com/benyamin-git/kumiho/internal/platform"
)

// netConfig is the no-op platform.NetConfig on Android.
//
// The Kotlin VpnService.Builder already applied everything the engine's Linux
// implementation would otherwise install: the tunnel addresses (10.8.0.2/32
// and fd00::2/128), the DNS server (10.8.0.2), the routes and the
// split-tunnel include/exclude lists (API 33+ excludeRoute, API 26-32
// complement routes), and the kill switch is the OS Always-on VPN /
// lockdown setting. The builder owns the interface until the service stops,
// so re-applying any of it from Go would fight it rather than help.
type netConfig struct{}

// Apply is a no-op: the VpnService.Builder configured the interface before
// its descriptor was adopted.
func (netConfig) Apply(context.Context, platform.NetConfigSpec) error { return nil }

// Cleanup is a no-op: stopping the VpnService tears the interface, its
// addresses, its routes and its DNS down with it.
func (netConfig) Cleanup(context.Context, platform.NetConfigSpec) {}
