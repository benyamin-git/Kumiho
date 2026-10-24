# Firefox Accounts browser OAuth — Implementation Plan

**Spec:** [`docs/superpowers/specs/2026-10-24-fxa-browser-oauth-design.md`](../specs/2026-10-24-fxa-browser-oauth-design.md)
**Status:** Approved
**Date:** 2026-10-24

## Global constraints

- Target **v0.6.1**; do not tag, push, publish or bump the release version in
  this plan. No release action without explicit approval.
- Start from `main` at the spec commit or later; the current working tree has
  superseded browser work that Task 0 removes.
- Go version pinned by `go.mod` (1.27.1), vendored dependencies, no network
  needed for builds/tests.
- Control protocol v1 stays byte-compatible. New fields are additive; do not
  remove existing fields. New command type `login_complete` is additive.
- `accounts.json` remains v2; the new re-auth flag is additive. New browser
  tokens live in `Account.FxA`; the legacy `BrowserToken` is decode-only.
- Android: compileSdk 37.2, targetSdk 36, minSdk 26, arm64-v8a/armeabi-v7a.
  No new Maven dependency; the WebView is platform-provided.
- Use the house design rules: `Dimens`, Material 3 semantic roles, 48dp touch
  targets, 16dp dialog radius, 64dp top bar, no raw colours in components.
- Never log or display an authorization code, PKCE verifier, redirect URL,
  access token or refresh token. Email stays sanitized in logs.
- Every commit leaves the full gate green:
  `make lint && make test && go test -race -count=1 ./... && make build`
  plus Windows/Android cross-builds; Android changes also run
  `make android-test`.
- Keep `.superpowers/`, `.ai-state`, `.playwright-cli/` and other scratch out
  of commits.

## Baseline and task order

The current main working tree contains the superseded v0.6.0 browser work:
Android deep-link handling, `webauth` external callback, and the browser-token
activation recovery. The only independent change worth keeping is the
`VpnServiceCoordinator` cold-start fix. Task 0 isolates that fix and resets the
rest. The CLI proof gate after Task 4 must pass before TUI/GUI/Android work;
otherwise stop and reopen the design.

```
Task 0  baseline: cold-start fix only
Task 1  fxa OAuth client
Task 2  account model, refresh, legacy removal
Task 3  engine browser flow + IPC + mobile command
Task 4  CLI paste flow
Gate G1 real-account CLI proof
Task 5  TUI paste flow
Task 6  Windows GUI paste flow
Task 7  Android WebView core
Task 8  Android account/setup integration
Task 9  remove old flow + docs
Task 10 gates, debug APK, phone/Linux acceptance
```

## Task 0 — Recover a clean baseline and keep the cold-start fix

**Commit:** `fix: complete waiting engine handles on reconnect`

**Depends on:** spec commit.

**Files**

- Modify: `android/app/src/main/java/dev/benyamin/kumiho/vpn/VpnServiceCoordinator.kt`
- Reset (discard working-tree changes): all files listed by
  `git status --short`, except the cold-start hunk below.
- Delete known superseded untracked paths:
  `android/app/src/main/java/dev/benyamin/kumiho/ui/account/BrowserDeepLink.kt`,
  `android/app/src/test/java/dev/benyamin/kumiho/ui/account/BrowserDeepLinkTest.kt`,
  `.playwright-cli/`, `android/app/src/debug/`.
- Do **not** run a broad `git clean -fd`; remove only those known paths.
- The debug-only `android/app/src/debug/res/values/strings.xml` label is
  re-added in Task 8, not kept here.

**Steps**

1. Save the cold-start hunk:
   ```diff
   -		synchronized(bindLock) {
   +		val waiter := synchronized(bindLock) {
    			// A fresh, already-completed handle for this binding; callers that
    			// waited through a previous failure see a healthy client again.
   +			val previous = pendingEngine
    			pendingEngine = CompletableDeferred<EngineClient>().also { it.complete(client) }
   +			previous
    		}
   +		// A caller may have captured the previous deferred before this
   +		// connection arrived (for example a deep link that cold-started the
   +		// activity). Complete it too, or that caller waits forever.
   +		waiter.complete(client)
   ```
2. Confirm this plan and the approved spec are committed, then run
   `git checkout -- .` to restore every tracked file to `HEAD`. This discards
   the old deep-link/webauth/activation work. Do not run this before the plan
   commit exists; the plan document is the only new tracked file this workflow
   needs to preserve.
3. Remove the known untracked paths above.
4. Re-apply only the cold-start hunk to `VpnServiceCoordinator.kt`; remove the
   old `submitBrowserCode` method, its `Log`/`CancellationException` imports and
   `TAG` if they are no longer used. Keep `CompletableDeferred`.
5. Confirm `git diff` contains only the cold-start hunk.

**Verification**

```
git diff --check
git diff --stat
make android-test
git status --short
```

Expected: `make android-test` passes; only the intended Kotlin file is changed
before the commit; after commit the tree is clean apart from ignored scratch.

## Task 1 — Add the Firefox Accounts OAuth client

**Commit:** `feat: add firefox accounts oauth browser client`

**Depends on:** Task 0.

**Files**

- Create: `internal/fxa/oauth.go`
- Create: `internal/fxa/oauth_test.go`
- Modify: `internal/fxa/fxa.go`
- Modify: `internal/fxa/client.go`

**Interfaces**

