package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/ipc"
)

func TestPrintSignInDone(t *testing.T) {
	tests := []struct {
		name   string
		adding bool
		want   string
	}{
		{name: "login", adding: false, want: "Signed in as user@example.com.\n"},
		{name: "add account", adding: true, want: "Added account user@example.com.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			printSignInDone(&out, tt.adding, "user@example.com")
			if got := out.String(); got != tt.want {
				t.Fatalf("printSignInDone() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBrowserRedirectPromptTrim(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "surrounding spaces", input: "  urn:example?code=a  \n", want: "urn:example?code=a"},
		{name: "crlf", input: "\turn:example?code=a\r\n", want: "urn:example?code=a"},
		{name: "blank line", input: "   \n", want: ""},
		{name: "partial line at eof", input: "urn:example?code=a", want: "urn:example?code=a"},
		{name: "empty input", input: "", wantErr: io.EOF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			line, err := promptBrowserRedirect(bufio.NewReader(strings.NewReader(tt.input)), &out)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("promptBrowserRedirect() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("promptBrowserRedirect() error = %v", err)
			}
			if line != tt.want {
				t.Fatalf("promptBrowserRedirect() = %q, want %q", line, tt.want)
			}
			if got := out.String(); got != "Redirect URL: " {
				t.Fatalf("prompt = %q", got)
			}
		})
	}
}

func TestLoginCompleteRetryTrimAndPayload(t *testing.T) {
	const wantInstruction = "After signing in, copy the complete redirect URL from the browser's address bar and paste it here."

	first := "urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel?code=bad-code&state=bad-state"
	second := "urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel?code=good-code&state=good-state"
	client, fake := newFakeLoginServer(t, fakeLoginConfig{
		completeErrs: []*api.RemoteError{
			{Code: api.CodeBadRequest, Message: "the redirect URL does not belong to this sign-in; paste the URL from this sign-in"},
			nil,
		},
	})

	var out, errBuf bytes.Buffer
	code := runBrowserSignIn(client, ipc.TypeLoginStart, ipc.TypeLoginStatus, ipc.TypeLoginCancel,
		"", "", true, false, "login",
		strings.NewReader("  "+first+"  \n"+second+"\t\n"), &out, &errBuf)

	if code != 0 {
		t.Fatalf("runBrowserSignIn() = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out.String(), errBuf.String())
	}
	if !strings.Contains(out.String(), wantInstruction) {
		t.Fatalf("stdout missing the paste instruction:\n%s", out.String())
	}
	if got := strings.Count(out.String(), wantInstruction); got != 1 {
		t.Fatalf("paste instruction printed %d times, want 1:\n%s", got, out.String())
	}
	if got := strings.Count(out.String(), "Redirect URL: "); got != 2 {
		t.Fatalf("redirect prompt printed %d times, want 2 (one per paste):\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "Signed in as user@example.com.") {
		t.Fatalf("stdout missing the success line:\n%s", out.String())
	}
	if !strings.Contains(errBuf.String(), "does not belong") {
		t.Fatalf("stderr missing the retryable error:\n%s", errBuf.String())
	}
	for _, redirect := range []string{first, second} {
		if strings.Contains(out.String(), redirect) || strings.Contains(errBuf.String(), redirect) {
			t.Fatalf("redirect URL was echoed:\nstdout:\n%s\nstderr:\n%s", out.String(), errBuf.String())
		}
	}

	completions, raws := fake.completionRecords()
	if len(completions) != 2 {
		t.Fatalf("login_complete calls = %d, want 2", len(completions))
	}
	wantRedirects := []string{first, second}
	for i, want := range wantRedirects {
		got := completions[i]
		if got.LoginID != "login-1" {
			t.Errorf("login_complete[%d] login_id = %q, want login-1", i, got.LoginID)
		}
		if got.RedirectURL != want {
			t.Errorf("login_complete[%d] redirect_url = %q, want %q", i, got.RedirectURL, want)
		}
		if got.Code != "" {
			t.Errorf("login_complete[%d] code = %q, want empty", i, got.Code)
		}
		if value, ok := raws[i]["code"]; ok && value != "" {
			t.Errorf("login_complete[%d] wire code = %v, want empty", i, value)
		}
	}

	statusIDs := fake.statusIDsSeen()
	if len(statusIDs) != 1 || statusIDs[0] != "login-1" {
		t.Fatalf("login_status ids = %v, want [login-1]", statusIDs)
	}
	if cancels := fake.cancelRecords(); len(cancels) != 0 {
		t.Fatalf("login_cancel calls = %v, want none", cancels)
	}
	wantTypes := []string{ipc.TypeLoginStart, ipc.TypeLoginComplete, ipc.TypeLoginComplete, ipc.TypeLoginStatus}
	if got := fake.typesSeen(); len(got) != len(wantTypes) {
		t.Fatalf("IPC calls = %v, want %v", got, wantTypes)
	} else {
		for i, want := range wantTypes {
			if got[i] != want {
				t.Fatalf("IPC call %d = %q, want %q", i, got[i], want)
			}
		}
	}
}

func TestLoginCompleteTerminalError(t *testing.T) {
	const terminal = "the browser authorization code was rejected or already used; start again"
	client, fake := newFakeLoginServer(t, fakeLoginConfig{
		statuses: []api.LoginState{{
			Step: "error", Method: "browser", LoginID: "login-1", Error: terminal,
		}},
	})

	var out, errBuf bytes.Buffer
	code := runBrowserSignIn(client, ipc.TypeLoginStart, ipc.TypeLoginStatus, ipc.TypeLoginCancel,
		"", "", true, false, "login",
		strings.NewReader("urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel?code=used&state=state\n"), &out, &errBuf)

	if code == 0 {
		t.Fatalf("runBrowserSignIn() = 0, want nonzero")
	}
	if !strings.Contains(errBuf.String(), terminal) {
		t.Fatalf("stderr missing terminal error:\n%s", errBuf.String())
	}
	if strings.Contains(out.String(), "Signed in") {
		t.Fatalf("terminal error reported success:\n%s", out.String())
	}
	if cancels := fake.cancelRecords(); len(cancels) != 0 {
		t.Fatalf("terminal error cancelled the flow again: %v", cancels)
	}
}

func TestRunBrowserSignInCancelled(t *testing.T) {
	client, _ := newFakeLoginServer(t, fakeLoginConfig{
		statuses: []api.LoginState{{Step: "cancelled", Method: "browser", LoginID: "login-1"}},
	})

	var out, errBuf bytes.Buffer
	code := runBrowserSignIn(client, ipc.TypeLoginStart, ipc.TypeLoginStatus, ipc.TypeLoginCancel,
		"", "", true, false, "login",
		strings.NewReader("urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel?code=a&state=s\n"), &out, &errBuf)

	if code == 0 {
		t.Fatalf("runBrowserSignIn() = 0, want nonzero")
	}
	if !strings.Contains(errBuf.String(), "Sign-in cancelled.") {
		t.Fatalf("stderr missing cancellation:\n%s", errBuf.String())
	}
}

func TestRunBrowserSignInInputClosed(t *testing.T) {
	client, fake := newFakeLoginServer(t, fakeLoginConfig{})

	var out, errBuf bytes.Buffer
	code := runBrowserSignIn(client, ipc.TypeLoginStart, ipc.TypeLoginStatus, ipc.TypeLoginCancel,
		"", "", true, false, "login", strings.NewReader(""), &out, &errBuf)

	if code == 0 {
		t.Fatalf("runBrowserSignIn() = 0, want nonzero")
	}
	if !strings.Contains(errBuf.String(), "no redirect URL was pasted") {
		t.Fatalf("stderr missing input error:\n%s", errBuf.String())
	}
	cancels := fake.cancelRecords()
	if len(cancels) != 1 || cancels[0].LoginID != "login-1" {
		t.Fatalf("login_cancel calls = %v, want one for login-1", cancels)
	}
}

// fakeLoginConfig scripts one fake daemon connection for the browser flow.
type fakeLoginConfig struct {
	start        api.LoginState
	completeErrs []*api.RemoteError
	statuses     []api.LoginState
}

// fakeLoginServer is an in-process IPC daemon that records the CLI's calls.
type fakeLoginServer struct {
	start        api.LoginState
	completeErrs []*api.RemoteError
	statuses     []api.LoginState

	mu            sync.Mutex
	completeCalls int
	statusCalls   int
	seenTypes     []string
	completions   []api.LoginCompletePayload
	rawPayloads   []map[string]any
	statusIDs     []string
	cancels       []api.LoginCancelPayload
}

func newFakeLoginServer(t *testing.T, cfg fakeLoginConfig) (*ipc.Client, *fakeLoginServer) {
	t.Helper()
	if cfg.start.Step == "" {
		cfg.start = api.LoginState{
			Step: "browser", Method: "browser", LoginID: "login-1",
			URL: "https://accounts.example.test/authorize?client_id=test",
		}
	}
	if len(cfg.statuses) == 0 {
		cfg.statuses = []api.LoginState{{
			Step: "done", Method: "browser", LoginID: cfg.start.LoginID, Email: "user@example.com",
		}}
	}
	fake := &fakeLoginServer{start: cfg.start, completeErrs: cfg.completeErrs, statuses: cfg.statuses}

	path := filepath.Join(t.TempDir(), "control.sock")
	srv, err := ipc.UnixTransport{}.Listen(path)
	if err != nil {
		if runtime.GOOS != "linux" {
			t.Skipf("unix sockets unavailable on this platform: %v", err)
		}
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})
	go srv.Serve(ctx, func(_ context.Context, conn *ipc.Conn) error {
		for {
			env, err := conn.Read()
			if err != nil {
				return nil
			}
			if err := fake.handle(conn, env); err != nil {
				return err
			}
		}
	})

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	client, err := ipc.UnixTransport{}.Dial(dialCtx, path)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client, fake
}

func (f *fakeLoginServer) handle(conn *ipc.Conn, env *ipc.Envelope) error {
	f.mu.Lock()
	f.seenTypes = append(f.seenTypes, env.Type)
	f.mu.Unlock()

	switch env.Type {
	case ipc.TypeLoginStart, ipc.TypeAccountsAddStart:
		var p api.LoginStartPayload
		if err := env.DecodePayload(&p); err != nil {
			return replyFakeError(conn, env, err)
		}
		return conn.Reply(env.ID, ipc.TypeLoginState, f.start)

	case ipc.TypeLoginComplete:
		var p api.LoginCompletePayload
		if err := env.DecodePayload(&p); err != nil {
			return replyFakeError(conn, env, err)
		}
		var raw map[string]any
		if err := json.Unmarshal(env.Payload, &raw); err != nil {
			return replyFakeError(conn, env, err)
		}
		f.mu.Lock()
		index := f.completeCalls
		f.completeCalls++
		f.completions = append(f.completions, p)
		f.rawPayloads = append(f.rawPayloads, raw)
		var failure *api.RemoteError
		if len(f.completeErrs) > 0 {
			at := index
			if at >= len(f.completeErrs) {
				at = len(f.completeErrs) - 1
			}
			failure = f.completeErrs[at]
		}
		f.mu.Unlock()
		if failure != nil {
			return conn.Reply(env.ID, ipc.TypeError, *failure)
		}
		return conn.Reply(env.ID, ipc.TypeResult, api.Result{OK: true})

	case ipc.TypeLoginStatus, ipc.TypeAccountsAddStatus:
		var p api.LoginStatusPayload
		if err := env.DecodePayload(&p); err != nil {
			return replyFakeError(conn, env, err)
		}
		f.mu.Lock()
		index := f.statusCalls
		f.statusCalls++
		f.statusIDs = append(f.statusIDs, p.LoginID)
		at := index
		if at >= len(f.statuses) {
			at = len(f.statuses) - 1
		}
		state := f.statuses[at]
		f.mu.Unlock()
		return conn.Reply(env.ID, ipc.TypeLoginState, state)

	case ipc.TypeLoginCancel, ipc.TypeAccountsAddCancel:
		var p api.LoginCancelPayload
		if err := env.DecodePayload(&p); err != nil {
			return replyFakeError(conn, env, err)
		}
		f.mu.Lock()
		f.cancels = append(f.cancels, p)
		f.mu.Unlock()
		return conn.Reply(env.ID, ipc.TypeResult, api.Result{OK: true})

	default:
		return conn.Reply(env.ID, ipc.TypeError, api.RemoteError{Code: api.CodeBadRequest, Message: "unexpected " + env.Type})
	}
}

func replyFakeError(conn *ipc.Conn, env *ipc.Envelope, err error) error {
	return conn.Reply(env.ID, ipc.TypeError, api.RemoteError{Code: api.CodeBadRequest, Message: err.Error()})
}

func (f *fakeLoginServer) completionRecords() ([]api.LoginCompletePayload, []map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	completions := append([]api.LoginCompletePayload(nil), f.completions...)
	raws := append([]map[string]any(nil), f.rawPayloads...)
	return completions, raws
}

func (f *fakeLoginServer) statusIDsSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.statusIDs...)
}

func (f *fakeLoginServer) typesSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seenTypes...)
}

func (f *fakeLoginServer) cancelRecords() []api.LoginCancelPayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]api.LoginCancelPayload(nil), f.cancels...)
}
