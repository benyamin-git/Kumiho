package mobile

import (
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// The expected tables below are the independent IPv4 range math for the
// exclusion sets, with 10.8.0.2/32 (C1) subtracted and the complement
// decomposed into the largest aligned prefixes.
var (
	excludes10 = []string{
		"10.0.0.0/13", "10.8.0.0/31", "10.8.0.3/32", "10.8.0.4/30",
		"10.8.0.8/29", "10.8.0.16/28", "10.8.0.32/27", "10.8.0.64/26",
		"10.8.0.128/25", "10.8.1.0/24", "10.8.2.0/23", "10.8.4.0/22",
		"10.8.8.0/21", "10.8.16.0/20", "10.8.32.0/19", "10.8.64.0/18",
		"10.8.128.0/17", "10.9.0.0/16", "10.10.0.0/15", "10.12.0.0/14",
		"10.16.0.0/12", "10.32.0.0/11", "10.64.0.0/10", "10.128.0.0/9",
	}
	routes10 = []string{
		"0.0.0.0/5", "8.0.0.0/7", "10.8.0.2/32", "11.0.0.0/8",
		"12.0.0.0/6", "16.0.0.0/4", "32.0.0.0/3", "64.0.0.0/2",
		"128.0.0.0/1",
	}

	excludesAllowLAN = append(slices.Clone(excludes10),
		"169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4")
	routesAllowLAN = []string{
		"0.0.0.0/5", "8.0.0.0/7", "10.8.0.2/32", "11.0.0.0/8",
		"12.0.0.0/6", "16.0.0.0/4", "32.0.0.0/3", "64.0.0.0/2",
		"128.0.0.0/3", "160.0.0.0/5", "168.0.0.0/8", "169.0.0.0/9",
		"169.128.0.0/10", "169.192.0.0/11", "169.224.0.0/12",
		"169.240.0.0/13", "169.248.0.0/14", "169.252.0.0/15",
		"169.255.0.0/16", "170.0.0.0/7", "172.0.0.0/12", "172.32.0.0/11",
		"172.64.0.0/10", "172.128.0.0/9", "173.0.0.0/8", "174.0.0.0/7",
		"176.0.0.0/4", "192.0.0.0/9", "192.128.0.0/11", "192.160.0.0/13",
		"192.169.0.0/16", "192.170.0.0/15", "192.172.0.0/14",
		"192.176.0.0/12", "192.192.0.0/10", "193.0.0.0/8", "194.0.0.0/7",
		"196.0.0.0/6", "200.0.0.0/5", "208.0.0.0/4", "240.0.0.0/4",
	}

	excludes203 = []string{"203.0.113.0/24"}
	routes203   = []string{
		"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/5", "200.0.0.0/7",
		"202.0.0.0/8", "203.0.0.0/18", "203.0.64.0/19", "203.0.96.0/20",
		"203.0.112.0/24", "203.0.114.0/23", "203.0.116.0/22",
		"203.0.120.0/21", "203.0.128.0/17", "203.1.0.0/16", "203.2.0.0/15",
		"203.4.0.0/14", "203.8.0.0/13", "203.16.0.0/12", "203.32.0.0/11",
		"203.64.0.0/10", "203.128.0.0/9", "204.0.0.0/6", "208.0.0.0/4",
		"224.0.0.0/3",
	}
)

func routesFor(t *testing.T, allowLAN bool, user ...string) routeSet {
	t.Helper()
	rs, err := computeRoutes(allowLAN, user, nil)
	if err != nil {
		t.Fatalf("computeRoutes(%v, %v): %v", allowLAN, user, err)
	}
	return rs
}

func requireStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s mismatch:\n got: %v\nwant: %v", what, got, want)
	}
}

func covers(t *testing.T, list []string, addr string) bool {
	t.Helper()
	ip := netip.MustParseAddr(addr)
	for _, raw := range list {
		if netip.MustParsePrefix(raw).Contains(ip) {
			return true
		}
	}
	return false
}

func TestRoutesNoExclusions(t *testing.T) {
	rs := routesFor(t, false)
	requireStrings(t, "excludes", rs.Excludes, nil)
	requireStrings(t, "routes", rs.Routes, []string{"0.0.0.0/0"})
}