```go
const (
    DefaultAuthorizationEndpoint = "https://accounts.firefox.com/authorization"
    OAuthRedirectURI             = "urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel"
    DefaultProfileEndpoint       = "https://profile.accounts.firefox.com/v1/profile"
    DefaultDestroyEndpoint       = "https://oauth.accounts.firefox.com/v1/destroy"
)

type PKCE struct {
    Verifier  string
    Challenge string
    State     string
}

type Profile struct {
    UID         string
    Email       string
    DisplayName string
    Avatar      string
}

func NewPKCE(r io.Reader) (PKCE, error)

func (c *Client) AuthorizationURL(state, challenge, email string, forceAuth bool) string
func (c *Client) ExchangeCode(ctx context.Context, code, verifier string) (*Tokens, error)
func (c *Client) Profile(ctx context.Context, accessToken string) (*Profile, error)
func (c *Client) Destroy(ctx context.Context, refreshToken string) error
```

Add `AuthorizationEndpoint`, `ProfileEndpoint` and `DestroyEndpoint` to
`Client`, defaulted by `NewClient`. Tests override them with `httptest.Server`
URLs. `ExchangeCode` posts to the existing `BaseURL` + `/oauth/token` so the
password flow's endpoint stays unchanged.

**Changes**

- `NewPKCE`: 32 random bytes → `base64.RawURLEncoding` 43-character verifier;
  SHA-256(verifier) → 43-character challenge; 16 random bytes → state. Reject
  short reads from `r`.
- `AuthorizationURL`: `url.Values` with `client_id=5882386c6d801776`,
  `scope=profile https://identity.mozilla.com/apps/vpn`,
  `response_type=code`, `redirect_uri`, `state`, `code_challenge`,
  `code_challenge_method=S256`, `access_type=offline`; optional `email`; when
  `forceAuth` add `action=force_auth`. No client secret.
- `ExchangeCode`: JSON body with `client_id`, `grant_type=authorization_code`,
  `code`, `code_verifier`, `redirect_uri`; parse with
  `tokensFromResponse(raw, "")`.
- `Profile`: GET `ProfileEndpoint` with `Authorization: Bearer <token>`,
  `Accept: application/json`; parse `uid`, `email`, `displayName`, `avatar`;
  require non-empty UID and email.
- `Destroy`: POST `DestroyEndpoint` with `{"refresh_token": token}`; used only
  best-effort by logout.
- Refactor `doJSON` into a shared request builder that accepts a full endpoint
  and an optional bearer token. `doJSON` keeps its current path/Hawk behaviour.
  Preserve the 406 challenge solver and 1 MiB response limit.
- No token/code/verifier logging.

**Tests**

- `TestAuthorizationURL`: exact client, scope, redirect, state, challenge,
  `access_type`, optional email and `force_auth`; no secret.
- `TestNewPKCE`: verifier 43 chars, challenge is S256, state non-empty.
- `TestExchangeCode`: request path/body and access/refresh/scope/expiry parse.
- `TestProfile`: bearer header and JSON fields; missing email/UID errors.
- `TestDestroy`: body and endpoint.
- `TestOAuthErrorParsing`: 400/401 bodies map to `fxa.APIError`.

**Verification**

```
go test ./internal/fxa -run 'Test(AuthorizationURL|NewPKCE|ExchangeCode|Profile|Destroy|OAuthError)' -v
go test ./internal/fxa
make lint
```

Expected: all tests pass; no network required; no secret/token strings in test
logs.

## Task 2 — Store browser sessions as FxA tokens and remove legacy accounts

**Commit:** `refactor: store browser sign-in as fxa tokens`

**Depends on:** Task 1.

**Files**

- Modify: `internal/engine/accounts.go`
- Modify: `internal/engine/engine.go`
- Modify: `internal/engine/connect.go`
- Modify: `internal/engine/rotation.go`
- Modify: `internal/engine/tokens.go` only if a sync helper is needed
- Modify: `internal/engine/accounts_test.go`
- Modify: `internal/engine/rotation_test.go`
- Modify: `internal/engine/browser_test.go` (model assertions only; flow is Task 3)
- Modify: `internal/engine/logout_test.go` if it asserts browser fields
- Modify: `internal/engine/connect_test.go` if it asserts browser activation

**Changes**

- `Account` gains `ReauthRequired bool json:"reauth_required,omitempty"`.
  `BrowserToken` is documented as legacy decode-only; keep its fields so old
  files load, but new code never writes it.
- `Account.Tokens()` returns `Account.FxA` for every method; it returns nil when
  FxA is absent. Remove `browserAsTokens` and the legacy fallback.
  `Account.ReauthRequired()` returns the top-level flag.
- `upsert`: copy `FxA` for `MethodBrowser` and `MethodPassword`; do not copy
  `Browser` from a new candidate. Keep the five-account cap and label rules.
- `markReauth`: set the top-level flag and `LastError`; do not create a
  `BrowserToken`. Keep it limited to browser accounts, or explicitly apply the
  same durable flag to any FxA account that needs re-auth.
- Remove `selectedBrowserToken` and `browserTokenUsable` and all callers. The
  generic `fxa.AccessTokenValid` + `refreshAccessToken` path now covers browser
  accounts.
- `accountSessionUsable`: use `!a.ReauthRequired()`, a non-nil FxA token set,
  and the existing access/refresh rules. No browser branch.
- `EnsureFreshToken`: remove the browser branch and `activateBrowserToken`;
  use `refreshAccessToken` for every account.
- `refreshAccessToken`: remove the browser-only gate. For a `MethodBrowser`
  account, a permanent refresh rejection sets the top-level re-auth flag and
  keeps the account for a fresh browser login. For a password account, keep the
  existing `dropActiveIf` behaviour unchanged.
