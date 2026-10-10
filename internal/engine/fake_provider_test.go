package engine

import (
	"context"
	"io"
	"net"
	"sync"

	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/platform"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// fakeProvider implements platform.Provider for engine tests. Every provider
// call that participates in the connect/teardown sequence is recorded in
// order, and the device/netcfg fakes expose hooks so the full-tunnel test can
// assert the documented ordering.
type fakeProvider struct {
	name    string
	ua      string
	tun     platform.TunDefaults
	dnsAddr string
	routeUp bool
	links   chan struct{}

	dev *fakeDevice
	nc  *fakeNetConfig

	mu     sync.Mutex
	events []string
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		name:    "fake",
		ua:      fxa.UserAgent,
		tun:     platform.TunDefaults{Name: "foxy0", Addr: "10.8.0.2"},
		dnsAddr: "127.0.0.1:0",
		routeUp: true,
	}
}

func (p *fakeProvider) record(event string) {
	p.mu.Lock()
	p.events = append(p.events, event)
	p.mu.Unlock()
}

func (p *fakeProvider) eventList() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.events...)
}

func (p *fakeProvider) Name() string                      { return p.name }
func (p *fakeProvider) UserAgent() string                 { return p.ua }
func (p *fakeProvider) TunDefaults() platform.TunDefaults { return p.tun }
func (p *fakeProvider) HasDefaultRoute() bool             { return p.routeUp }
func (p *fakeProvider) BypassDialer() *net.Dialer         { return &net.Dialer{} }
func (p *fakeProvider) Paths() settings.Paths             { return settings.Paths{} }
func (p *fakeProvider) ControlEndpoint() string           { return "" }
func (p *fakeProvider) ChownControlEndpoint(string)       {}

func (p *fakeProvider) DNSListenAddr() string {
	p.record("dns.addr")
	return p.dnsAddr
}

func (p *fakeProvider) WatchLinks(context.Context) (<-chan struct{}, error) {
	if p.links != nil {
		return p.links, nil
	}
	return make(chan struct{}), nil
}

func (p *fakeProvider) OpenDevice(_ context.Context, spec platform.DeviceSpec) (platform.Device, error) {
	p.record("device.open")
	if p.dev == nil {
		p.dev = newFakeDevice(p, spec.Name)
	}
	return p.dev, nil
}

func (p *fakeProvider) NetConfig() platform.NetConfig {
	if p.nc == nil {
		p.nc = &fakeNetConfig{p: p}
	}
	return p.nc
}

// fakeDevice is a blocking in-memory platform.Device: Read blocks until Close
// (net.Pipe semantics), which is what unblocks the netstack loops on teardown.
type fakeDevice struct {
	name string
	p    *fakeProvider
	c, s net.Conn

	mu      sync.Mutex
	closed  bool
	onClose func()
}

func newFakeDevice(p *fakeProvider, name string) *fakeDevice {
	c, s := net.Pipe()
	return &fakeDevice{name: name, p: p, c: c, s: s}
}

func (d *fakeDevice) Read(b []byte) (int, error)  { return d.c.Read(b) }
func (d *fakeDevice) Write(b []byte) (int, error) { return d.c.Write(b) }
func (d *fakeDevice) Name() string                { return d.name }

func (d *fakeDevice) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	onClose := d.onClose
	d.mu.Unlock()

	d.p.record("device.close")
	if onClose != nil {
		onClose()
	}
	err := d.c.Close()
	_ = d.s.Close()
	return err
}

// fakeNetConfig records Apply/Cleanup calls and the last spec they saw.
type fakeNetConfig struct {
	p *fakeProvider

	mu        sync.Mutex
	applies   int
	cleanups  int
	last      platform.NetConfigSpec
	onApply   func(platform.NetConfigSpec)
	onCleanup func()
}

func (n *fakeNetConfig) Apply(_ context.Context, spec platform.NetConfigSpec) error {
	n.mu.Lock()
	n.applies++
	n.last = spec
	hook := n.onApply
	n.mu.Unlock()
	n.p.record("netcfg.apply")
	if hook != nil {
		hook(spec)
	}
	return nil
}

func (n *fakeNetConfig) Cleanup(_ context.Context, spec platform.NetConfigSpec) {
	n.mu.Lock()
	n.cleanups++
	n.last = spec
	hook := n.onCleanup
	n.mu.Unlock()
	n.p.record("netcfg.cleanup")
	if hook != nil {
		hook()
	}
}

func (n *fakeNetConfig) counts() (applies, cleanups int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.applies, n.cleanups
}

func (n *fakeNetConfig) lastSpec() platform.NetConfigSpec {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.last
}

var (
	_ platform.Provider  = (*fakeProvider)(nil)
	_ platform.Device    = (*fakeDevice)(nil)
	_ platform.NetConfig = (*fakeNetConfig)(nil)
	_ io.ReadWriteCloser = (*fakeDevice)(nil)
)