func TestRoutesAllowLAN(t *testing.T) {
	rs := routesFor(t, true)
	requireStrings(t, "excludes", rs.Excludes, excludesAllowLAN)
	requireStrings(t, "routes", rs.Routes, routesAllowLAN)
	for _, lan := range []string{"10.1.2.3", "172.16.5.4", "192.168.1.1", "169.254.1.1", "224.0.0.1"} {
		if !covers(t, rs.Excludes, lan) {
			t.Errorf("%s is not excluded with allow_lan", lan)
		}
		if covers(t, rs.Routes, lan) {
			t.Errorf("%s is routed into the tunnel with allow_lan", lan)
		}
	}
}

func TestRoutesUserCIDR(t *testing.T) {
	rs := routesFor(t, false, "203.0.113.0/24")
	requireStrings(t, "excludes", rs.Excludes, excludes203)
	requireStrings(t, "routes", rs.Routes, routes203)
}

func TestRoutesMergesOverlappingAndAdjacent(t *testing.T) {
	for name, user := range map[string][]string{
		"single":    {"10.0.0.0/8"},
		"adjacent":  {"10.0.0.0/9", "10.128.0.0/9"},
		"overlap":   {"10.0.0.0/8", "10.1.0.0/16"},
		"duplicate": {"10.0.0.0/8", "10.0.0.0/8"},
	} {
		t.Run(name, func(t *testing.T) {
			rs := routesFor(t, false, user...)
			requireStrings(t, "excludes", rs.Excludes, excludes10)
			requireStrings(t, "routes", rs.Routes, routes10)
		})
	}
}

func TestRoutesRejectsDefaultRoute(t *testing.T) {
	_, err := computeRoutes(false, []string{"0.0.0.0/0"}, nil)
	if err == nil {
		t.Fatal("computeRoutes accepted 0.0.0.0/0")
	}
	var re *api.RemoteError
	if !errors.As(err, &re) {
		t.Fatalf("error is %T, want *api.RemoteError", err)
	}
	if re.Code != api.CodeBadRequest {
		t.Fatalf("code = %q, want %q", re.Code, api.CodeBadRequest)
	}
	if !containsFold(re.Message, "0.0.0.0/0") {
		t.Fatalf("message %q does not name the rejected prefix", re.Message)
	}
}

func TestRoutesIgnoresIPv6(t *testing.T) {
	for name, user := range map[string][]string{
		"only-v6":  {"2001:db8::/32", "fd00::/8"},
		"mixed":    {"2001:db8::/32", "203.0.113.0/24"},
		"v6-first": {"::/0"},
	} {
		t.Run(name, func(t *testing.T) {
			rs, err := computeRoutes(false, user, nil)
			if err != nil {
				t.Fatalf("computeRoutes(%v): %v", user, err)
			}
			for _, raw := range append(slices.Clone(rs.Excludes), rs.Routes...) {
				p := netip.MustParsePrefix(raw)
				if p.Addr().Is6() {
					t.Fatalf("%q is IPv6; IPv6 must never be in either list", raw)
				}
			}
			if name != "mixed" && !slices.Equal(rs.Routes, []string{"0.0.0.0/0"}) {
				t.Fatalf("routes = %v, want only 0.0.0.0/0", rs.Routes)
			}
			if name == "mixed" {
				requireStrings(t, "excludes", rs.Excludes, excludes203)
			}
		})
	}
}

func TestRoutesC1NeverExcludesDNSHost(t *testing.T) {
	t.Run("user excludes the DNS host alone", func(t *testing.T) {
		rs := routesFor(t, false, "10.8.0.2/32")
		requireStrings(t, "excludes", rs.Excludes, nil)
		requireStrings(t, "routes", rs.Routes, []string{"0.0.0.0/0"})
	})

	t.Run("user excludes the containing prefix", func(t *testing.T) {
		rs := routesFor(t, false, "10.8.0.0/16")
		if covers(t, rs.Excludes, "10.8.0.2") {
			t.Fatalf("10.8.0.2 is excluded: %v", rs.Excludes)
		}
		if !covers(t, rs.Routes, "10.8.0.2") {
			t.Fatalf("10.8.0.2 is not routed: %v", rs.Routes)
		}
		if !covers(t, rs.Excludes, "10.8.0.3") || !covers(t, rs.Excludes, "10.8.0.1") {
			t.Fatalf("the containing prefix was not kept: %v", rs.Excludes)
		}
	})

	t.Run("allow_lan keeps the DNS host routed", func(t *testing.T) {
		rs := routesFor(t, true)
		if covers(t, rs.Excludes, "10.8.0.2") {
			t.Fatalf("10.8.0.2 is excluded with allow_lan: %v", rs.Excludes)
		}
		if !covers(t, rs.Routes, "10.8.0.2") {
			t.Fatalf("10.8.0.2 is not routed with allow_lan: %v", rs.Routes)
		}
	})
}

