package gui

import (
	"errors"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
)

func TestDispatchRegionCheck(t *testing.T) {
	f := newFakeDaemon()
	f.regionResult = api.RegionCheckResult{
		Hosts: []api.RegionHostStatus{
			{Host: "vpn.mozilla.org", Status: api.RegionBlocked},
			{Host: "profile.accounts.firefox.com", Status: api.RegionReachable},
		},
		Blocked:        []string{"vpn.mozilla.org"},
		BrowserBlocked: true,
	}
	ctrl := NewController(f, Options{})

	if err := ctrl.Dispatch(Action{Name: ActionRegionCheck}); err != nil {
		t.Fatalf("Dispatch(region_check) = %v", err)
	}
	state := ctrl.Snapshot()
	if state.RegionCheck == nil || !state.RegionCheck.BrowserBlocked || len(state.RegionCheck.Hosts) != 2 {
		t.Fatalf("RegionCheck = %+v", state.RegionCheck)
	}
	if f.regionCalls != 1 {
		t.Fatalf("region calls = %d", f.regionCalls)
	}
}

func TestDispatchPrepare(t *testing.T) {
	f := newFakeDaemon()
	f.status = api.Status{State: "IDLE", Authenticated: true, PassExpires: 1234}
	f.prepareResult = api.PrepareResult{ExpiresAt: 1234, Locations: 9, Servers: 20}
	ctrl := NewController(f, Options{})

	if err := ctrl.Dispatch(Action{Name: ActionPrepare}); err != nil {
		t.Fatalf("Dispatch(prepare) = %v", err)
	}
	state := ctrl.Snapshot()
	if state.Prepare == nil || state.Prepare.ExpiresAt != 1234 || state.Prepare.Locations != 9 {
		t.Fatalf("Prepare = %+v", state.Prepare)
	}
	if state.Status == nil || state.Status.PassExpires != 1234 {
		t.Fatalf("status pass expiry = %+v", state.Status)
	}
	if n := state.Notice; n == nil || n.Tone != toneGood {
		t.Fatalf("notice = %+v, want a good prepared notice", n)
	}
	if f.prepareCalls != 1 {
		t.Fatalf("prepare calls = %d", f.prepareCalls)
	}
}

func TestDispatchPrepareFailure(t *testing.T) {
	f := newFakeDaemon()
	f.prepareErr = errors.New("disconnect first")
	ctrl := NewController(f, Options{})
	if err := ctrl.Dispatch(Action{Name: ActionPrepare}); err == nil {
		t.Fatal("Dispatch(prepare) succeeded, want the daemon error")
	}
	if n := ctrl.Snapshot().Notice; n == nil || n.Tone != toneBad {
		t.Fatalf("notice = %+v, want bad", n)
	}
}
