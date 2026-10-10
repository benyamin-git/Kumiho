//go:build linux && !android

package linux

import (
	"context"

	"github.com/benyamin-git/kumiho/internal/netcfg"
	"github.com/benyamin-git/kumiho/internal/platform"
)

// netConfig adapts netcfg.Applier to platform.NetConfig. LAN detection stays
// in the applier so the Linux mechanisms remain in one package.
type netConfig struct {
	applier *netcfg.Applier
}

// Apply installs the network configuration, starting from a clean slate.
func (n netConfig) Apply(ctx context.Context, spec platform.NetConfigSpec) error {
	return n.applier.Apply(ctx, netcfgConfig(spec))
}

// Cleanup removes the network configuration; it never fails.
func (n netConfig) Cleanup(ctx context.Context, spec platform.NetConfigSpec) {
	n.applier.Cleanup(ctx, netcfgConfig(spec))
}

func netcfgConfig(spec platform.NetConfigSpec) netcfg.Config {
	return netcfg.Config{
		TunName:      spec.TunName,
		TunAddr:      spec.TunAddr,
		TunMTU:       spec.TunMTU,
		KillSwitch:   spec.KillSwitch,
		AllowLAN:     spec.AllowLAN,
		ExcludeCIDRs: spec.ExcludeCIDRs,
		LANSubnets:   spec.LANSubnets,
	}
}
