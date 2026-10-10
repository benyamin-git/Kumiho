//go:build !linux

package engine

// The daemon only runs on Linux; the stubs keep the package (and its tests)
// building elsewhere. No events are ever delivered and the network is always
// assumed to be up.

type netMonitor struct {
	events chan struct{}
}

func (m *netMonitor) C() <-chan struct{} { return m.events }

func startNetMonitor(_ <-chan struct{}) *netMonitor {
	return &netMonitor{events: make(chan struct{})}
}

func hasDefaultRoute() bool { return true }
