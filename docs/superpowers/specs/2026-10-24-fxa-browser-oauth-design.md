# Firefox Accounts browser OAuth — Design Spec

**Status:** Approved
**Date:** 2026-10-24
**Supersedes:** the browser-sign-in token assumption in
`2026-10-23-v0.6.0-roadmap-design.md` and the shipped v0.6.0 browser flow.

## Objective / why

Browser sign-in is shipped but cannot produce a token that the free Firefox
VPN (FPN) API accepts. The Mozilla VPN browser exchange returns an opaque
token for the paid VPN API; `/api/v1/fpn/status`, `/api/v1/fpn/token` and
`/api/v1/fpn/activate` all reject it. Password sign-in works because it mints a
real Firefox Accounts OAuth token with the `apps/vpn` scope.

v0.6.1 replaces the browser token source with **direct Firefox Accounts OAuth**
using the project's existing Firefox client (`5882386c6d801776`), PKCE and the
`profile https://identity.mozilla.com/apps/vpn` scope. Android completes the
flow in an in-app WebView; desktop clients open the system browser and accept
the final `urn:` redirect URL as a paste. The resulting access/refresh tokens
are ordinary FxA tokens, stored in the existing `Account.FxA` field, refreshed
through the existing path, and used for `/fpn` like a password login.

The old browser flow, Android Custom Tabs/deep-link return and opaque browser
token recovery are removed rather than kept as a fallback.

## Success criteria / definition of done

1. On a physical Android phone, **Continue in browser** opens a full-screen
   in-app Mozilla account WebView; completing sign-in (including TOTP, email
   verification or a captcha) adds or re-authenticates the account without a
   password.
2. The stored account uses `MethodBrowser` with FxA access and refresh tokens.
   `/fpn/status` accepts the token and **Connect** succeeds on the phone.
3. The access token refreshes silently after expiry; a failed refresh marks
   the account for re-authentication instead of deleting it or asking for a
   password.
4. On Linux, `kumiho login --browser` and the TUI/GUI paste flows accept the
   complete redirect URL, complete the same exchange, and connect.
5. The Windows GUI exposes the same paste flow and is covered by Go/JS tests
   and cross-builds; no Windows manual acceptance is required for v0.6.1.
6. Existing opaque browser accounts are removed automatically on first load,
   with the notice **“Old browser sign-in removed. Add your Firefox account
   again.”** Password accounts are untouched.
7. The old `mozilla-vpn://login/success` manifest filter, deep-link parser,
   Custom Tabs launcher, Mozilla VPN login/verify exchange and browser-token
   activation recovery are deleted.
8. No authorization code, PKCE verifier, redirect URL, access token or refresh
   token appears in logs, crash reports or user-facing errors.
9. Sign-out best-effort revokes the FxA refresh token, always clears local
   state, and clears the Android WebView's Firefox Accounts cookies.
10. The full gate passes: `make lint && make test && go test -race -count=1
    ./... && make build`, Windows/Android cross-builds, `make android-test`,
    and the focused Android tests.
11. `README.md`, `AGENTS.md`, `docs/android.md`, `docs/usage.md`,
    `docs/gui.md`, `docs/manual.md`, `CHANGELOG.md` and the old v0.6.0 spec
    describe the new flow and the ToS gray area accurately.
12. No tag, push, release or version bump happens without explicit approval.

## Scope

### In scope

- Direct FxA authorization-code OAuth with PKCE S256, `state`, and
  `access_type=offline`.
- Authorization, token exchange, profile lookup and best-effort token destroy
  in `internal/fxa`.
- Engine-owned browser flow with a 10-minute deadline, start/status/complete/
  cancel, and no loopback listener.
- Account storage in `Account.FxA`; durable re-auth marking; refresh.
- Automatic removal of legacy opaque browser accounts.
- Android full-screen WebView, redirect interception, cookie handling and
  paste/password fallback.
- CLI, TUI and Windows GUI paste flows.
- Sign-out revocation and WebView cookie clearing.
- Tests, documentation and the v0.6.1 implementation plan.

### Non-goals (explicitly out)

- Registering a new Mozilla OAuth client or a custom HTTPS/custom-scheme
  redirect.
