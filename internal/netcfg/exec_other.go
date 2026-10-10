//go:build !linux || android

package netcfg

import (
	"context"
	"fmt"
)

// DefaultApplier on non-Linux hosts is a stub: the full tunnel only exists
// on Linux (this keeps the daemon buildable on the Windows dev box).
func DefaultApplier(logf func(format string, args ...any)) *Applier {
	return &Applier{
		Logf: logf,
		Run: func(context.Context, string, ...string) (string, error) {
			return "", fmt.Errorf("netcfg: only supported on Linux")
		},
	}
}
