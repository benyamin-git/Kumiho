//go:build !linux || android

// Package auto selects the platform.Provider for the running OS. It is used
// only at composition roots; the engine receives the provider explicitly.
package auto

import (
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/platform/unsupported"
)

// Current returns the fallback provider until this OS gets a real one (T5
// retags this file once windows/android stubs exist).
func Current() platform.Provider {
	return unsupported.New("unsupported", unsupported.DefaultPaths())
}