- `Account()` and `acquirePass()`: remove the browser activation path; use the
  normal refresh retry. `acquirePass` keeps the password 401 refresh/activate
  chain.
- `prepareAccount`, `rotationSnapshot`, `rotationCandidate` and
  `pollQuotaOnce`: remove the browser token branch; every account with FxA is
  eligible for the normal refresh path. Keep rotation skip notices.
- `refreshAccountToken`: on a permanent refresh rejection for a
  `MethodBrowser` account, set the top-level re-auth flag before returning the
  error; password accounts keep the existing skip/drop behaviour.
- `completeLogin` clears the top-level re-auth flag on a successful password
  login for the same email.
- Add `removeLegacyBrowserAccounts(f *accountsFile) int`:
  - remove accounts with `MethodBrowser`, `FxA == nil`, and a non-empty
    legacy `Browser.Token`;
  - clear `Browser` on password accounts without removing them;
  - return the number of removed accounts.
- Call it after `loadAccounts`/`migrateAccounts` in `Start` and
  `ensureAccountsLocked`, before selecting the active account. If it removed
  accounts, persist `accounts.json`, sync `tokens.json`, select the most
  recently used remaining account or clear the session, and set
  `c.legacyNotice` to:
  `Old browser sign-in removed. Add your Firefox account again.`
- Add `legacyNotice string` to `Controller`. `Accounts()` exposes
  `rotationNotice` first, otherwise `legacyNotice`, and clears the legacy notice
  after the first read. The removal itself is persisted.
- `Logout`: snapshot every stored FxA refresh token under `c.mu`, then
  best-effort call `c.fxa.Destroy(ctx, refreshToken)` under one 5-second
  context for the whole pass (ignore failures), then call
  `clearSession("user logout")`. Other
  `clearSession` callers (account removal, reauth failure, shutdown) stay
  local-only. A failed revoke never blocks local sign-out.
- Do not remove password accounts; do not remove a browser account that already
  has FxA tokens.

**Tests**

- `TestAccountUsesFxAForBrowserMethod`: MethodBrowser + FxA returns the FxA
  access/refresh tokens; no browser fallback.
- `TestUpsertCopiesFxAForBrowserMethod` and password method.
- `TestLegacyBrowserAccountsRemovedOnStart`: seed one legacy browser and one
  password account; Start removes only the browser account, keeps the password
  account, selects the MRU remaining account, clears the stale token copy, and
  sets the notice.
- `TestLegacyBrowserRemovalWhenActive`: active legacy account removed; next
  account selected; all-removed case signs out.
- `TestBrowserRefreshUsesFxaRefresh`: expired browser FxA token refreshes;
  rotated refresh token persists; permanent 401 sets re-auth.
- Update rotation tests to use FxA browser accounts and no `BrowserToken`.
- Update logout/account tests that referenced `Browser.ReauthRequired`.
- `TestLogoutRevokesBestEffort`: a mock FxA destroy endpoint receives the
  refresh token; a failing/offline destroy still clears local accounts,
  `tokens.json`, and the session.

**Verification**

```
go test ./internal/engine -run 'Test(Account|Upsert|Legacy|BrowserRefresh|Rotation|EnsureFresh|Logout)' -v
go test -race -count=1 ./internal/engine
make lint
```

Expected: all pass; legacy removal notice asserted; no stale `BrowserToken`
write in new flows.

## Task 3 — Engine browser flow, IPC and mobile command

**Commit:** `feat: add fxa browser sign-in flow`

**Depends on:** Tasks 1 and 2.

**Files**

- Rewrite: `internal/engine/browser.go`
- Modify: `internal/engine/engine.go` (`Controller` fields, `New`, `LoginStart`)
- Modify: `internal/engine/browser_test.go`
- Modify: `internal/api/api.go`
- Modify: `internal/api/compat_test.go`
- Modify: `internal/ipc/ipc.go`
- Modify: `internal/daemon/run.go`
- Modify: `mobile/commands.go`
- Modify: `mobile/commands_test.go`
- Delete: `internal/webauth/webauth.go`, `internal/webauth/webauth_test.go`
- Delete references to `internal/webauth` in `internal/engine/engine.go` and
  `internal/engine/browser.go`

**Interfaces**

```go
type LoginCompletePayload struct {
    LoginID     string `json:"login_id,omitempty"`
    RedirectURL string `json:"redirect_url,omitempty"`
    Code        string `json:"code"` // legacy field; new flow rejects it
}

const TypeLoginComplete = "login_complete"

func (c *Controller) LoginComplete(id, redirectURL string) error
```

`LoginStartPayload` and `LoginState` keep their shape. The engine's
`LoginComplete` signature takes the redirect URL, not a raw code.

**Changes**

- `browserLogin`:
  ```go
  type browserLogin struct {
      id       string
      state    api.LoginState
      targetID string
      active   bool
      verifier string
      oauthState string
      created  time.Time
      tokens   *fxa.Tokens
      profile  *fxa.Profile
      profileErr error
  }
  ```
  Keep the fields guarded by `Controller.mu`; no listener, `session`, `done`,
  `cancel` or `ctx`.
- `Controller`: remove `browserBaseURL`, `browserPlatform`, `newBrowserFlow`
  and the `webauth` import. Set `browserTimeout` to `10 * time.Minute` in `New`.