- OOB “copy the code” pages; Firefox client `5882386c6d801776` has no such
  page.
- Keeping the Mozilla VPN browser flow or paid VPN token support.
- Changing the password/TOTP/unblock flow, except shared token refresh and
  logout handling.
- Multi-account semantics, rotation or quota changes.
- v0.8.0 region/location work.
- iOS, desktop embedded WebView2 login, or an Android Custom Tab flow as the
  primary browser path.
- Releasing, tagging, pushing or publishing anything.

## Decision log

| # | Decision | Rationale | Status | Source |
| --- | --- | --- | --- | --- |
| D1 | Cover Android, Linux/CLI, TUI, Windows GUI and the shared engine in v0.6.1. | The token source is shared; leaving desktop on the broken flow would preserve the same bug. | decided | user |
| D2 | Use a direct FxA authorization-code flow, not the Mozilla VPN browser exchange. | MOZILLA VPN returns an opaque token that `/fpn` rejects; password login proves FxA OAuth works. | decided | user |
| D3 | Android returns the code by intercepting the registered `urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel` redirect inside an in-app WebView. | Client 588 has no HTTP redirect and no OOB code page; WebView can receive the URN before it leaves the app. | decided | user |
| D4 | Desktop clients open the system browser and accept the complete redirect URL as a paste. | The same URN is visible in the browser after the failed navigation; no registered desktop redirect exists. | decided | user |
| D5 | An in-app WebView is acceptable with no JavaScript bridge, no file/content access, no mixed content and only the expected redirect intercepted. | Mozilla's Android FxA components support WebView/Custom Tab; the app never reads the credential fields. | decided | user |
| D6 | Remove the old Mozilla VPN browser flow, Android Custom Tabs/deep link, `mozilla-vpn` manifest filter and opaque-token activation recovery. | They cannot produce an FPN token and would keep a false fallback. | decided | user |
| D7 | Android falls back to paste + password if the WebView fails; password remains the final fallback. | The user must never be stranded by a WebView or browser problem. | decided | user |
| D8 | Store browser OAuth tokens in `Account.FxA` and keep `MethodBrowser`; retain `BrowserToken` only for legacy decoding/removal. | Reuses the generic refresh path and avoids a new account method/protocol value. | decided | user |
| D9 | Remove legacy opaque browser accounts automatically on first load and keep password accounts. | The old tokens cannot work; preserving dead accounts only produces false re-auth loops. | decided | user |
| D10 | Fetch the FxA profile after token exchange to obtain UID/email/display name. | The authorization response has no user identity; the `profile` scope is already requested. | decided | user |
| D11 | Re-authentication must use the stored email; a different account is rejected and its new tokens discarded. | Prevents silently replacing the wrong account; keeps labels and IDs meaningful. | decided | user |
| D12 | Sign-out best-effort revokes the FxA refresh token, then always deletes local state. | Follows Mozilla OAuth guidance while keeping offline sign-out reliable. | decided | user |
| D13 | Target v0.6.1; do not tag/push before the phone test and explicit approval. | This is a post-v0.6.0 bug fix, not v0.8.0 roadmap work; v0.7.0 became Android TV. | decided | user |
| D14 | Expose the paste flow in CLI, TUI and Windows GUI. | All desktop clients share the engine and should not diverge silently. | decided | user |
| D15 | Android WebView is a full-screen dedicated screen with a top bar. | Avoids dialog sizing/clipping and gives the login page full room. | decided | user |
| D16 | Keep the labels **Continue in browser** and **Browser**. | The in-app WebView is still a browser surface; minimal copy churn. | decided | user |
| D17 | Browser flows expire after 10 minutes. | Enough for 2FA/captcha while staying under FxA's 15-minute code lifetime. | decided | user |
| D18 | Paste accepts the complete redirect URL only; the engine validates both code and state. | Keeps the OAuth state check; a bare code would skip it. | decided | user |
| D19 | Keep only the valid `VpnServiceCoordinator` cold-start fix, commit it separately, and discard the old deep-link/webauth/activation work. | The cold-start fix is independent and useful; the rest is superseded. | decided | user |
| D20 | Persist Android WebView cookies across attempts; clear them on sign-out. | Avoids repeated Mozilla logins, while sign-out removes the browser session. | decided | user |
| D21 | On profile lookup failure, keep the exchanged tokens in the active flow and retry the profile only. | Avoids repeating a valid browser login; the user chose a retry, not fallback identity. | decided | user |
| D22 | Extend `internal/fxa` with OAuth/profile/destroy code and remove `internal/webauth`. | The FxA package already owns client/scope/token refresh; one package is enough. | decided | user |
| D23 | Manual acceptance: real Android phone plus Linux CLI/TUI; Windows GUI automated only. | No Windows test device; the Windows surface is still built and tested. | decided | user |
| D24 | New docs: `2026-10-24-fxa-browser-oauth-design.md` and `2026-10-24-fxa-browser-oauth.md`. | Clear, release-agnostic topic naming. | decided | user |
| D25 | Re-auth passes the stored email with `action=force_auth`; add-account prefills a supplied email but allows a change. | Prevent a silent account switch while keeping add-account flexible. | decided | user |
| D26 | Add `redirect_url` to `LoginCompletePayload`; keep `code` for protocol shape but the new browser flow rejects it. | Full URL preserves state validation; the field addition is protocol-compatible. | decided | user |
| D27 | A bad paste/transient exchange error keeps the flow active for retry; expired/used/wrong-client codes are terminal. | The user can fix a typo without restarting; dead codes should not loop. | decided | user |
| D28 | Android WebView keeps `accounts.firefox.com` inside; other HTTPS links open in the system browser; non-HTTPS is blocked. | Keeps the credential surface on Mozilla's account origin. | decided | user |
| D29 | Android paste fallback is an expandable **Paste redirect URL instead** section on the waiting card, with **Use password** beside it. | Discoverable without making the normal path look broken. | decided | user |
| D30 | The old v0.6.0 spec gets a superseded note; README/AGENTS/Android docs are corrected during implementation. | The shipped docs currently claim a Guardian token that never worked. | decided | user |
| D31 | Removal notice copy: **“Old browser sign-in removed. Add your Firefox account again.”** | States the event and the next action in one line. | decided | user |
| D32 | Android WebView title: **Mozilla account**. | Names the service, not Kumiho's form. | decided | user |
| D33 | Paste controls: **Paste redirect URL instead** → **Redirect URL** field → **Continue**, plus **Use password**. | Direct, labelled, and keeps the fallback obvious. | decided | user |
| D34 | Sign-out dialog mentions the best-effort Mozilla revoke and that local sign-out still completes offline. | Sets expectations without making revocation the headline. | decided | user |
| D35 | If the Android WebView cannot be created, open the same URL in a Custom Tab/system browser and reveal the paste field. | Preserves browser sign-in on devices without a usable WebView. | decided | user |
| D36 | Implementation order: FxA client + engine + CLI paste first; prove `/fpn/status` with a real account; then TUI/GUI/Android. | Stops another blind APK cycle and de-risks the token before UI work. | decided | user |
| D37 | Use existing Firefox client `5882386c6d801776`, scope `profile https://identity.mozilla.com/apps/vpn`, redirect `urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel`, and `access_type=offline`. | These are the project's existing FPN client constants; a new client is out of scope. | assumed | recommendation |
| D38 | Use PKCE S256 with a 43-character base64url verifier, a random `state`, and the authorization endpoint `https://accounts.firefox.com/authorization`. | FxA validates a 43-character verifier; SHA-256 base64url yields a 43-character challenge. | assumed | recommendation |
| D39 | Exchange at `https://oauth.accounts.firefox.com/v1/token`, fetch identity at `https://profile.accounts.firefox.com/v1/profile`, destroy at `https://oauth.accounts.firefox.com/v1/destroy`, and refresh through the existing `fxa.Refresh`. | These are the production FxA endpoints from discovery and OAuth docs. | assumed | recommendation |
| D40 | Add a top-level durable `reauth_required` flag for FxA-backed browser accounts; keep `accounts.json` v2 and the legacy `BrowserToken` decode-only. | The legacy browser flag lives inside the token that is being removed; the schema change is additive. | assumed | recommendation |
| D41 | Legacy removal runs during engine start; set the accounts notice in memory for the first client read, select the most recently used remaining account if the active one was removed, and sync `tokens.json`. | Makes the upgrade deterministic and avoids a stale active token. | assumed | recommendation |
| D42 | Profile retry reuses `login_complete` for the same login id: if tokens are already exchanged, retry only the profile call. | No new IPC command; keeps tokens alive in the flow. | assumed | recommendation |
| D43 | Error classification: malformed URL/wrong state → retry; network/5xx → retry; `invalid_grant`/expired/used/wrong client → terminal; profile 401 → discard tokens, otherwise retry. | Matches FxA's stable error semantics and avoids consuming codes twice. | assumed | recommendation |
| D44 | WebView hardening: JavaScript and DOM storage on; file/content access off; mixed content never; multiple windows off; downloads ignored; no JS interface. | Required for the login page while keeping credentials inside Mozilla's page. | assumed | recommendation |
| D45 | Desktop UX detail: CLI prints the URL and prompts on stdin; TUI waiting card gets a paste field; GUI waiting cards get a field and Submit; system browser opens unless `--no-open`. | Consistent with each client's existing interaction model. | assumed | recommendation |
| D46 | Tests: FxA URL/exchange/profile/refresh/destroy mocks, engine flow/migration/reauth, CLI/TUI/GUI IPC, Android redirect/URL policy/paste/cookie tests, plus existing gates. | Each layer must be verifiable without a real Mozilla login. | assumed | recommendation |
| D47 | Docs: correct README/AGENTS/Android/usage/GUI/manual/CHANGELOG and add the old-spec superseded note; document the ToS gray area. | The current docs describe the disproved Guardian-token assumption. | assumed | recommendation |
| D48 | Keep the password flow behavior unchanged except shared FxA refresh and best-effort logout revocation. | v0.6.1 is a browser fix, not a password-flow redesign. | assumed | recommendation |

