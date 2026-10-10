//go:build linux && !android

package linux

import (
	"context"
	"net"

	"github.com/vishvananda/netlink"
)

// WatchLinks subscribes to netlink events until ctx is done. The
// subscriptions are best-effort: if they fail the watchdog still wakes on its
// 2 s tick, so startup never depends on them.
func (p *Provider) WatchLinks(ctx context.Context) (<-chan struct{}, error) {
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(done)
	}()

	linkCh := make(chan netlink.LinkUpdate, 16)
	routeCh := make(chan netlink.RouteUpdate, 32)
	addrCh := make(chan netlink.AddrUpdate, 16)
	_ = netlink.LinkSubscribe(linkCh, done)
	_ = netlink.RouteSubscribe(routeCh, done)
	_ = netlink.AddrSubscribe(addrCh, done)

	return coalesce(done,
		forward(done, linkCh),
		forward(done, routeCh),
		forward(done, addrCh),
	), nil
}

// forward turns a typed subscription channel into empty wake-ups until done
// closes.
func forward[T any](done <-chan struct{}, updates <-chan T) <-chan struct{} {
	wakeups := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case _, ok := <-updates:
				if !ok {
					return
				}
			}
			select {
			case wakeups <- struct{}{}:
			case <-done:
				return
			}
		}
	}()
	return wakeups
}

// coalesce merges wake-up sources into one channel of capacity 1 for the
// watchdog (PLAN.md §4.2): while the physical network is down the watchdog
// parks in WAITING_NETWORK, and an event here wakes it the moment
// connectivity returns instead of waiting out the poll interval. Bursts
// collapse into a single pending wake-up.
func coalesce(done <-chan struct{}, sources ...<-chan struct{}) <-chan struct{} {
	events := make(chan struct{}, 1)
	for _, src := range sources {
		go func(src <-chan struct{}) {
			for {
				select {
				case <-done:
					return
				case _, ok := <-src:
					if !ok {
						return
					}
				}
				select {
				case events <- struct{}{}:
				default: // a burst collapses into one pending wake-up
				}
			}
		}(src)
	}
	return events
}

// HasDefaultRoute reports whether the physical network has a usable default
// route. The lookup runs against the main table; Kumiho's own routes live in
// tables 1000/1001 behind fwmark rules and never show up here.
func (p *Provider) HasDefaultRoute() bool {
	if _, err := netlink.RouteGet(net.IPv4(9, 9, 9, 9)); err == nil {
		return true
	}
	if _, err := netlink.RouteGet(net.ParseIP("2001:4860:4860::8888")); err == nil {
		return true
	}
	return false
}