- `LoginStart(MethodBrowser, ...)`:
  - generate PKCE with `fxa.NewPKCE(c.fxa.Rand)`;
  - build the URL with `c.fxa.AuthorizationURL(...)`, using `forceAuth` when
    `accountID != ""` and the stored email is known; prefill `email` for add;
  - store the flow, return `Step:"browser", URL, LoginID`;
  - no goroutine, no loopback listener.
- `LoginStatus`: return the stored state; if the flow is older than
  `browserTimeout`, mark it terminal with
  `browser sign-in timed out; start again`.
- `LoginComplete(id, redirectURL)`:
  - require an active flow; an empty id addresses the active flow, a non-empty
    id must match exactly; reject a flow older than `browserTimeout`;
  - if `redirectURL == ""` and the legacy `Code` field was supplied, return
    `paste the complete redirect URL from the browser`;
  - parse the URN redirect: scheme `urn`, opaque part
    `ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel`, query `code` and `state`;
    reject malformed/error redirects with a stable message;
  - verify `state` exactly; a mismatch is retryable and keeps the flow active;
  - if `tokens == nil`, call `c.fxa.ExchangeCode`; on a permanent
    `invalid_grant`/expired/used/wrong-client error, mark the flow terminal;
    on network/5xx, keep it active and return the error;
  - call `c.fxa.Profile`; on network/5xx, keep the tokens and return a
    retryable `profile` error; a second `LoginComplete` with the same id retries
    only the profile call; on profile 401, discard tokens and make the flow
    terminal;
  - on success, save the account via `finishBrowserSuccess` with
    `MethodBrowser`, `FxA: tokens`, `ReauthRequired:false`, profile email,
    label rules, active selection and `saveAccountsLocked(true)`;
  - enforce the re-auth email match: target email must equal profile email;
  - enforce the tunnel/account guard as today;
  - on success or a terminal failure, set `active=false` and publish the
    terminal `done`/`error` state; keep `c.browser` until the next
    `LoginStart`/`LoginCancel` so `LoginStatus` can report it.
- Remove `runBrowserLogin`, `finishBrowserError`, `finishBrowserSuccess`'s old
  `webauth.Result` signature, `tokenExpiryString`, `friendlyBrowserError`'s
  `webauth:` prefix and all `webauth.Session` calls.
- `LoginCancel`: set `active=false`, state `cancelled`; no session cancel.
- `cancelBrowserLocked`: same, no `session.Cancel`.
- `api.LoginCompletePayload`: add `RedirectURL`; keep `Code` for wire
  compatibility. `compat_test` asserts the new key.
- `ipc`: add `TypeLoginComplete`.
- `daemon/run.go`: handle `ipc.TypeLoginComplete`; decode the payload; if
  `RedirectURL == "" && Code != ""` return `CodeBadRequest` with the full-URL
  message; call `c.ctrl.LoginComplete(p.LoginID, p.RedirectURL)`.
- `mobile/commands.go`: the `login_complete` branch calls
  `f.ctrl.LoginComplete(p.LoginID, p.RedirectURL)`; reject a legacy code with
  the same bad-request message.
- Delete `internal/webauth` and its tests.

**Tests**

Rewrite `internal/engine/browser_test.go` with a mock FxA server:
- start URL contains state, PKCE, `force_auth`, redirect and client;
- complete with a full URN redirect creates a browser account with FxA tokens
  and profile email;
- wrong state, malformed URL and missing code keep the flow alive with a clear
  error;
- token endpoint 400 `invalid_grant` is terminal; 500/network is retryable;
- profile 500 keeps tokens and a second complete call retries only profile;
- profile 401 discards tokens and is terminal;
- re-auth mismatch rejects and leaves the old account untouched;
- account limit, tunnel guard, cancel and 10-minute timeout;
- no token/code/verifier in logs.
- Update `mobile/commands_test.go` to send `redirect_url`; assert a legacy
  `code` is rejected.
- Update `api` compat tests for `redirect_url`.

**Verification**

```
go test ./internal/fxa ./internal/engine ./internal/api ./internal/ipc ./internal/daemon ./mobile -v
go test -race -count=1 ./internal/engine
make lint
make build
```

Expected: all pass; `internal/webauth` is gone; `make build` links.

## Task 4 — CLI redirect-URL paste flow

**Commit:** `feat: accept browser redirect urls in the cli`

**Depends on:** Task 3.

**Files**

- Modify: `internal/cli/login.go`
- Modify: `internal/cli/login_test.go`
- Modify: `internal/cli/cli_test.go` if the help/golden text changes

**Changes**

- Change `runBrowserSignIn` to accept an `io.Reader` for the redirect URL; keep
  the `os.Stdin` caller. Refactor the existing terminal reader so tests can pass
  a buffer.
- After printing the URL and optionally opening it, print:
  `After signing in, copy the complete redirect URL from the browser's address bar and paste it here.`
  The browser may say it cannot open the `urn:` address; that is expected.
- Read one line; trim; call `ipc.TypeLoginComplete` with
  `api.LoginCompletePayload{LoginID: ls.LoginID, RedirectURL: line}`.
- On a retryable paste/exchange/profile error, print the error and prompt again
  until the 10-minute deadline. On a terminal error or `cancelled`, stop.
- After a successful complete, poll `login_status` until `done`; print the
  existing `printSignInDone`.
- Update the 5-minute deadline to 10 minutes.
- Keep `--no-open`: print the URL but still prompt for the redirect URL.
- Do not echo or log the redirect URL.
- Tests: unit-test the prompt/trim/retry helper and the `login_complete`
  payload against a fake IPC server; verify a wrong URL is re-prompted and a
  terminal error exits non-zero.

