//go:build linux

package netcfg

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// execRun runs one command on Linux, capturing combined output.
func execRun(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if err != nil {
		return out, fmt.Errorf("%v: %w", err, cmdErr{out: out})
	}
	return out, nil
}

type cmdErr struct{ out string }

func (e cmdErr) Error() string {
	if e.out == "" {
		return "command failed"
	}
	return e.out
}

// DetectLANSubnets returns on-link IPv4 subnets that should bypass the
// tunnel (PLAN.md §3.2 rule 5). /31 and /32 links are skipped: they carry no
// interesting neighbours.
func DetectLANSubnets() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			ones, bits := ipnet.Mask.Size()
			if bits != 32 || ones >= 31 {
				continue
			}
			network := ipnet.IP.Mask(ipnet.Mask)
			cidr := fmt.Sprintf("%s/%d", network.String(), ones)
			if seen[cidr] {
				continue
			}
			seen[cidr] = true
			out = append(out, cidr)
		}
	}
	return out
}

// DefaultApplier returns an Applier wired to real system commands.
func DefaultApplier(logf func(format string, args ...any)) *Applier {
	return &Applier{
		Logf:      logf,
		Run:       execRun,
		DetectLAN: DetectLANSubnets,
	}
}
