package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"

	"github.com/benyamin-git/kumiho/internal/netcfg"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// runCleanup removes stale routing/nftables/DNS artifacts left by a crashed
// daemon (PLAN.md §7, §4.3). Needs CAP_NET_ADMIN; best-effort otherwise.
func runCleanup(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: kumiho cleanup\n\nRemoves stale routes, rules, nftables state and DNS link configuration.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "kumiho cleanup: only supported on Linux")
		return 1
	}

	store, err := settings.Open(settings.DefaultPaths())
	if err != nil {
		fmt.Fprintf(stderr, "kumiho cleanup: %v\n", err)
		return 1
	}
	cfg := store.EffectiveConfig()
	applier := netcfg.DefaultApplier(func(format string, args ...any) {
		fmt.Fprintf(stderr, "kumiho cleanup: "+format+"\n", args...)
	})
	applier.Cleanup(context.Background(), netcfg.Config{
		TunName:      netcfg.DefaultTunName,
		TunAddr:      netcfg.DefaultTunAddr,
		TunMTU:       cfg.MTU,
		KillSwitch:   store.EffectiveKillSwitch(),
		AllowLAN:     cfg.AllowLAN,
		ExcludeCIDRs: cfg.ExcludeCIDRs,
	})
	fmt.Fprintln(stdout, "Stale kumiho routing, nftables and DNS state were removed (best effort).")
	return 0
}