**Verification**

```
go test ./internal/cli -run 'Test(BrowserRedirect|LoginComplete|RunBrowser)' -v
go test ./internal/cli
make lint
```

Expected: all pass; no real browser/network.

## Gate G1 — Real-account CLI proof

**No commit. This is a stop/go gate.**

Run the CLI against a real Firefox account with the VPN entitlement:

```
scripts/dev-daemon.sh start
source scripts/dev-env.sh
make build
dist/kumiho-linux-amd64 login --browser
# open the printed URL, complete Mozilla sign-in, copy the urn: redirect URL
# paste it into the CLI
dist/kumiho-linux-amd64 account
dist/kumiho-linux-amd64 connect
dist/kumiho-linux-amd64 status
```

Expected:

- `login --browser` completes and reports the profile email;
- `account` shows `subscribed=true` and a valid quota from `/fpn/status` (or
  equivalent entitlement output);
- `connect` reaches `CONNECTED`;
- logs contain `signed in as … (browser)` and `renewed FxA access token` (or an
  FxA token with a scope), never an opaque VPN token or a 401 loop.

If `/fpn/status` rejects the token, or the profile lookup cannot complete, stop
this plan and reopen the design. Do not build TUI/GUI/Android work on an
unproven token. Record the exact error (without tokens) in `.ai-state`.

## Task 5 — TUI redirect-URL paste flow

**Commit:** `feat: accept browser redirect urls in the tui`

**Depends on:** Task 3 and Gate G1.

**Files**

- Modify: `internal/tui/account.go`
- Modify: `internal/tui/tui.go`
- Modify: `internal/tui/help.go`
- Modify: `internal/tui/account_test.go`

**Changes**

- Extend `accountState` with `browserInput`, `browserSubmitting`,
  `browserError` (or equivalent).
- The browser waiting card shows the URL and:
  `Paste the complete redirect URL from the browser:`
  followed by a labelled input line, plus Cancel. The card explains that the
  browser's “can't open this address” page is expected.
- Handle printable paste/keys while the browser flow is waiting; Enter calls a
  new `completeBrowserLogin(client, id, url)` command using
  `ipc.TypeLoginComplete` and `api.LoginCompletePayload{LoginID, RedirectURL}`.
- On success, clear the input and continue polling; on a retryable error, keep
  the flow and show the error; on terminal error, clear the flow.
- Esc cancels as today. Update the help line to mention paste/Enter.
- Add tests: Enter sends the exact redirect URL; wrong URL shows an error and
  keeps `browserID`; cancel still calls the cancel command; `done` finishes.

**Verification**

```
go test ./internal/tui -run 'TestAccountBrowser|TestLoginScreen' -v
go test ./internal/tui
make lint
```

Expected: all pass; no ANSI/terminal dependency in tests.

## Task 6 — Windows GUI redirect-URL paste flow

**Commit:** `feat: accept browser redirect urls in the windows gui`

**Depends on:** Task 3 and Gate G1.

**Files**

- Modify: `internal/gui/bridge.go`
- Modify: `internal/gui/client.go`
- Modify: `internal/gui/controller.go`
- Modify: `internal/gui/controller_test.go`
- Modify: `internal/gui/mock/mock.go`
- Modify: `internal/gui/mock/scenario.go` only if a waiting fixture needs the
  new field
- Modify: `internal/gui/web/static/js/screens/account.js`
- Modify: `internal/gui/web/static/js/screens/onboarding.js`
- Modify: `internal/gui/web/static/css/` only for the paste row if needed

**Changes**

- Add `ActionLoginComplete = "login_complete"` to the frozen action list and
  `knownActions`.
- Add `LoginComplete(context.Context, api.LoginCompletePayload) (api.Result, error)`
  to the `Daemon` interface and `DaemonClient`; add it to `fakeDaemon` and
  `mock.Mock`.
- Controller dispatch:
  - decode `LoginCompletePayload`;
  - choose account vs onboarding from the existing `accountAction` flag;
  - call the matching daemon method (both currently use the same engine
    command; the distinction is the screen notice/state);
  - on success, observe/nil, refresh the login state via the existing poll or
    `LoginStatus`, and let the web UI's poll see `done`;
  - add `loginActionError` prefixes for `ActionLoginComplete`.
- Account waiting card: add a labelled `Redirect URL` input and `Continue`
  button (and keep Open again/Cancel). Submitting sends
  `{login_id, redirect_url}`; disable while sending; show the error inline.
- Onboarding waiting card: same field/button; send `login_complete`.
- Sign-out dialog in both account and onboarding screens: keep the existing
  question but add the line that Kumiho will also try to revoke its Mozilla
  sign-in and that local sign-out completes offline.
- Do not auto-open the browser more than once per URL; preserve the existing
  `openedBrowserURL` guard.
- Tests: controller dispatch payload, success, error prefix, and that the
  waiting card still polls. Mock scenario exposes a waiting flow.
- Manual web harness check with `scripts/dev-gui-web.sh` if available; no
  Windows manual acceptance required.

**Verification**

```
go test ./internal/gui -run 'Test.*Login|Test.*Browser|Test.*Account' -v
go test ./internal/gui/...
make lint
make build-gui-web
```

Expected: all pass; the web harness shows the paste field; no raw URL/token in
logs.

## Task 7 — Android Mozilla account WebView core

**Commit:** `feat: add android mozilla account webview`

**Depends on:** Task 3.

**Files**

