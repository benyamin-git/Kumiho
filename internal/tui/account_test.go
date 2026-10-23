package tui

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

type tuiRequest struct {
	typ     string
	payload []byte
}

type tuiFakeDaemon struct {
	client *ipc.Client
	mu     sync.Mutex
	reqs   []tuiRequest
}

func (d *tuiFakeDaemon) requests() []tuiRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]tuiRequest(nil), d.reqs...)
}

func (d *tuiFakeDaemon) types() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.reqs))
	for i, r := range d.reqs {
		out[i] = r.typ
	}
	return out
}

// newTUIFakeDaemon starts a control server for command tests. It skips on
// platforms where the default transport cannot bind the temp endpoint.
func newTUIFakeDaemon(t *testing.T, reply func(tuiRequest) (string, any)) *tuiFakeDaemon {
	t.Helper()
	endpoint := filepath.Join(t.TempDir(), "control.sock")
	srv, err := ipc.DefaultTransport().Listen(endpoint)
	if err != nil {
		t.Skipf("control transport unavailable: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = srv.Close()
	})
	d := &tuiFakeDaemon{}
	go srv.Serve(ctx, func(ctx context.Context, conn *ipc.Conn) error {
		for {
			env, err := conn.Read()
			if err != nil {
				return nil
			}
			var payload []byte
			if env.Payload != nil {
				payload = env.Payload
			}
			req := tuiRequest{typ: env.Type, payload: payload}
			d.mu.Lock()
			d.reqs = append(d.reqs, req)
			d.mu.Unlock()
			typ, body := reply(req)
			if typ == "" {
				continue
			}
			if err := conn.Reply(env.ID, typ, body); err != nil {
				return nil
			}
		}
	})
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	client, err := ipc.DefaultTransport().Dial(dialCtx, endpoint)
	if err != nil {
		t.Fatalf("dial fake daemon: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	d.client = client
	return d
}

func accountTestView() api.AccountsView {
	return api.AccountsView{
		Accounts: []api.AccountSummary{
			{
				ID: "id-1", Email: "user@example.com", Method: "browser", Active: true,
				Quota: &api.Quota{Limit: accountTestI64(100), Remaining: accountTestI64(80)},
			},
			{
				ID: "id-2", Email: "other@example.com", Label: "Work", Method: "password",
				ReauthRequired: true,
			},
		},
		Active: "id-1", Threshold: 10,
	}
}

func accountTestI64(v int64) *int64 { return &v }

func TestAccountScreenListsAndMarksActive(t *testing.T) {
	m := &Model{
		screen: screenAccount,
		ac:     accountState{loaded: true, view: accountTestView()},
	}
	view := m.View()
	for _, want := range []string{"user@example.com", "other@example.com", "* user@example.com", "needs sign-in", "80% left"} {
		if !strings.Contains(view, want) {
			t.Errorf("account view missing %q:\n%s", want, view)
		}
	}
}

func TestAccountQuota(t *testing.T) {
	limit := accountTestI64(100)
	tests := []struct {
		name string
		q    *api.Quota
		want string
	}{
		{name: "nil", q: nil, want: "quota unknown"},
		{name: "exhausted", q: &api.Quota{Limit: limit, Remaining: accountTestI64(0)}, want: "quota exhausted"},
		{name: "over limit", q: &api.Quota{Limit: limit, Remaining: accountTestI64(120)}, want: "100% left"},
		{name: "normal", q: &api.Quota{Limit: limit, Remaining: accountTestI64(80)}, want: "80% left"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := accountQuota(tt.q); got != tt.want {
				t.Fatalf("accountQuota() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAccountScreenShowsDaemonNoticeAndError(t *testing.T) {
	view := accountTestView()
	view.Notice = "skipping other@example.com: sign in again"
	view.Error = "accounts: token store unavailable"
	m := &Model{
		screen: screenAccount,
		ac:     accountState{loaded: true, view: view},
	}
	rendered := m.View()
	for _, want := range []string{view.Notice, view.Error} {
		if !strings.Contains(rendered, want) {
			t.Errorf("account view missing %q:\n%s", want, rendered)
		}
	}
}

func TestAccountScreenKeys(t *testing.T) {
	d := newTUIFakeDaemon(t, func(req tuiRequest) (string, any) {
		switch req.typ {
		case ipc.TypeAccountsUse:
			return ipc.TypeResult, api.Result{OK: true}
		case ipc.TypeAccountsUpdate:
			return ipc.TypeResult, api.Result{OK: true}
		case ipc.TypeAccountsSetRotation:
			return ipc.TypeResult, api.Result{OK: true}
		default:
			return "", nil
		}
	})
	m := &Model{
		client: d.client,
		screen: screenAccount,
		ac:     accountState{loaded: true, view: accountTestView()},
	}

	_, cmd := m.handleAccountKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if cmd == nil {
		t.Fatal("u should emit accounts_use")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("use command returned no message")
	}
	requests := d.requests()
	if len(requests) != 1 || requests[0].typ != ipc.TypeAccountsUse {
		t.Fatalf("requests = %v, want accounts_use", d.types())
	}

	m.handleAccountKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if m.ac.confirmRemove == nil || m.ac.confirmRemove.ID != "id-1" {
		t.Fatalf("d should open remove confirmation for selected account: %+v", m.ac.confirmRemove)
	}
	m.handleAccountKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.ac.confirmRemove != nil {
		t.Fatal("n should cancel the remove confirmation")
	}

	m.handleAccountKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if !m.ac.rename {
		t.Fatal("n should open a rename prompt")
	}
	m.ac.renameText = "Work"
	_, cmd = m.handleAccountKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter in rename should emit accounts_update")
	}
	_ = cmd()
	requests = d.requests()
	if len(requests) < 2 || requests[len(requests)-1].typ != ipc.TypeAccountsUpdate {
		t.Fatalf("requests = %v, want accounts_update", d.types())
	}

	_, cmd = m.handleAccountKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if cmd == nil {
		t.Fatal("t should emit accounts_set_rotation")
	}
	_ = cmd()
	_, cmd = m.handleAccountKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("+")})
	if cmd == nil {
		t.Fatal("+ should emit accounts_set_rotation")
	}
	_ = cmd()
	requests = d.requests()
	if len(requests) < 4 {
		t.Fatalf("requests = %v, want use/update/rotation/rotation", d.types())
	}
}

func TestAccountBrowserWaitingShowsURL(t *testing.T) {
	d := newTUIFakeDaemon(t, func(req tuiRequest) (string, any) {
		if req.typ == ipc.TypeAccountsAddCancel {
			return ipc.TypeResult, api.Result{OK: true}
		}
		return "", nil
	})
	m := &Model{
		client: d.client,
		screen: screenAccount,
		ac: accountState{
			loaded: true, view: accountTestView(), adding: true,
			polling: true, browserURL: "https://example.test/signin", browserID: "login-1",
		},
	}
	view := m.View()
	if !strings.Contains(view, "https://example.test/signin") || !strings.Contains(view, "press Esc to cancel") {
		t.Fatalf("browser waiting card missing URL or cancel hint:\n%s", view)
	}
	_, cmd := m.handleAccountKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should cancel the browser flow")
	}
	_ = cmd()
	if got := d.types(); len(got) != 1 || got[0] != ipc.TypeAccountsAddCancel {
		t.Fatalf("requests = %v, want accounts_add_cancel", got)
	}
}

func TestAccountReauthUsesTargetID(t *testing.T) {
	d := newTUIFakeDaemon(t, func(req tuiRequest) (string, any) {
		if req.typ == ipc.TypeAccountsAddStart {
			return ipc.TypeLoginState, api.LoginState{
				Step: "browser", Method: "browser", URL: "https://example.test/reauth", LoginID: "login-2",
			}
		}
		return "", nil
	})
	m := &Model{
		client: d.client,
		screen: screenAccount,
		ac:     accountState{loaded: true, view: accountTestView(), selected: 1},
	}
	_, cmd := m.handleAccountKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	if cmd == nil {
		t.Fatal("R should start browser re-authentication")
	}
	if msg, ok := cmd().(browserStartMsg); !ok || msg.state.Step != "browser" {
		t.Fatalf("R command message = %#v", msg)
	}
	requests := d.requests()
	if len(requests) != 1 || requests[0].typ != ipc.TypeAccountsAddStart {
		t.Fatalf("requests = %v, want accounts_add_start", d.types())
	}
	var payload api.LoginStartPayload
	if err := envPayload(requests[0].payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Method != "browser" || payload.AccountID != "id-2" {
		t.Fatalf("start payload = %+v, want browser/id-2", payload)
	}
}

func TestAccountRemoveActiveCopy(t *testing.T) {
	view := accountTestView()
	m := &Model{screen: screenAccount, ac: accountState{loaded: true, view: view}}
	m.handleAccountKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if !strings.Contains(m.View(), "next account will be used") {
		t.Fatalf("active-remove copy missing next-account hint:\n%s", m.View())
	}

	one := view
	one.Accounts = view.Accounts[:1]
	m = &Model{screen: screenAccount, ac: accountState{loaded: true, view: one}}
	m.handleAccountKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if !strings.Contains(m.View(), "this signs out") {
		t.Fatalf("last-remove copy missing sign-out hint:\n%s", m.View())
	}
}

func TestLoginScreenOffersBrowserAndPassword(t *testing.T) {
	m := &Model{screen: screenLogin, step: "method"}
	view := m.View()
	if !strings.Contains(view, "browser") || !strings.Contains(view, "password") {
		t.Fatalf("login method choice missing:\n%s", view)
	}
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if cmd != nil || m.step != "email" {
		t.Fatalf("p should keep the email form: step=%q cmd=%v", m.step, cmd)
	}

	d := newTUIFakeDaemon(t, func(req tuiRequest) (string, any) {
		if req.typ == ipc.TypeLoginStart {
			return ipc.TypeLoginState, api.LoginState{
				Step: "browser", Method: "browser", URL: "https://example.test/login", LoginID: "login-3",
			}
		}
		return "", nil
	})
	m = &Model{client: d.client, screen: screenLogin, step: "method"}
	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	if cmd == nil {
		t.Fatal("b should start browser sign-in")
	}
	_ = cmd()
	requests := d.requests()
	if len(requests) != 1 || requests[0].typ != ipc.TypeLoginStart {
		t.Fatalf("requests = %v, want login_start", d.types())
	}
	var payload api.LoginStartPayload
	if err := envPayload(requests[0].payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Method != "browser" {
		t.Fatalf("method = %q, want browser", payload.Method)
	}
}

// envPayload decodes a JSON payload captured from the fake daemon.
func envPayload(raw []byte, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}
