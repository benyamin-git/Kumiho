//go:build linux && !android

// Package auto selects the platform.Provider for the running OS. It is used
// only at composition roots; the engine receives the provider explicitly.
package auto

import (
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/platform/linux"
)

// Current returns the provider for the running OS.
func Current() platform.Provider { return linux.New() }