## Architecture

### Firefox Accounts client (`internal/fxa`)

Add an OAuth browser surface next to the existing password login:

- `AuthorizationURL(email string, forceAuth bool, state, codeChallenge string) string`
  builds the FxA authorization URL with the client, scope, redirect, PKCE,
  `state`, `access_type=offline`, and optional email/`action=force_auth`.
- `ExchangeCode(ctx, code, verifier, redirectURI string) (*Tokens, error)` posts
  `grant_type=authorization_code`, `client_id`, `code`, `code_verifier` and
  `redirect_uri` to the OAuth token endpoint. It parses access token, refresh
  token, scope and `expires_in` with the existing token parser.
- `Profile(ctx, accessToken string) (*Profile, error)` calls the profile
  endpoint and returns `UID`, `Email`, `DisplayName` and `Avatar`.
- `Destroy(ctx, token string) error` posts the refresh token to the destroy
  endpoint for best-effort sign-out revocation.
- Existing `Refresh` remains the refresh path; a rotated refresh token replaces
  the stored one.
- The 43-character PKCE verifier and SHA-256 challenge live in this package.

No token, code, verifier or redirect URL is logged.

### Engine browser flow (`internal/engine`)

`LoginStart` and `AccountsAddStart` create an in-memory browser flow:

```
id, state, verifier, URL, target account id, adding, email, created_at,
tokens (after exchange), profile_error, active
```

