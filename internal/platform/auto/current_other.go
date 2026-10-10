//go:build !linux && !windows

// Package auto selects the platform.Provider for the running OS. It is used
// only at composition roots; the engine receives the provider explicitly.
package auto

import (
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/platform/unsupported"
)

// Current returns the fallback provider for OSes without a real one.
func Current() platform.Provider {
	return unsupported.New("unsupported", unsupported.DefaultPaths())
}