- Create: `android/app/src/main/java/dev/benyamin/kumiho/ui/account/MozillaAccountWebView.kt`
- Create: `android/app/src/test/java/dev/benyamin/kumiho/ui/account/MozillaAccountWebViewTest.kt`
- Modify: `android/app/src/androidTest/java/dev/benyamin/kumiho/ui/account/AccountBrowserTest.kt`
- Modify: `android/app/src/main/res/values/strings.xml`
- Modify: `android/app/src/main/java/dev/benyamin/kumiho/ui/theme/Dimens.kt` only
  if a new shared dimension is required (prefer existing).
- Re-add: `android/app/src/debug/res/values/strings.xml` with
  `<string name="app_name">Kumiho Debug</string>` and the existing comment.

**Interfaces**

```kotlin
data class BrowserRedirect(val code: String, val state: String)

fun isMozillaAuthorizationUrl(raw: String): Boolean
fun parseBrowserRedirect(raw: String): BrowserRedirect?
fun clearMozillaCookies(context: Context)

@Composable
fun MozillaAccountBrowserScreen(
    loginId: String,
    url: String,
    onRedirect: (String) -> Unit,
    onClose: () -> Unit,
    onUnavailable: () -> Unit,
    modifier: Modifier = Modifier,
)
```

**Changes**

- `isMozillaAuthorizationUrl`: HTTPS only, host `accounts.firefox.com`,
  non-empty path; used before loading the initial URL.
- `parseBrowserRedirect`: parse `urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel`
  with `code` and `state`; URL-decode; reject empty values, wrong scheme/opaque
  part and error redirects. Unit-testable without Android.
- `MozillaAccountBrowserScreen`:
  - full-screen `Column` with a 64dp top bar, title `Mozilla account`, close
    button; no bottom navigation;
  - `AndroidView` hosting a `WebView` created once; `DisposableEffect` destroys
    it;
  - WebView settings: JavaScript on (FxA requirement), DOM storage on, file and
    content access off, mixed content never, multiple windows off, save form
    data off; no `addJavascriptInterface`;
  - `WebViewClient.shouldOverrideUrlLoading`: if URL is the expected URN,
    parse and call `onRedirect(fullURL)`, return true; if HTTPS
    `accounts.firefox.com`, load in WebView; other HTTPS → external
    `ACTION_VIEW`; non-HTTPS → block;
  - `onPageFinished` fallback: if the current URL is the URN, parse and submit;
    guard both paths with a one-shot flag so the redirect cannot be submitted
    twice;
  - loading indicator until first page; error state with Close and a note that
    the redirect URL can be pasted from Account;
  - catch `WebView` creation/`WebViewFactory` failure and call `onUnavailable`;
  - `clearMozillaCookies`: `CookieManager.removeAllCookies(null); flush()`.
- Strings: title, close, loading, error, paste hint. Keep `Continue in browser`
  and `Browser` labels unchanged.
- The existing `openBrowserSignIn` Custom Tabs helper in `AccountBrowser.kt`
  remains only as the no-WebView fallback; update its doc and tests. Do not
  delete it.
- Debug label: re-add the debug-only app name; no release effect.

**Tests**

- JVM: URL policy accepts only `https://accounts.firefox.com/...`; redirect
  parser accepts the exact URN, decodes code/state, rejects HTTP, wrong host,
  missing values, error params.
- Instrumented: WebView creation on the emulator (if available) or a fake
  redirect page; no real Mozilla login.
- `make android-test` passes.

**Verification**

```
make android-test
make android-emulator-test
```

Expected: JVM/lint/debug build pass; emulator test passes if the AVD is booted.
If no emulator, record that the connected test was not run and run the JVM
tests.

## Task 8 — Android account and setup integration

**Commit:** `feat: complete android browser sign-in in a webview`

**Depends on:** Task 7 and Task 3.

**Files**

- Modify: `android/app/src/main/java/dev/benyamin/kumiho/engine/EngineClient.kt`
- Modify: `android/app/src/main/java/dev/benyamin/kumiho/ui/account/AccountViewModel.kt`
- Modify: `android/app/src/main/java/dev/benyamin/kumiho/ui/account/AccountState.kt`
- Modify: `android/app/src/main/java/dev/benyamin/kumiho/ui/account/AccountScreen.kt`
- Modify: `android/app/src/main/java/dev/benyamin/kumiho/ui/setup/SetupViewModel.kt`
- Modify: `android/app/src/main/java/dev/benyamin/kumiho/ui/setup/SetupState.kt`
- Modify: `android/app/src/main/java/dev/benyamin/kumiho/ui/setup/SetupFlow.kt`
- Modify: `android/app/src/main/java/dev/benyamin/kumiho/ui/account/AccountBrowser.kt`
- Modify: `android/app/src/main/java/dev/benyamin/kumiho/MainActivity.kt`
- Modify: `android/app/src/main/java/dev/benyamin/kumiho/vpn/VpnServiceCoordinator.kt`
- Modify: `android/app/src/main/AndroidManifest.xml`
- Modify: `android/app/src/main/res/values/strings.xml`
- Modify: `android/app/src/test/java/dev/benyamin/kumiho/ui/account/AccountAccountsTest.kt`
- Modify: `android/app/src/test/java/dev/benyamin/kumiho/ui/setup/SetupViewModelTest.kt`
- Modify: `android/app/src/test/java/dev/benyamin/kumiho/engine/EngineClientTest.kt`
- Delete: `android/app/src/main/java/dev/benyamin/kumiho/ui/account/BrowserDeepLink.kt`
- Delete: `android/app/src/test/java/dev/benyamin/kumiho/ui/account/BrowserDeepLinkTest.kt`

