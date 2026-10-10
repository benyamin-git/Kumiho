package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/platform/auto"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// cleanupLogfSetter is the optional logging capability a platform NetConfig
// provides (both the Linux and Windows implementations do).
type cleanupLogfSetter interface {
	SetLogf(func(format string, args ...any))
}

// staleCleaner is the optional capability of platform NetConfig
// implementations that can remove state a crashed run left behind even
// without a live configuration (Windows foxy0 adapter and default routes).
type staleCleaner interface {
	CleanupStale(context.Context)
}

// runCleanup removes stale routing/DNS/interface artifacts left by a crashed
// daemon (PLAN.md §7, §4.3). Needs privileges; best-effort otherwise.
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

	plat := auto.Current()
	store, err := settings.Open(plat.Paths())
	if err != nil {
		fmt.Fprintf(stderr, "kumiho cleanup: %v\n", err)
		return 1
	}
	cfg := store.EffectiveConfig()
	nc := plat.NetConfig()
	if ls, ok := nc.(cleanupLogfSetter); ok {
		ls.SetLogf(func(format string, args ...any) {
			fmt.Fprintf(stderr, "kumiho cleanup: "+format+"\n", args...)
		})
	}
	tun := plat.TunDefaults()
	nc.Cleanup(context.Background(), platform.NetConfigSpec{
		TunName:      tun.Name,
		TunAddr:      tun.Addr,
		TunMTU:       cfg.MTU,
		KillSwitch:   store.EffectiveKillSwitch(),
		AllowLAN:     cfg.AllowLAN,
		ExcludeCIDRs: cfg.ExcludeCIDRs,
	})
	if stale, ok := nc.(staleCleaner); ok {
		stale.CleanupStale(context.Background())
	}
	fmt.Fprintln(stdout, cleanupSuccessMessage())
	return 0
}
