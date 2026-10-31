# Kumiho TODO

Source: manual testing of the **v0.7.0 Android pre-release** on a phone.
Captured: 2026-10-29.

Status: implemented in the v0.8.0 work; A2 (browser sign-in) is fixed after
v0.8.3 and needs one live sign-in to confirm. D1 and D2 remain open.
UI polish applies to all clients where the surface exists: Android phone,
Android TV, Windows GUI and the TUI.

## Authentication and updates

- [x] **A1 — Scroll to the password form when adding an account**
  - Problem: choosing password sign-in opens the form below the fold; the
    input is not visible without scrolling.
  - Expected: switching to password sign-in brings the form (email field
    first) into view immediately.
  - Done: `AccountScreen` now owns a `LazyListState` and scrolls to the top
    when `addingAccount` becomes true. Added
    `choosingPasswordReturnsToTheTopOfTheAccountList` on the phone emulator.
  - Also check: preserve entered text, focus the first field only when
    appropriate, and verify the same flow on Android TV and the desktop
    clients. Android TV and desktop use different account surfaces without
    this account-list scroll.

- [x] **A2 — Fix browser sign-in when adding an account**
  - Problem: the browser page loaded and recognised the account, but choosing
    **Sign in** did nothing. The failure reproduced in Chrome, Brave and
    Firefox, and the app never received a redirect.
  - Cause: Firefox Desktop's OAuth client (`5882386c6d801776`) is tied to
    Firefox's in-process WebChannel. Its URN redirect cannot be delivered to a
    standalone app; Chromium blocks the `urn:` navigation without showing an
    error, and the content server rejects that URN when it is sent explicitly
    as `redirect_uri` (**Bad Request: Invalid Query Parameters**). Omitting
    `redirect_uri` fixed the page load but still left the browser at the dead
    URN.
  - Fixed: browser sign-in uses the public Firefox for Android OAuth client
    (`a2270f727f45f648`) and its registered HTTPS success page,
    `accounts.firefox.com/oauth/success/...`. Any ordinary browser can navigate
    there and show the complete URL for paste-back; the Android WebView
    intercepts that HTTPS URL directly. Browser tokens record the issuing
    client in `Tokens.ClientID`, and refresh sends the same client, so browser
    and password accounts renew correctly.
  - Live check: complete one browser sign-in on the CLI and one in the Android
    WebView. Unit tests cover the URL, redirect and token contracts, not the
    real Mozilla consent page.

- [x] **A3 — Fix “Check for update” returning HTTP 404**
  - Problem: update check reports `github answered HTTP 404` on **every
    attempt** on v0.7.0. It worked in previous versions.
  - Cause: every v0.x release is a GitHub prerelease, so
    `/releases/latest` returns 404 for the whole series.
  - Done: Android and Windows GUI now read the release list, skip drafts and
    take the newest published release (prereleases included). Tests cover the
    list endpoint, draft skipping, prerelease releases and empty/failed
    replies. Android phone/TV use the same checker.

## Client UI polish

- [x] **B1 — Group Settings sections into cards**
  - Done on Android phone: App, Connection, Android VPN and Advanced groups
    are cards using the same `KumihoCard` treatment as Account and Home.
  - Done on Android TV: app controls are inside a section card; connection and
    Android rows already use focusable card surfaces.
  - Done on Windows GUI: every Settings section has a card surface and
    hairline border.
  - TUI: no card surface exists; it keeps its section headings.

- [x] **B2 — Add spacing between Diagnostics cards**
  - Done: the Split tunneling card has the standard `Dimens.Space3` gap below
    the VPN service card.

- [x] **B3 — Use semantic colours for the battery-optimisation pill**
  - Done: green **Unrestricted**, amber **Optimized**, red **Restricted** and
    grey **Unknown**, with the text label kept. Added the pure
    `batteryOptimizationState` mapping and `BatteryOptimizationTest`; phone and
    TV both use it.

- [x] **B4 — Make “Overridden” neutral, not yellow**
  - Done: the phone override chip is `StatusTone.Neutral`; the TV override text
    was already neutral.

