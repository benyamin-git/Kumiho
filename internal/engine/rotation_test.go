package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/benyamin-git/kumiho/internal/api"
	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/guardian"
	"github.com/benyamin-git/kumiho/internal/settings"
	"github.com/benyamin-git/kumiho/internal/upstream"
)

type quotaReply struct {
	status    int
	limit     *int64
	remaining *int64
	reset     *int64
}

type quotaTracker struct {
	mu   sync.Mutex
	head map[string]int
	get  map[string]int
}

func (q *quotaTracker) record(method, token string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if method == http.MethodHead {
		q.head[token]++
	} else {
		q.get[token]++
	}
}

func (q *quotaTracker) heads(token string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.head[token]
}

func (q *quotaTracker) total() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	total := 0
	for _, n := range q.head {
		total += n
	}
	for _, n := range q.get {
		total += n
	}
	return total
}

func quotaInt(v int64) *int64 { return &v }

func newQuotaServer(t *testing.T, replies map[string]quotaReply) (*httptest.Server, *quotaTracker) {
	t.Helper()
	tracker := &quotaTracker{head: make(map[string]int), get: make(map[string]int)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/fpn/token" {
			http.NotFound(w, r)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		tracker.record(r.Method, token)
		reply, ok := replies[token]
		if !ok {
			http.NotFound(w, r)
			return
		}
		status := reply.status
		if status == 0 {
			status = http.StatusOK
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		if reply.limit != nil {
			w.Header().Set("X-Quota-Limit", strconv.FormatInt(*reply.limit, 10))
		}
		if reply.remaining != nil {
			w.Header().Set("X-Quota-Remaining", strconv.FormatInt(*reply.remaining, 10))
		}
		if reply.reset != nil {
			w.Header().Set("X-Quota-Reset", strconv.FormatInt(*reply.reset, 10))
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "pass-" + token})
	}))
	t.Cleanup(srv.Close)
	return srv, tracker
}

func rotationPasswordAccount(id, email, access string, lastUsed time.Time) *Account {
	now := time.Now()
	return &Account{
		ID:         id,
		Email:      email,
		Label:      email,
		Method:     MethodPassword,
		FxA:        &fxa.Tokens{AccessToken: access, RefreshToken: "refresh-" + id, ExpiresAt: now.Add(2 * time.Hour).Unix()},
		AddedAt:    now.Add(-24 * time.Hour),
		LastUsedAt: lastUsed,
	}
}

func rotationBrowserAccount(id, email, access string, expires int64, reauth bool, lastUsed time.Time) *Account {
	now := time.Now()
	return &Account{
		ID:         id,
		Email:      email,
		Label:      email,
		Method:     MethodBrowser,
		Browser:    &BrowserToken{Token: access, ExpiresAt: expires, ReauthRequired: reauth},
		AddedAt:    now.Add(-24 * time.Hour),
		LastUsedAt: lastUsed,
	}
}

func enableRotation(t *testing.T, ctrl *Controller, threshold int) {
	t.Helper()
	if threshold <= 0 {
		threshold = 10
	}
	if err := ctrl.store.SetOverride("account_rotation", "true"); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.store.SetOverride("rotation_threshold", strconv.Itoa(threshold)); err != nil {
		t.Fatal(err)
	}
}

func newRotationController(t *testing.T, accounts []*Account, activeID string, replies map[string]quotaReply, threshold int) (*Controller, settings.Paths, *quotaTracker) {
	t.Helper()
	ctrl, paths := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	enableRotation(t, ctrl, threshold)
	srv, tracker := newQuotaServer(t, replies)
	ctrl.guardian = &guardian.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	f := &accountsFile{Version: 2, Active: activeID, LastMethod: MethodPassword, Accounts: accounts}
	if err := saveAccounts(accountsPath(paths), f); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctrl.mu.Lock()
	ctrl.setStateLocked(StateConnecting)
	ctrl.mu.Unlock()
	t.Cleanup(func() {
		ctrl.mu.Lock()
		if ctrl.state == StateConnecting || ctrl.state == StateFatal {
			ctrl.setStateLocked(StateIdle)
		}
		ctrl.mu.Unlock()
	})
	return ctrl, paths, tracker
}