func TestRoutesCap(t *testing.T) {
	var user []string
	for i := 0; i < 300; i++ {
		// Every other /32 stays disjoint, so the ranges do not merge and
		// the exclusion count reaches the cap.
		v := uint32(0x0A000000) + uint32(i)*2
		addr := netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
		user = append(user, netip.PrefixFrom(addr, 32).String())
	}
	_, err := computeRoutes(false, user, nil)
	if err == nil {
		t.Fatal("computeRoutes accepted more than 256 exclusions")
	}
	var re *api.RemoteError
	if !errors.As(err, &re) || re.Code != api.CodeBadRequest {
		t.Fatalf("error = %v, want bad_request", err)
	}
	if !containsFold(re.Message, "too many route exclusions") {
		t.Fatalf("message = %q, want the route-cap wording", re.Message)
	}
}

func TestRoutesAreSortedAndDisjoint(t *testing.T) {
	rs := routesFor(t, true, "203.0.113.0/24")
	for _, list := range [][]string{rs.Excludes, rs.Routes} {
		prevEnd := -1
		for _, raw := range list {
			p := netip.MustParsePrefix(raw)
			if p != p.Masked() {
				t.Fatalf("%q is not masked", raw)
			}
			start := testIPv4ToUint32(p.Addr())
			end := start + (1 << (32 - p.Bits())) - 1
			if int(start) <= prevEnd {
				t.Fatalf("%v is not sorted and disjoint at %q", list, raw)
			}
			prevEnd = int(end)
		}
	}
}

func TestBuildNetworkConfig(t *testing.T) {
	cfg := settings.Config{
		MTU:          1500,
		AllowLAN:     true,
		ExcludeCIDRs: []string{"203.0.113.0/24"},
	}
	raw, err := buildNetworkConfig(cfg, nil)
	if err != nil {
		t.Fatalf("buildNetworkConfig: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("network JSON is not an object: %v\n%s", err, raw)
	}
	for _, key := range []string{"mtu", "allow_lan", "exclude_cidrs", "ipv4_excludes", "ipv4_routes"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("network JSON lacks %q: %s", key, raw)
		}
	}
	var nc networkConfig
	if err := json.Unmarshal([]byte(raw), &nc); err != nil {
		t.Fatalf("decode network JSON: %v", err)
	}
	if nc.MTU != 1500 || !nc.AllowLAN {
		t.Fatalf("network JSON = %+v, want mtu 1500 and allow_lan", nc)
	}
	if !slices.Equal(nc.ExcludeCIDRs, []string{"203.0.113.0/24"}) {
		t.Fatalf("exclude_cidrs = %v, want the configured list", nc.ExcludeCIDRs)
	}
	if !slices.Contains(nc.IPv4Excludes, "203.0.113.0/24") {
		t.Fatalf("ipv4_excludes lacks the user CIDR: %v", nc.IPv4Excludes)
	}
	if !covers(t, nc.IPv4Routes, "8.8.8.8") || covers(t, nc.IPv4Routes, "203.0.113.9") {
		t.Fatalf("ipv4_routes is not the complement: %v", nc.IPv4Routes)
	}
	if !covers(t, nc.IPv4Routes, "10.8.0.2") {
		t.Fatal("10.8.0.2 is not routed")
	}
}

func TestBuildNetworkConfigEmptyListsAreArrays(t *testing.T) {
	raw, err := buildNetworkConfig(settings.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("buildNetworkConfig: %v", err)
	}
	var decoded struct {
		ExcludeCIDRs []string `json:"exclude_cidrs"`
		IPv4Excludes []string `json:"ipv4_excludes"`
		IPv4Routes   []string `json:"ipv4_routes"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.ExcludeCIDRs == nil || decoded.IPv4Excludes == nil || decoded.IPv4Routes == nil {
		t.Fatalf("lists must encode as arrays, got %s", raw)
	}
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func testIPv4ToUint32(addr netip.Addr) uint32 {
	b := addr.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
