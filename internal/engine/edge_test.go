package engine

import (
	"context"
	"net"
	"testing"

	mdns "github.com/miekg/dns"

	"github.com/benyamin-git/kumiho/internal/serverlist"
)

// fakeBootstrap answers every A query with its configured address ("" = none).
type fakeBootstrap struct{ ip string }

func (f fakeBootstrap) HandleQuery(req *mdns.Msg, _ bool) *mdns.Msg {
	resp := new(mdns.Msg)
	resp.SetReply(req)
	if len(req.Question) == 1 && req.Question[0].Qtype == mdns.TypeA && f.ip != "" {
		resp.Answer = append(resp.Answer, &mdns.A{
			Hdr: mdns.RR_Header{Name: req.Question[0].Name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: 60},
			A:   net.ParseIP(f.ip).To4(),
		})
	}
	return resp
}

// peerSession is a fake upstream session that reports a peer address.
type peerSession struct {
	*fakeSession
	peer net.Addr
}

func (p peerSession) RemoteAddr() net.Addr { return p.peer }

func TestResolveEdgeUsesBootstrapAndCaches(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.bootstrap = []bootstrapResolver{fakeBootstrap{ip: "203.0.113.7"}}

	if got := ctrl.resolveEdge(context.Background(), "p.m1.example.net"); got != "203.0.113.7" {
		t.Fatalf("resolveEdge = %q, want the bootstrap address", got)
	}
	// Redials answer from the cache even when no resolver is available.
	ctrl.bootstrap = nil
	if got := ctrl.resolveEdge(context.Background(), "p.m1.example.net"); got != "203.0.113.7" {
		t.Fatalf("cached resolveEdge = %q", got)
	}
	// IP literals pass through untouched.
	if got := ctrl.resolveEdge(context.Background(), "203.0.113.9"); got != "203.0.113.9" {
		t.Fatalf("resolveEdge(literal) = %q", got)
	}
}

func TestResolveEdgeFallsBackToSystemAndCaches(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.bootstrap = []bootstrapResolver{fakeBootstrap{ip: ""}} // providers answer nothing

	got := ctrl.resolveEdge(context.Background(), "localhost")
	if got != "127.0.0.1" {
		t.Fatalf("system fallback = %q, want 127.0.0.1", got)
	}
	if cached := ctrl.cachedEdgeIP("localhost"); cached != "127.0.0.1" {
		t.Fatalf("cache = %q, want 127.0.0.1", cached)
	}
}

func TestRememberEdgePeerCachesSessionIP(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	sess := peerSession{fakeSession: newFakeSession(), peer: &net.TCPAddr{IP: net.ParseIP("198.51.100.9"), Port: 2499}}
	ctrl.rememberEdgePeer("p.m1.example.net", sess)
	if got := ctrl.cachedEdgeIP("p.m1.example.net"); got != "198.51.100.9" {
		t.Fatalf("cached peer = %q, want 198.51.100.9", got)
	}
}

func TestUpstreamOptionsPinsBootstrappedEdge(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	ctrl.bootstrap = []bootstrapResolver{fakeBootstrap{ip: "203.0.113.7"}}

	opts := ctrl.upstreamOptions(context.Background(),
		serverlist.Candidate{Host: "p.m1.example.net", Port: 2499}, nil)
	if opts.Host != "p.m1.example.net" || opts.Port != 2499 || opts.PinnedIP != "203.0.113.7" {
		t.Fatalf("opts = %+v", opts)
	}

	// The cache keeps the pin across redials with no resolvers left.
	ctrl.bootstrap = nil
	opts = ctrl.upstreamOptions(context.Background(),
		serverlist.Candidate{Host: "p.m1.example.net", Port: 2499}, nil)
	if opts.PinnedIP != "203.0.113.7" {
		t.Fatalf("cached PinnedIP = %q", opts.PinnedIP)
	}
}

func TestUpstreamOptionsWithoutAnyResolution(t *testing.T) {
	ctrl, _ := newTestController(t, loginOKHandler(t))
	opts := ctrl.upstreamOptions(context.Background(),
		serverlist.Candidate{Host: "edge.invalid", Port: 2499}, nil)
	if opts.PinnedIP != "" {
		t.Fatalf("PinnedIP = %q, want empty (system resolver fallback)", opts.PinnedIP)
	}
}
