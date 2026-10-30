# Kumiho TODO

Source: manual testing of the **v0.7.0 Android pre-release** on a phone.
Captured: 2026-10-29.

Status: **implemented in the v0.8.0 work except A2**, which stays open.
A2 remains out of scope for this pass; do not treat it as fixed.
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

- [ ] **A2 — Fix browser sign-in when adding an account**
  - Problem: the browser flow still does not complete on the v0.7.0 Android
    pre-release downloaded from GitHub.
  - Observed: Mozilla's page shows **Bad Request: Invalid Query Parameters**
    (error ID `3bcf11f926a940ab99778fcb728efaef`) and reports that
    `redirectUri` failed the `IsFxaRedirectUri` constraint. The failure happens
    before the app can receive a redirect.
  - Expected: **Continue in browser** completes the Firefox Accounts flow and
    returns to the app with the account added.
  - Investigate: the configured FxA OAuth client and its allowed redirect URI
    (including whether the WebChannel `urn:` redirect is accepted), and capture
    `adb logcat` plus the complete authorization URL if the page error
    persists.

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
  - The workflow publishes these names from v0.8.0. `scripts/install-release.sh`
    falls back to the old versioned names for v0.7.2 and earlier.
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