**Changes**

- `EngineClient`:
  - `suspend fun loginComplete(redirectUrl: String, id: String? = null)` sends
    `{"redirect_url": ...}` and optional `login_id`;
  - do not send `code`.
- `VpnServiceCoordinator`: replace `submitBrowserCode` with
  `submitBrowserRedirect(redirectUrl, loginId)`; call `engine.loginComplete`.
  Keep the cold-start fix and remove old `Log`/`CancellationException` if
  unused.
- `MainActivity`: remove the deep-link fields, saved hash, `deliverBrowserCode`,
  `onNewIntent` override and imports. Keep the normal splash/theme flow.
- `AndroidManifest`: remove the `mozilla-vpn` intent filter and
  `launchMode="singleTask"` (unless another feature needs it); keep exported
  launcher activity.
- `AccountViewModel`:
  - add `loginComplete(redirectUrl, id)` to `AccountGateway` and
    `EngineAccountGateway`;
  - replace the Custom Tabs auto-open with in-app WebView state: keep
    `browserLaunchURL` as the URL to load and add
    `browserWebViewOpen: Boolean`. `openBrowser()` sets it true;
    `closeBrowserWebView()` sets it false but keeps the active `browser` flow;
    the route renders `MozillaAccountBrowserScreen` only when both the open
    flag and URL are present;
  - `openBrowser()` sets the WebView open; `closeBrowserWebView()` clears only
    the WebView, keeping the flow; `submitBrowserRedirect(url)` calls
    `gateway.loginComplete`, sets `browserSubmitting`, keeps the flow on
    retryable errors, and clears on done;
  - add `browserPasteOpen`, `browserPasteValue`, `browserPasteError`,
    `submitBrowserPaste()`;
  - `browserUnavailable()` closes the WebView, opens the system browser via the
    existing helper, reveals the paste field, and keeps the flow cancellable;
  - `cancelBrowser` clears the WebView and paste state;
  - `confirmSignOut` accepts an `onSignedOut: () -> Unit` dependency; the
    production route passes `{ clearMozillaCookies(context) }` and the JVM fake
    passes a no-op. Call it only after `gateway.logout()` succeeds, so the
    WebView cookie jar is cleared with the local session.
- `AccountScreen`:
  - if `browserWebViewOpen` and URL present, show
    `MozillaAccountBrowserScreen` full-screen (outside the Account LazyColumn);
    on redirect call `submitBrowserRedirect`, on close call
    `closeBrowserWebView`, on unavailable call `browserUnavailable`;
  - waiting card: title/copy, **Open again**, expandable **Paste redirect URL
    instead** with a labelled `Redirect URL` field and **Continue**, **Use
    password**, **Cancel**; use `Dimens.ControlHeight`, existing cards/tokens,
    test tags;
  - after successful submit, the engine state becomes done and the list
    refreshes; no stale WebView.
- `SetupViewModel`/`SetupFlow`: mirror the same WebView and paste behavior for
  first-run browser sign-in. The setup step should not advance until the engine
  reports done. Keep password fallback.
- `AccountBrowser.kt`: keep only the validated system-browser fallback
  (`openBrowserSignIn`); update the comment to say it is used only when the
  WebView is unavailable. Validate HTTPS/loopback as today.
- Remove the deep-link parser/test and all `mozilla-vpn` manifest handling.
- Strings: add `Mozilla account`, paste fallback, `Redirect URL`, `Continue`,
  `Use password`, paste error, WebView unavailable, and the sign-out revoke
  disclosure; keep **Continue in browser** and **Browser**.
- Tests:
  - ViewModel: start browser → WebView opens; redirect submit calls
    `loginComplete` with the full URL and id; done clears state/refreshes;
    retryable error keeps browser and shows error; close keeps flow; cancel
    clears; paste submit; unavailable sets system fallback and paste.
  - Setup: same minimal path.
  - EngineClient: JSON has `redirect_url`, not `code`.
  - Compose: waiting card paste expand/submit, tags, password link; no dialog
    clipping.
- Debug label remains only in `src/debug`.

**Verification**

```
make android-test
make android-debug
KUMIHO_EXPECT_PACKAGE=dev.benyamin.kumiho.debug KUMIHO_ABI=universal \
  bash scripts/android-verify-apk.sh android/app/build/outputs/apk/debug/app-debug.apk
```

Expected: JVM tests/lint/assemble pass; the debug APK verifies; no deep-link
filter; the waiting card fits.

## Task 9 — Remove the old flow and correct documentation

**Commit:** `docs: document firefox accounts browser sign-in`

**Depends on:** Tasks 3, 6, 8.

**Files**

- Verify deleted: `internal/webauth/*`, Android `BrowserDeepLink.kt`/test.
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/android.md`
- Modify: `docs/usage.md`
- Modify: `docs/gui.md`
- Modify: `docs/manual.md`
- Modify: `CHANGELOG.md`
- Modify: `PLAN.md` §13 only if the ToS/risk wording needs the new flow.
- Modify: `docs/superpowers/specs/2026-10-23-v0.6.0-roadmap-design.md` with a
  short superseded note pointing to the new spec.

**Changes**

- Describe direct FxA OAuth, the profile lookup, refresh, the Android WebView
  plus paste/password fallback, desktop paste, legacy account removal, and
  best-effort sign-out revocation.
- Correct every claim that browser sign-in stores a Guardian token or uses
  `mozilla-vpn://login/success`, Custom Tabs as the primary path, or a loopback
  callback on Android.