The flow has no listener and no browser dependency. `LoginStatus` reports
`browser` until completion; `LoginComplete` accepts the full redirect URL,
verifies `state`, exchanges the code, fetches the profile, and creates/replaces
the account. `LoginCancel` clears the flow; the 10-minute deadline does the same
on the next status/complete.

Account rules:

- New/re-authenticated browser accounts use `MethodBrowser` + `Account.FxA`.
- Re-auth requires the same email; a different profile email is a terminal
  mismatch and the new tokens are discarded.
- Add-account with a matching email replaces tokens and preserves the label;
  a new email adds an account subject to the five-account cap.
- `EnsureFreshToken` and `acquirePass` use the generic FxA refresh path for
  browser accounts. A permanent refresh rejection sets `reauth_required` and
  clears only that account's active session.
- No browser-token activation path remains.
- `logout` best-effort destroys each stored refresh token, then clears accounts,
  `tokens.json`, the active session and the Android WebView cookie jar.

### Legacy cleanup

On engine start, remove accounts that have `MethodBrowser`, no `FxA`, and a
legacy `Browser` token. Keep password accounts and password FxA tokens. Drop
legacy `Browser` fields from password accounts if present. If the active account
was removed, select the most recently used remaining account or sign out. Sync
`tokens.json` and expose the one-time notice through `AccountsView.Notice`.