func selectActiveID(t *testing.T, ctrl *Controller) string {
	t.Helper()
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	return ctrl.activeID
}

func TestRotationSwitchesWhenActiveBelowThreshold(t *testing.T) {
	now := time.Now()
	a := rotationPasswordAccount("a", "a@example.com", "token-a", now)
	b := rotationPasswordAccount("b", "b@example.com", "token-b", now.Add(-time.Hour))
	replies := map[string]quotaReply{
		"token-a": {limit: quotaInt(100), remaining: quotaInt(5)},
		"token-b": {limit: quotaInt(100), remaining: quotaInt(80)},
	}
	ctrl, paths, _ := newRotationController(t, []*Account{a, b}, "a", replies, 10)
	if err := ctrl.selectAccount(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := selectActiveID(t, ctrl); got != "b" {
		t.Fatalf("active account = %q, want b", got)
	}
	tokens, err := loadTokens(paths.Tokens)
	if err != nil || tokens == nil || tokens.AccessToken != "token-b" {
		t.Fatalf("tokens.json = %+v, err = %v", tokens, err)
	}
	view := ctrl.Accounts()
	if view.Active != "b" || view.Notice != "" {
		t.Fatalf("accounts view = %+v", view)
	}
	for _, email := range []string{"a@example.com", "b@example.com"} {
		summary := findAccountInView(view, email)
		if summary == nil || summary.Quota == nil || summary.Quota.Remaining == nil {
			t.Fatalf("summary for %s = %+v", email, summary)
		}
	}
}

func TestRotationKeepsActiveAboveThreshold(t *testing.T) {
	now := time.Now()
	a := rotationPasswordAccount("a", "a@example.com", "token-a", now)
	b := rotationPasswordAccount("b", "b@example.com", "token-b", now.Add(-time.Hour))
	replies := map[string]quotaReply{
		"token-a": {limit: quotaInt(100), remaining: quotaInt(80)},
		"token-b": {limit: quotaInt(100), remaining: quotaInt(100)},
	}
	ctrl, paths, _ := newRotationController(t, []*Account{a, b}, "a", replies, 10)
	if err := ctrl.selectAccount(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := selectActiveID(t, ctrl); got != "a" {
		t.Fatalf("active account = %q, want a", got)
	}
	tokens, err := loadTokens(paths.Tokens)
	if err != nil || tokens == nil || tokens.AccessToken != "token-a" {
		t.Fatalf("tokens.json = %+v, err = %v", tokens, err)
	}
}

func TestRotationFetchesQuotasConcurrently(t *testing.T) {
	now := time.Now()
	a := rotationPasswordAccount("a", "a@example.com", "token-a", now)
	b := rotationPasswordAccount("b", "b@example.com", "token-b", now.Add(-time.Hour))
	starts := make(chan string, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/fpn/token" {
			http.NotFound(w, r)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		starts <- token
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		remaining := "80"
		if token == "token-a" {
			remaining = "5"
		}
		w.Header().Set("X-Quota-Limit", "100")
		w.Header().Set("X-Quota-Remaining", remaining)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(releaseAll)

	ctrl, paths := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	enableRotation(t, ctrl, 10)
	ctrl.guardian = &guardian.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	f := &accountsFile{Version: 2, Active: "a", LastMethod: MethodPassword, Accounts: []*Account{a, b}}
	if err := saveAccounts(accountsPath(paths), f); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctrl.mu.Lock()
	ctrl.setStateLocked(StateConnecting)
	ctrl.mu.Unlock()
	t.Cleanup(func() {
		ctrl.mu.Lock()
		if ctrl.state == StateConnecting || ctrl.state == StateFatal {
			ctrl.setStateLocked(StateIdle)
		}
		ctrl.mu.Unlock()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- ctrl.selectAccount(ctx) }()
	for i := 0; i < 2; i++ {
		select {
		case <-starts:
		case <-time.After(time.Second):
			t.Fatalf("quota requests did not overlap; only %d started", i)
		}
	}
	releaseAll()
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if got := selectActiveID(t, ctrl); got != "b" {
		t.Fatalf("active account = %q, want b", got)
	}
}

func TestRotationQuotaTimeoutIsUnknown(t *testing.T) {
	now := time.Now()
	a := rotationPasswordAccount("a", "a@example.com", "token-a", now)
	b := rotationPasswordAccount("b", "b@example.com", "token-b", now.Add(-time.Hour))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/fpn/token" {
			http.NotFound(w, r)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if token == "token-a" {
			<-r.Context().Done()
			return
		}
		w.Header().Set("X-Quota-Limit", "100")
		w.Header().Set("X-Quota-Remaining", "90")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ctrl, paths := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	enableRotation(t, ctrl, 10)
	ctrl.rotationQuotaTimeout = 50 * time.Millisecond
	ctrl.rotationBudget = 2 * time.Second
	ctrl.guardian = &guardian.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	f := &accountsFile{Version: 2, Active: "a", LastMethod: MethodPassword, Accounts: []*Account{a, b}}
	if err := saveAccounts(accountsPath(paths), f); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctrl.mu.Lock()
	ctrl.setStateLocked(StateConnecting)
	ctrl.mu.Unlock()
	t.Cleanup(func() {
		ctrl.mu.Lock()
		if ctrl.state == StateConnecting || ctrl.state == StateFatal {
			ctrl.setStateLocked(StateIdle)
		}
		ctrl.mu.Unlock()
	})

	started := time.Now()
	if err := ctrl.selectAccount(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("rotation waited %v for a stalled quota lookup", elapsed)
	}
	if got := selectActiveID(t, ctrl); got != "b" {
		t.Fatalf("active account = %q, want b", got)
	}
	if view := ctrl.Accounts(); !strings.Contains(view.Notice, "quota unknown for a@example.com") {
		t.Fatalf("rotation notice = %q", view.Notice)
	}
}

func TestRotationSkipsReauthAndExpiredBrowser(t *testing.T) {
	now := time.Now()
	a := rotationBrowserAccount("a", "reauth@example.com", "reauth-token", now.Add(time.Hour).Unix(), true, now)
	b := rotationBrowserAccount("b", "expired@example.com", "expired-token", now.Add(-time.Hour).Unix(), false, now.Add(-time.Hour))
	c := rotationPasswordAccount("c", "valid@example.com", "valid-token", now.Add(-2*time.Hour))
	replies := map[string]quotaReply{
		"valid-token": {limit: quotaInt(100), remaining: quotaInt(80)},
	}
	ctrl, paths, tracker := newRotationController(t, []*Account{a, b, c}, "a", replies, 10)
	if err := ctrl.selectAccount(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := selectActiveID(t, ctrl); got != "c" {
		t.Fatalf("active account = %q, want c", got)
	}
	if tracker.heads("reauth-token") != 0 || tracker.heads("expired-token") != 0 {
		t.Fatalf("fetched quota for skipped browsers: reauth=%d expired=%d",
			tracker.heads("reauth-token"), tracker.heads("expired-token"))
	}
	if tracker.heads("valid-token") != 1 {
		t.Fatalf("valid account HEAD calls = %d, want 1", tracker.heads("valid-token"))
	}
	view := ctrl.Accounts()
	if !strings.Contains(view.Notice, "reauth@example.com") || !strings.Contains(view.Notice, "expired@example.com") {
		t.Fatalf("rotation notice = %q", view.Notice)
	}
	loaded, _, err := loadAccounts(accountsPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	expired := findAccountLocked(loaded, "b")
	if expired == nil || expired.Browser == nil || !expired.Browser.ReauthRequired {
		t.Fatalf("expired browser account = %+v", expired)
	}
}

func TestRotationTieBreaksLeastRecentlyUsed(t *testing.T) {
	now := time.Now()
	a := rotationPasswordAccount("a", "a@example.com", "token-a", now)
	b := rotationPasswordAccount("b", "b@example.com", "token-b", now.Add(-2*time.Hour))
	c := rotationPasswordAccount("c", "c@example.com", "token-c", now.Add(-time.Hour))
	replies := map[string]quotaReply{
		"token-a": {limit: quotaInt(100), remaining: quotaInt(0)},
		"token-b": {limit: quotaInt(100), remaining: quotaInt(80)},
		"token-c": {limit: quotaInt(100), remaining: quotaInt(80)},
	}
	ctrl, _, _ := newRotationController(t, []*Account{a, b, c}, "a", replies, 10)
	if err := ctrl.selectAccount(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := selectActiveID(t, ctrl); got != "b" {
		t.Fatalf("active account = %q, want least recently used b", got)
	}
}

func TestRotationUnknownQuotaSwitchesToKnown(t *testing.T) {
	now := time.Now()
	a := rotationPasswordAccount("a", "a@example.com", "token-a", now)
	b := rotationPasswordAccount("b", "b@example.com", "token-b", now.Add(-time.Hour))
	replies := map[string]quotaReply{
		"token-a": {status: http.StatusInternalServerError},
		"token-b": {limit: quotaInt(100), remaining: quotaInt(80)},
	}
	ctrl, _, _ := newRotationController(t, []*Account{a, b}, "a", replies, 10)
	if err := ctrl.selectAccount(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := selectActiveID(t, ctrl); got != "b" {
		t.Fatalf("active account = %q, want b", got)
	}
}

func TestRotationUnknownQuotaKeepsActiveWhenNoKnown(t *testing.T) {
	now := time.Now()
	a := rotationPasswordAccount("a", "a@example.com", "token-a", now)
	b := rotationPasswordAccount("b", "b@example.com", "token-b", now.Add(-time.Hour))
	replies := map[string]quotaReply{
		"token-a": {status: http.StatusInternalServerError},
		"token-b": {status: http.StatusInternalServerError},
	}
	ctrl, _, _ := newRotationController(t, []*Account{a, b}, "a", replies, 10)
	if err := ctrl.selectAccount(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := selectActiveID(t, ctrl); got != "a" {
		t.Fatalf("active account = %q, want a", got)
	}
}

func TestRotationAllExhaustedFailsConnect(t *testing.T) {
	now := time.Now()
	a := rotationPasswordAccount("a", "a@example.com", "token-a", now)
	b := rotationPasswordAccount("b", "b@example.com", "token-b", now.Add(-time.Hour))
	replies := map[string]quotaReply{
		"token-a": {limit: quotaInt(100), remaining: quotaInt(0)},
		"token-b": {limit: quotaInt(100), remaining: quotaInt(0)},
	}
	ctrl, paths := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	enableRotation(t, ctrl, 10)
	srv, _ := newQuotaServer(t, replies)
	ctrl.guardian = &guardian.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if err := saveAccounts(accountsPath(paths), &accountsFile{
		Version: 2, Active: "a", LastMethod: MethodPassword, Accounts: []*Account{a, b},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ctrl.stopTunnel("test cleanup") })
	var dials atomic.Int32
	ctrl.dialUpstream = func(context.Context, upstream.Options) (upstreamSession, error) {
		dials.Add(1)
		return nil, errors.New("must not dial")
	}

	err := ctrl.Connect(context.Background(), api.ConnectPayload{ProxyOnly: true})
	var fatal *fatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("Connect error = %v, want fatal quota error", err)
	}
	for _, email := range []string{"a@example.com", "b@example.com"} {
		if !strings.Contains(err.Error(), email) {
			t.Fatalf("Connect error %q does not name %s", err, email)
		}
	}
	if dials.Load() != 0 {
		t.Fatalf("dialed an edge %d times despite exhausted quota", dials.Load())
	}
	if st := ctrl.Snapshot(); st.State != string(StateFatal) {
		t.Fatalf("state = %q, want fatal", st.State)
	}
}

func TestQuotaPollUpdatesSnapshot(t *testing.T) {
	now := time.Now()
	a := rotationPasswordAccount("poll", "poll@example.com", "poll-token", now)
	ctrl, _, _ := newQuotaPollController(t, a, quotaReply{limit: quotaInt(100), remaining: quotaInt(42)})
	if err := ctrl.pollQuotaOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := ctrl.Snapshot()
	if st.Quota == nil || st.Quota.Remaining == nil || *st.Quota.Remaining != 42 {
		t.Fatalf("snapshot quota = %+v", st.Quota)
	}
	summary := findAccountInView(ctrl.Accounts(), "poll@example.com")
	if summary == nil || summary.Quota == nil || summary.Quota.Remaining == nil || *summary.Quota.Remaining != 42 {
		t.Fatalf("account summary quota = %+v", summary)
	}
}

func TestQuotaPoll401MarksReauthAndStops(t *testing.T) {
	now := time.Now()
	a := rotationBrowserAccount("browser", "browser@example.com", "browser-token", now.Add(time.Hour).Unix(), false, now)
	ctrl, paths, _ := newQuotaPollController(t, a, quotaReply{status: http.StatusUnauthorized})
	err := ctrl.pollQuotaOnce(context.Background())
	if !errors.Is(err, errQuotaReauth) {
		t.Fatalf("poll error = %v, want errQuotaReauth", err)
	}
	if st := ctrl.Snapshot(); st.State != string(StateConnected) {
		t.Fatalf("state after quota 401 = %q", st.State)
	}
	loaded, _, err := loadAccounts(accountsPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	account := findAccountLocked(loaded, "browser")
	if account == nil || account.Browser == nil || !account.Browser.ReauthRequired {
		t.Fatalf("browser reauth flag = %+v", account)
	}

	fresh := rotationBrowserAccount("browser", "fresh@example.com", "fresh-token", now.Add(time.Hour).Unix(), false, now)
	poller, _, pollerTracker := newQuotaPollController(t, fresh, quotaReply{status: http.StatusUnauthorized})
	oldUnit := quotaPollUnit
	quotaPollUnit = time.Millisecond
	defer func() { quotaPollUnit = oldUnit }()
	if err := poller.store.SetOverride("quota_poll_minutes", "1"); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	poller.startQuotaPoll(stop)
	deadline := time.Now().Add(time.Second)
	for pollerTracker.total() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if pollerTracker.total() != 1 {
		t.Fatalf("quota requests after 401 = %d, want 1", pollerTracker.total())
	}
	time.Sleep(30 * time.Millisecond)
	if pollerTracker.total() != 1 {
		t.Fatalf("poll continued after reauth: requests = %d", pollerTracker.total())
	}
	close(stop)
}

func TestQuotaPoll401RefreshesPasswordOnce(t *testing.T) {
	now := time.Now()
	a := rotationPasswordAccount("poll", "poll@example.com", "old-token", now)
	var refreshCalls atomic.Int32
	ctrl, paths := newTestController(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/oauth/token" {
			http.NotFound(w, r)
			return
		}
		refreshCalls.Add(1)
		fmt.Fprint(w, `{"access_token":"new-token","refresh_token":"refresh-poll","expires_in":3600}`)
	})
	srv, tracker := newQuotaServer(t, map[string]quotaReply{
		"old-token": {status: http.StatusUnauthorized},
		"new-token": {limit: quotaInt(100), remaining: quotaInt(42)},
	})
	ctrl.guardian = &guardian.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if err := saveAccounts(accountsPath(paths), &accountsFile{
		Version: 2, Active: a.ID, LastMethod: a.Method, Accounts: []*Account{a},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctrl.mu.Lock()
	ctrl.setStateLocked(StateConnected)
	ctrl.mu.Unlock()
	t.Cleanup(func() { ctrl.stopTunnel("test cleanup") })

	if err := ctrl.pollQuotaOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls.Load())
	}
	if tracker.heads("old-token") != 1 || tracker.heads("new-token") != 1 {
		t.Fatalf("quota HEAD calls: old=%d new=%d", tracker.heads("old-token"), tracker.heads("new-token"))
	}
	st := ctrl.Snapshot()
	if st.Quota == nil || st.Quota.Remaining == nil || *st.Quota.Remaining != 42 {
		t.Fatalf("quota after refresh = %+v", st.Quota)
	}
	account := ctrl.accounts
	if account == nil || len(account.Accounts) != 1 || account.Accounts[0].FxA == nil ||
		account.Accounts[0].FxA.AccessToken != "new-token" {
		t.Fatalf("refreshed account = %+v", account)
	}
}

func TestQuotaPoll429KeepsTunnel(t *testing.T) {
	now := time.Now()
	a := rotationPasswordAccount("poll", "poll@example.com", "poll-token", now)
	ctrl, _, _ := newQuotaPollController(t, a, quotaReply{status: http.StatusTooManyRequests})
	if err := ctrl.pollQuotaOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := ctrl.Snapshot()
	if st.State != string(StateConnected) {
		t.Fatalf("state after 429 = %q, want connected", st.State)
	}
	if st.Quota == nil || st.Quota.Remaining == nil || *st.Quota.Remaining != 0 {
		t.Fatalf("quota after 429 = %+v", st.Quota)
	}
}

func TestQuotaPollStopsOnDisconnect(t *testing.T) {
	now := time.Now()
	a := rotationPasswordAccount("poll", "poll@example.com", "poll-token", now)
	ctrl, _, tracker := newQuotaPollController(t, a, quotaReply{limit: quotaInt(100), remaining: quotaInt(42)})
	oldUnit := quotaPollUnit
	quotaPollUnit = 5 * time.Millisecond
	defer func() { quotaPollUnit = oldUnit }()
	if err := ctrl.store.SetOverride("quota_poll_minutes", "1"); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	ctrl.startQuotaPoll(stop)
	close(stop)
	time.Sleep(30 * time.Millisecond)
	if tracker.total() != 0 {
		t.Fatalf("quota requests after immediate stop = %d, want 0", tracker.total())
	}
}

func TestQuotaPollBackoff(t *testing.T) {
	oldUnit := quotaPollUnit
	quotaPollUnit = time.Minute
	defer func() { quotaPollUnit = oldUnit }()
	normal := time.Minute
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{0, 1 * time.Minute},
		{1, 2 * time.Minute},
		{2, 4 * time.Minute},
		{4, 16 * time.Minute},
		{10, 30 * time.Minute},
	}
	for _, tc := range cases {
		if got := quotaPollDelay(normal, tc.failures); got != tc.want {
			t.Errorf("quotaPollDelay(%s, %d) = %s, want %s", normal, tc.failures, got, tc.want)
		}
	}
}

func newQuotaPollController(t *testing.T, a *Account, reply quotaReply) (*Controller, settings.Paths, *quotaTracker) {
	t.Helper()
	return newQuotaPollControllerWithHandler(t, a, reply, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
}

func newQuotaPollControllerWithHandler(t *testing.T, a *Account, reply quotaReply, fxaHandler http.HandlerFunc) (*Controller, settings.Paths, *quotaTracker) {
	t.Helper()
	ctrl, paths := newTestController(t, fxaHandler)
	srv, tracker := newQuotaServer(t, map[string]quotaReply{
		a.Tokens().AccessToken: reply,
	})
	ctrl.guardian = &guardian.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if err := saveAccounts(accountsPath(paths), &accountsFile{
		Version: 2, Active: a.ID, LastMethod: a.Method, Accounts: []*Account{a},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctrl.mu.Lock()
	ctrl.setStateLocked(StateConnected)
	ctrl.mu.Unlock()
	t.Cleanup(func() {
		ctrl.mu.Lock()
		if ctrl.state != StateUnauthenticated {
			ctrl.setStateLocked(StateIdle)
		}
		ctrl.mu.Unlock()
	})
	return ctrl, paths, tracker
}

func TestRotationMatrix(t *testing.T) {
	now := time.Now()
	unknown := quotaReply{status: http.StatusInternalServerError}
	zero := quotaReply{limit: quotaInt(100), remaining: quotaInt(0)}
	eighty := quotaReply{limit: quotaInt(100), remaining: quotaInt(80)}
	hundred := quotaReply{limit: quotaInt(100), remaining: quotaInt(100)}
	five := quotaReply{limit: quotaInt(100), remaining: quotaInt(5)}
	browserReauth := func(id, email, token string, lastUsed time.Time) *Account {
		return rotationBrowserAccount(id, email, token, now.Add(time.Hour).Unix(), true, lastUsed)
	}
	cases := []struct {
		name       string
		accounts   []*Account
		active     string
		replies    map[string]quotaReply
		wantActive string
		wantFatal  bool
		wantError  []string
	}{
		{
			name: "active above threshold",
			accounts: []*Account{
				rotationPasswordAccount("a", "a@example.com", "a", now),
				rotationPasswordAccount("b", "b@example.com", "b", now.Add(-time.Hour)),
			},
			active: "a", replies: map[string]quotaReply{"a": eighty, "b": hundred}, wantActive: "a",
		},
		{
			name: "active below threshold",
			accounts: []*Account{
				rotationPasswordAccount("a", "a@example.com", "a", now),
				rotationPasswordAccount("b", "b@example.com", "b", now.Add(-time.Hour)),
			},
			active: "a", replies: map[string]quotaReply{"a": five, "b": eighty}, wantActive: "b",
		},
		{
			name: "active unknown other known",
			accounts: []*Account{
				rotationPasswordAccount("a", "a@example.com", "a", now),
				rotationPasswordAccount("b", "b@example.com", "b", now.Add(-time.Hour)),
			},
			active: "a", replies: map[string]quotaReply{"a": unknown, "b": eighty}, wantActive: "b",
		},
		{
			name: "both unknown",
			accounts: []*Account{
				rotationPasswordAccount("a", "a@example.com", "a", now),
				rotationPasswordAccount("b", "b@example.com", "b", now.Add(-time.Hour)),
			},
			active: "a", replies: map[string]quotaReply{"a": unknown, "b": unknown}, wantActive: "a",
		},
		{
			name: "active positive other unknown",
			accounts: []*Account{
				rotationPasswordAccount("a", "a@example.com", "a", now),
				rotationPasswordAccount("b", "b@example.com", "b", now.Add(-time.Hour)),
			},
			active: "a", replies: map[string]quotaReply{"a": five, "b": unknown}, wantActive: "a",
		},
		{
			name: "both exhausted",
			accounts: []*Account{
				rotationPasswordAccount("a", "a@example.com", "a", now),
				rotationPasswordAccount("b", "b@example.com", "b", now.Add(-time.Hour)),
			},
			active: "a", replies: map[string]quotaReply{"a": zero, "b": zero},
			wantFatal: true, wantError: []string{"a@example.com", "b@example.com"},
		},
		{
			name: "active exhausted other unknown",
			accounts: []*Account{
				rotationPasswordAccount("a", "a@example.com", "a", now),
				rotationPasswordAccount("b", "b@example.com", "b", now.Add(-time.Hour)),
			},
			active: "a", replies: map[string]quotaReply{"a": zero, "b": unknown}, wantActive: "b",
		},
		{
			name: "active reauth other exhausted",
			accounts: []*Account{
				browserReauth("a", "reauth@example.com", "reauth-token", now),
				rotationPasswordAccount("b", "b@example.com", "b", now.Add(-time.Hour)),
			},
			active: "a", replies: map[string]quotaReply{"b": zero},
			wantFatal: true, wantError: []string{"reauth@example.com", "b@example.com"},
		},
		{
			name: "active zero two unknown least recently used",
			accounts: []*Account{
				rotationPasswordAccount("a", "a@example.com", "a", now),
				rotationPasswordAccount("b", "b@example.com", "b", now.Add(-2*time.Hour)),
				rotationPasswordAccount("c", "c@example.com", "c", now.Add(-time.Hour)),
			},
			active: "a", replies: map[string]quotaReply{"a": zero, "b": unknown, "c": unknown},
			wantActive: "b",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl, _, _ := newRotationController(t, tc.accounts, tc.active, tc.replies, 10)
			err := ctrl.selectAccount(context.Background())
			if tc.wantError != nil {
				if err == nil {
					t.Fatal("selectAccount succeeded, want error")
				}
				var fatal *fatalError
				if tc.wantFatal && !errors.As(err, &fatal) {
					t.Fatalf("error = %v, want fatal", err)
				}
				for _, want := range tc.wantError {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("error %q does not contain %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := selectActiveID(t, ctrl); got != tc.wantActive {
				t.Fatalf("active = %q, want %q", got, tc.wantActive)
			}
		})
	}
}

func TestAllAccountsNeedSignInIsNotFatal(t *testing.T) {
	now := time.Now()
	a := rotationBrowserAccount("a", "a@example.com", "expired-a", now.Add(-time.Hour).Unix(), false, now)
	b := rotationBrowserAccount("b", "b@example.com", "expired-b", now.Add(-time.Hour).Unix(), false, now.Add(-time.Hour))
	ctrl, _, _ := newRotationController(t, []*Account{a, b}, "a", nil, 10)
	err := ctrl.selectAccount(context.Background())
	if err == nil || !strings.Contains(err.Error(), "all accounts need sign-in again") {
		t.Fatalf("selectAccount error = %v", err)
	}
	var fatal *fatalError
	if errors.As(err, &fatal) {
		t.Fatalf("all-reauth error must be non-fatal: %v", err)
	}
	if !strings.Contains(err.Error(), "a@example.com") || !strings.Contains(err.Error(), "b@example.com") {
		t.Fatalf("error does not name both accounts: %v", err)
	}
}

func TestQuotaEligible(t *testing.T) {
	limit := int64(100)
	remaining := int64(10)
	ctrl := &Controller{}
	if eligible, known := ctrl.quotaEligible(&limit, &remaining, 10); !eligible || !known {
		t.Fatalf("10%% at 10 = eligible %v known %v", eligible, known)
	}
	remaining = 9
	if eligible, known := ctrl.quotaEligible(&limit, &remaining, 10); eligible || !known {
		t.Fatalf("9%% at 10 = eligible %v known %v", eligible, known)
	}
	eligible, known := ctrl.quotaEligible(nil, &remaining, 10)
	if !eligible || known {
		t.Fatalf("unknown = eligible %v known %v", eligible, known)
	}
}

func TestQuotaPollBackoffScalesWithUnit(t *testing.T) {
	oldUnit := quotaPollUnit
	quotaPollUnit = time.Millisecond
	defer func() { quotaPollUnit = oldUnit }()
	if got := quotaPollDelay(2*time.Millisecond, 10); got != 30*time.Millisecond {
		t.Fatalf("scaled cap = %s, want 30ms", got)
	}
	if got := quotaPollDelay(2*time.Millisecond, 1); got != 4*time.Millisecond {
		t.Fatalf("scaled double = %s, want 4ms", got)
	}
}