- Document that the browser's URN redirect produces a “can't open this
  address” page and that the address bar URL is the value to paste.
- Document the ToS gray area for the unofficial client.
- Update `AGENTS.md` architecture map: `internal/fxa` owns OAuth, profile and
  destroy; `internal/webauth` is gone; Android uses the WebView and the
  `login_complete` redirect URL; desktop uses paste.
- CHANGELOG entry under v0.6.1 (unreleased), no tag/push.
- Old spec note:
  `> Superseded by 2026-10-24-fxa-browser-oauth-design.md: the browser flow
  uses direct Firefox Accounts OAuth, not a Guardian token.`

**Verification**

```
grep -R "mozilla-vpn://login/success\|Guardian token\|webauth" README.md AGENTS.md docs internal --exclude-dir=vendor
grep -R "mozilla-vpn" android/app/src/main || true
make lint
go test ./...
```

Expected: no stale claims; only historical references in the old spec/section
if intentionally kept; Android manifest has no deep-link filter; tests pass.

## Task 10 — Full gates, debug APK and acceptance

**Commit:** only if fixes are required; otherwise no commit.

**Depends on:** Tasks 1–9.

**Verification — full gate**

```
make lint
make test
go test -race -count=1 ./...
make build
GOOS=windows GOARCH=amd64 go vet ./...
GOOS=windows GOARCH=amd64 go build ./...
GOOS=android GOARCH=arm64 go build ./...
make android-test
```

**Android artifact**

```
make android-debug
cp android/app/build/outputs/apk/debug/app-debug.apk \
  dist/kumiho-android-debug-0.6.0-fxa-oauth.apk
KUMIHO_EXPECT_PACKAGE=dev.benyamin.kumiho.debug KUMIHO_ABI=universal \
  bash scripts/android-verify-apk.sh dist/kumiho-android-debug-0.6.0-fxa-oauth.apk
sha256sum dist/kumiho-android-debug-0.6.0-fxa-oauth.apk
```

Expected: verifier reports OK, universal ABI, 16 KB alignment, signature; the
hash is recorded for the user. The debug label is **Kumiho Debug**. No release
version bump or tag.

**Manual acceptance — phone**

The user installs the ARM debug APK and:
1. Adds or re-adds a Firefox account via **Continue in browser**; completes
   TOTP/email/captcha in the in-app **Mozilla account** screen.
2. Confirms the account appears and **Connect** succeeds.
3. Restarts the app; the account remains; the token refreshes without a new
   login (or a unit-tested refresh path is reported).
4. Cancels a flow, pastes a redirect URL manually, and uses the password
   fallback.
5. Confirms sign-out clears the account and the WebView session.

**Manual acceptance — Linux desktop**

```
scripts/dev-daemon.sh start
source scripts/dev-env.sh
make build
dist/kumiho-linux-amd64 login --browser
dist/kumiho-linux-amd64 account
dist/kumiho-linux-amd64 connect
```

Also exercise the TUI paste flow. Windows GUI is covered by Go/JS tests and
cross-build; no Windows manual acceptance is required.

**Stop condition:** if the phone connect fails, capture the sanitized logs and
reopen the design. Do not ship a server-side token detour or a password-only
workaround without approval.

## Risks and rollback

- **Token exchange fails or `/fpn` rejects the new token.** Gate G1 catches it
  before UI work. Stop and reopen; do not continue.
- **FxA blocks WebView.** The Android flow falls back to a Custom Tab plus paste;
  password remains available. The user can still complete v0.6.1.
- **URN copy is awkward.** The browser's error page keeps the URL; the UI
  instructions explain it. Password fallback remains.
- **Legacy removal deletes a browser account.** Intentional, noticed and
  persisted; password accounts and settings are untouched. Re-add the account.
- **Rollback to v0.6.0.** Old browser code cannot use FxA tokens in
  `Account.FxA`; a rollback requires password sign-in or a later build. The
  account file remains readable; no password account is removed.
- **Refresh token rotation.** Always persist a returned refresh token; if the
  server rotates it and the write fails, mark the account re-auth rather than
  retrying with a stale token.
- **WebView security.** No JS bridge, only `accounts.firefox.com`, external
  HTTPS links leave, non-HTTPS blocked, cookies cleared on sign-out.
- **No release/tag/push.** The plan ends at acceptance; release work needs a
  new explicit approval.

## Self-review

- Spec objective/success criteria → Tasks 1–10 and Gate G1.
- FxA client/profile/destroy → Task 1.
- Account storage/refresh/legacy removal → Task 2.
- Engine flow, IPC, mobile command → Task 3.
- CLI proof first → Tasks 4 and G1.
- TUI/GUI/Android → Tasks 5, 6, 7, 8.
- Security/privacy → Tasks 1, 3, 7, 8, 9.
- Docs/old-spec correction → Task 9.
- Gates/APK/phone/Linux → Task 10.
- Names are consistent: `LoginComplete(id, redirectURL)`,
  `LoginCompletePayload.RedirectURL`, `TypeLoginComplete`,
  `MozillaAccountBrowserScreen`, `submitBrowserRedirect`,
  `ActionLoginComplete`.
- Every task states files, exact interfaces, verification and a commit.
- Every task leaves the gate green; Task 0 starts clean; Gate G1 prevents
  another blind APK cycle.
- Open questions: none. Assumed decisions D37–D48 from the spec are flagged;
  D37–D39/D43 are verified at Gate G1.

## Open questions

None.
