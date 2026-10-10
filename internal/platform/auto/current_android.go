//go:build android

// Package auto selects the platform.Provider for the running OS. It is used
// only at composition roots; the engine receives the provider explicitly.
package auto

import (
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/platform/android"
)

// Current returns the provider for the running OS.
func Current() platform.Provider { return android.New() }