- [x] **B5 — Keep the bottom bar labels unwrapped on narrow phones**
  - Problem: on a narrower phone (found on v0.8.1), the **Locations** label
    wrapped its last letter because the five bottom-bar labels did not fit.
  - Expected: the bar gives a page up to **More** rather than wrapping a label.
  - Done: the phone bar measures its widest label at the bar's own type scale
    and drops **Settings**, then **Account**, into **More** as the width or the
    system font scale runs out. Home, Locations and More always stay, the
    navigation rail still shows all five, the More sheet lists the moved pages
    first, and More stays marked while one of them is open. Covered by
    `ShellNavigationTest` and a narrow-width `ShellMoreRoutesTest` case.

## README and GitHub presentation

- [x] **C1 — Shorten and restructure the README**
  - Done: end-user introduction, download table and screenshots are near the
    top. Build/test/architecture material moved to `docs/development.md`;
    protocol detail stays in `PLAN.md`.

- [x] **C2 — Keep only the latest release in “What's new”**
  - Done: only **What's new in v0.8.0** remains; older notes stay in
    `CHANGELOG.md`.

- [x] **C3 — Add Android phone and Android TV screenshots**
  - Done: `screenshots/android-phone/` has Dashboard, Locations and Settings;
    `screenshots/android-tv/` has Home, Locations and Settings in its own
    README section. The capture procedure is in
    `scripts/screenshots/README.md`.

- [x] **C4 — Add a formatted Download section**
  - Done: README and generated release notes use the stable names
    `Kumiho-Linux-amd64.zip`, `Kumiho-Linux-arm64.zip`,
    `Kumiho-Windows-amd64.zip`, `Kumiho-Android-arm64-v8a.apk`,
    `Kumiho-Android-armeabi-v7a.apk`, `Kumiho-Android-universal.apk` and
    `SHA256SUMS`.
  - The workflow publishes these names from v0.8.0; v0.8.0 is published
    (2026-10-30). `scripts/install-release.sh` falls back to the old versioned
    names for v0.7.2 and earlier.
  - Full releases start at v0.8.0 so `/releases/latest/download/...` works;
    v0.7.2 and earlier remain prereleases.

- [x] **C5 — Add repository badges**
  - Done: latest release, total downloads and licence badges, linked to the
    releases page and `LICENSE`.

- [x] **C6 — Update the GitHub repository About panel with releases**
  - Done: the About description now names Android phones and Android TV, the
    homepage points at Releases, and the topics include `android`,
    `android-tv`, `google-tv`, `firefox-accounts`, `compose` and `kotlin`.
  - Added [docs/releasing.md](docs/releasing.md) to the release checklist so
    the About panel is reviewed whenever capabilities change.

## Engineering follow-ups

- [ ] **D1 — Harden `TestEngineForwardsTCPFlowToDialer` against CI flakes**
  - Observed: the `build-test` race job failed at `d6f7c5c`
    ([run 41077607903](https://github.com/benyamin-git/Kumiho/actions/runs/41077607903),
    job `124137849026`) with
    `--- FAIL: TestEngineForwardsTCPFlowToDialer (10.01s)` /
    `stack_test.go:204: the TCP flow never reached the proxy dialer`.
  - Detail: no packets were logged from `scriptedDevice.wrote`, so the
    SYN-ACK never arrived within the 10-second deadline.
  - Action: make the handshake deterministic instead of timing-based — feed
    the SYN once the stack's reader is confirmed running, retry the SYN-ACK
    answer, and log the fed packet plus device state on timeout. A longer
    deadline is a last resort.
  - Passed locally and on the v0.8.2 tag (`fb0e5fd`).

- [ ] **D2 — Investigate the `android-tv` connected-test flake**
  - Observed: the `android-tv` job failed at `5b72aba`
    ([run 41106804477](https://github.com/benyamin-git/Kumiho/actions/runs/41106804477));
    `build-test`, `windows` and `android` passed on the same commit.
  - Action: pull the connected-test report artifact and classify the failure
    as a real test bug or emulator/boot timing before adding any retry.

## Decisions captured on 2026-10-29

- Target the next release, **v0.8.0**.
- Apply UI polish to all clients where the surface exists.
- “GitHub About” means the repository sidebar description/website/topics.
- Use stable, version-free release asset names for direct latest links.
- Update check fails on **every attempt**.
- README target: **end users only**; technical material moves to docs.
- Battery palette: green Unrestricted, amber Optimized, red Restricted, grey
  Unknown.
- Phone screenshots: Dashboard, Locations and Settings.
- Android TV screenshots get their own section.
- “What's new” shows only the latest **published** release.
