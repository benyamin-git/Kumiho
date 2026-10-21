package mobile

import (
	"encoding/json"
	"fmt"
	"math/bits"
	"net/netip"
	"slices"
	"strings"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/settings"
)

const (
	// routeListLimit bounds ipv4_excludes and ipv4_routes. Both become
	// VpnService.Builder entries, and an unbounded list would let a user
	// config exhaust the builder or the Binder transaction.
	routeListLimit = 256
	// dnsListenerAddr is the in-tunnel resolver the VpnService advertises and
	// the engine serves. C1: it must never be excluded, or DNS blackholes.
	dnsListenerAddr = "10.8.0.2"
	maxIPv4         = uint32(0xffffffff)
)

// lanExclusions are the base split-tunnel exclusions for allow_lan (D49).
var lanExclusions = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"224.0.0.0/4",
}

// routeSet is the sanitized exclusion list and its complement.
type routeSet struct {
	Excludes []string
	Routes   []string
}

// networkConfig is the JSON Kotlin's DeviceBridge.Configure parses. The
// excludes serve Builder.excludeRoute on API 33+; the routes are the computed
// complement for addRoute on API 26-32.
type networkConfig struct {
	MTU          int      `json:"mtu"`
	AllowLAN     bool     `json:"allow_lan"`
	ExcludeCIDRs []string `json:"exclude_cidrs"`
	IPv4Excludes []string `json:"ipv4_excludes"`
	IPv4Routes   []string `json:"ipv4_routes"`
}

// buildNetworkConfig renders the connect-time network plan from the effective
// config and the D49/C1 route rules.
func buildNetworkConfig(cfg settings.Config, logf func(string, ...any)) (string, error) {
	rs, err := computeRoutes(cfg.AllowLAN, cfg.ExcludeCIDRs, logf)
	if err != nil {
		return "", err
	}
	excludeCIDRs := cfg.ExcludeCIDRs
	if excludeCIDRs == nil {
		excludeCIDRs = []string{}
	}
	raw, err := json.Marshal(networkConfig{
		MTU:          cfg.MTU,
		AllowLAN:     cfg.AllowLAN,
		ExcludeCIDRs: excludeCIDRs,
		IPv4Excludes: rs.Excludes,
		IPv4Routes:   rs.Routes,
	})
	if err != nil {
		return "", &api.RemoteError{Code: api.CodeInternal, Message: fmt.Sprintf("encode network config: %v", err)}
	}
	return string(raw), nil
}

// computeRoutes turns allow_lan plus the user CIDRs into the sanitized IPv4
// exclusion list and the complement routes. IPv6 prefixes are ignored with a
// debug log; 0.0.0.0/0 is rejected because excluding it would blackhole the
// whole device.
func computeRoutes(allowLAN bool, userCIDRs []string, logf func(string, ...any)) (routeSet, error) {
	var ranges []addrRange
	if allowLAN {
		for _, raw := range lanExclusions {
			prefix, err := netip.ParsePrefix(raw)
			if err != nil {
				return routeSet{}, fmt.Errorf("kumiho mobile: built-in exclusion %q: %w", raw, err)
			}
			ranges = append(ranges, rangeOf(prefix))
		}
	}
	for _, raw := range userCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return routeSet{}, badRequest("invalid route exclusion %q: %v", raw, err)
		}
		if prefix.Addr().Is6() {
			debugf(logf, "ignoring IPv6 route exclusion %q; IPv6 stays blackholed", raw)
			continue
		}
		prefix = prefix.Masked()
		if prefix.Bits() == 0 {
			return routeSet{}, badRequest("route exclusion %q would exclude the whole internet", prefix)
		}
		ranges = append(ranges, rangeOf(prefix))
	}

	ranges = mergeRanges(ranges)
	// C1: the DNS listener address is never in the exclude list, even when
	// the user (or allow_lan) excluded the prefix that contains it.
	ranges = subtractAddr(ranges, netip.MustParseAddr(dnsListenerAddr))

	excludes := rangesToPrefixes(ranges)
	routes := rangesToPrefixes(complementRanges(ranges))
	if len(excludes) > routeListLimit || len(routes) > routeListLimit {
		return routeSet{}, badRequest(
			"too many route exclusions: %d excluded prefixes and %d routes (limit %d each)",
			len(excludes), len(routes), routeListLimit)
	}
	return routeSet{Excludes: excludes, Routes: routes}, nil
}

// addrRange is an inclusive IPv4 address range in host byte order.
type addrRange struct {
	start uint32
	end   uint32
}

func rangeOf(prefix netip.Prefix) addrRange {
	start := ipv4Uint32(prefix.Addr())
	size := uint64(1) << (32 - prefix.Bits())
	return addrRange{start: start, end: start + uint32(size-1)}
}

// mergeRanges sorts and merges overlapping, duplicate and adjacent ranges.
func mergeRanges(in []addrRange) []addrRange {
	if len(in) == 0 {
		return nil
	}
	slices.SortFunc(in, func(a, b addrRange) int {
		switch {
		case a.start < b.start:
			return -1
		case a.start > b.start:
			return 1
		}
		return 0
	})
	out := make([]addrRange, 0, len(in))
	for _, r := range in {
		if n := len(out); n > 0 && uint64(r.start) <= uint64(out[n-1].end)+1 {
			if r.end > out[n-1].end {
				out[n-1].end = r.end
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// subtractAddr removes one address from the ranges, splitting the containing
// range into up to two.
func subtractAddr(in []addrRange, addr netip.Addr) []addrRange {
	target := ipv4Uint32(addr)
	out := make([]addrRange, 0, len(in)+1)
	for _, r := range in {
		if target < r.start || target > r.end {
			out = append(out, r)
			continue
		}
		if r.start < target {
			out = append(out, addrRange{start: r.start, end: target - 1})
		}
		if target < r.end {
			out = append(out, addrRange{start: target + 1, end: r.end})
		}
	}
	return out
}

// complementRanges returns 0.0.0.0/0 minus the merged ranges.
func complementRanges(in []addrRange) []addrRange {
	if len(in) == 0 {
		return []addrRange{{start: 0, end: maxIPv4}}
	}
	out := make([]addrRange, 0, len(in)+1)
	next := uint64(0)
	for _, r := range in {
		if uint64(r.start) > next {
			out = append(out, addrRange{start: uint32(next), end: r.start - 1})
		}
		next = uint64(r.end) + 1
	}
	if next <= uint64(maxIPv4) {
		out = append(out, addrRange{start: uint32(next), end: maxIPv4})
	}
	return out
}

// rangesToPrefixes decomposes sorted, disjoint ranges into the largest aligned
// CIDR blocks, in ascending order.
func rangesToPrefixes(in []addrRange) []string {
	out := make([]string, 0, len(in))
	for _, r := range in {
		for start := uint64(r.start); start <= uint64(r.end); {
			size := uint64(1)
			for {
				next := size * 2
				if start%next == 0 && start+next-1 <= uint64(r.end) {
					size = next
					continue
				}
				break
			}
			prefix := netip.PrefixFrom(uint32ToIPv4(uint32(start)), 32-bits.Len64(size-1))
			out = append(out, prefix.String())
			start += size
		}
	}
	return out
}

func ipv4Uint32(addr netip.Addr) uint32 {
	b := addr.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func uint32ToIPv4(v uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

func debugf(logf func(string, ...any), format string, args ...any) {
	if logf != nil {
		logf(format, args...)
	}
}