### IPC and clients

- `LoginStartPayload` and `LoginState` keep their existing shape.
- `LoginCompletePayload` gains `redirect_url`; `code` stays in the struct but
  the new browser flow rejects it.
- CLI: prompt for the complete redirect URL after opening the browser; submit
  `login_complete`; poll until done/error.
- TUI: the existing browser waiting card gains a labelled input and submit/
  cancel actions; the existing password fallback remains.
- Windows GUI: account and onboarding waiting cards gain a paste field and
  Submit; the controller and mock gain `login_complete`; the browser is opened
  once per flow.
- Android: replace `AccountBrowser`/`BrowserDeepLink`/MainActivity deep-link
  handling with a full-screen `MozillaAccountWebView` screen. It validates the
  initial URL, intercepts the URN redirect, and submits the full URL. A waiting
  card exposes the expandable paste field, **Use password**, and Cancel. If the
  WebView cannot be created, open the URL in a Custom Tab and keep the paste
  field available.

### Security and privacy

- PKCE and `state` are mandatory. The WebView has no JavaScript bridge; the
  app reads only the intercepted redirect URL.
- The WebView permits only `https://accounts.firefox.com` in the main frame;
  other HTTPS links leave to the system browser; non-HTTPS is blocked.
- Cookies persist until sign-out, then are cleared with the local session.
- FxA tokens live in the existing 0600 app-private account file; Android uses
  no-backup storage. Logs sanitize email and never include tokens/codes.
- Best-effort revocation does not block offline sign-out.
- The flow remains the same documented ToS gray area as the password client.

## Risks + rollback

- **FxA rejects the direct authorization/exchange or `/fpn` rejects the
  resulting token.** The CLI proof task runs before any Android/TUI/GUI UI; if
  it fails, stop and reopen the design. Do not build another APK blindly.
- **FxA blocks embedded WebViews.** The Android flow still works through
  Custom Tabs + paste, then password. The WebView is an optimization, not the
  only path.
- **The `urn:` redirect is not copyable in some browser.** The user can use the
  password fallback; the CLI/TUI/GUI instructions explain that the browser's
  “can't open this address” page is expected and the address bar holds the URL.
- **Upgrade removes old browser accounts.** This is intentional and noticed; the
  user re-adds the account. Password accounts and local settings are untouched.
- **Rollback to v0.6.0.** The old browser code cannot use FxA tokens stored in
  `Account.FxA`; a rollback would require password sign-in or a later build.
  The account file remains readable, and no password account is removed.
- **Refresh token loss or revocation.** Permanent refresh rejection marks the
  account reauth-required and keeps the account row for a fresh browser login.
- **WebView/cookie privacy.** Sign-out clears cookies; a user who never signs
  out retains a Mozilla browser session in app storage, as with any browser.
- No release/tag/push until the phone test and explicit approval.

## Dependencies / ordering

1. Preserve and commit the independent cold-start fix; discard the superseded
   dirty work.
2. FxA OAuth client (URL, exchange, profile, refresh, destroy) with mocks.
3. Engine browser flow, account storage/refresh, legacy cleanup, IPC.
4. CLI paste flow.
5. **CLI proof gate:** real account → `/fpn/status` → connect. Stop if this
   fails.
6. TUI and Windows GUI paste flows.
7. Android WebView, paste/password fallback, cookie handling.
8. Remove the old flow/deep-link code and update docs.
9. Gates, debug APK, physical-phone test, then approval for release.

The plan will make each step a commit-sized task with exact files and
verification.

## Open questions

None. Technical assumptions D37–D48 are flagged for review; D37–D39 and D43
are verified by the CLI proof gate before UI work.
