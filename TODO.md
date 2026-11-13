# Kumiho TODO

Source: manual testing of the **v0.7.0 Android pre-release** on a phone.
Captured: 2026-10-29.

Status: implemented in the v0.8.0 work; A2 (browser sign-in) is fixed after
v0.8.3 and needs one live sign-in to confirm. D1 and D2 are fixed (2026-11-03).
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
  - Live check: the CLI browser sign-in, a forced token refresh and the Android
    WebView sign-in all passed on 2026-10-31. Unit tests cover the URL, redirect
    and token contracts; the live checks cover the real Mozilla page.

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
  - Superseded 2026-11-11: user-facing README links use the tagged primary
    names from v0.12.1 onward (D3); stable aliases are compatibility-only.

- [x] **C5 — Add repository badges**
  - Done: latest release, total downloads and licence badges, linked to the
    releases page and `LICENSE`.

- [x] **C6 — Update the GitHub repository About panel with releases**
  - Done: the About description now names Android phones and Android TV, the
    homepage points at Releases, and the topics include `android`,
    `android-tv`, `google-tv`, `firefox-accounts`, `compose` and `kotlin`.
  - Added [docs/releasing.md](docs/releasing.md) to the release checklist so
    the About panel is reviewed whenever capabilities change.

- [x] **C7 — Regenerate the Android Locations screenshots**
  - Problem: `screenshots/android-phone/locations.png` and
    `screenshots/android-tv/locations.png` are v0.9.0 captures, but v0.9.1
    draws every server as its own card, so both images no longer match the app.
  - Done (2026-11-02): every phone and TV capture now renders through
    `AppShell` / `TvAppShell` instead of the bare screen, so the bottom bar,
    the rail, the top bar and the overscan frame appear. The TV Settings
    capture passes `accentPreviews`, so the accent buttons show their colour
    swatches like the app, and both Settings captures read
    `BuildConfig.VERSION_NAME` instead of hardcoded versions. All six images
    were regenerated from the running phone and TV emulators.

## Engineering follow-ups

- [x] **D1 — Harden `TestEngineForwardsTCPFlowToDialer` against CI flakes**
  - Observed: the `build-test` race job failed at `d6f7c5c`
    ([run 41077607903](https://github.com/benyamin-git/Kumiho/actions/runs/41077607903),
    job `124137849026`) with
    `--- FAIL: TestEngineForwardsTCPFlowToDialer (10.01s)` /
    `stack_test.go:204: the TCP flow never reached the proxy dialer`.
  - Detail: no packets were logged from `scriptedDevice.wrote`, so the
    SYN-ACK never arrived within the 10-second deadline.
  - Cause: the test queued its SYN before `Start`. The netstack's device
    reader starts inside `CreateStack` (when the NIC is created), while
    promiscuous mode and spoofing are enabled afterwards; a packet to an
    unowned address is dropped in that window, and nothing retransmits the
    SYN. Reproduced at 9 failures in 100 runs under `-race`.
  - Done (2026-11-03): the SYN is fed after `Start` returns (the reader is
    running and both NIC flags are set), the timeout logs the fed packet and
    every written packet, and `go test -race -count=300` passes under eight
    CPU hogs. No deadline change and no retry loop.

- [x] **D2 — Investigate the `android-tv` connected-test flake**
  - Observed: the `android-tv` job failed at `5b72aba`
    ([run 41106804477](https://github.com/benyamin-git/Kumiho/actions/runs/41106804477));
    `build-test`, `windows` and `android` passed on the same commit.
  - Done (2026-11-03): classified from the run log — the job died in **Set up
    Android toolchain** after 44 s with `Warning: An error occurred while
    preparing SDK package Android Emulator: Error on ZipFile unknown archive`,
    before the emulator AAR was built. No connected test ran, so this was a
    truncated SDK download, not a test bug or emulator/boot timing.
    `scripts/android-setup.sh` now drops the cached archive and retries the
    install up to three times; the third failure still fails the job.

- [x] **D3 — Use tagged asset names in the README download tables**
  - Problem: the v0.12.0 `README.md` and `README.fa.md` use
    `/releases/latest/download/...` stable aliases, so a downloaded APK is
    named `Kumiho-Android-arm64-v8a.apk` without the tag. The generated release
    notes already use the tagged primary names.
  - Next release: point both README download tables and their checksum link at
    `.../releases/download/vX.Y.Z/Kumiho-vX.Y.Z-...` and
    `SHA256SUMS-vX.Y.Z`. Keep stable aliases only for `/releases/latest`
    compatibility; do not advertise them or use an ad-hoc file share for the
    handoff.
  - The v0.12.0 README is intentionally unchanged per the user's request.
  - Done 2026-11-12: both READMEs link the v0.12.1 versioned primaries and
    `SHA256SUMS-v0.12.1`.
  - Done 2026-11-12 (v0.13.0): both READMEs link the v0.13.0 versioned
    primaries and `SHA256SUMS-v0.13.0`.
  - Done 2026-11-13 (v1.0.0): both READMEs link the v1.0.0 versioned
    primaries and `SHA256SUMS-v1.0.0`.

## Release follow-ups

- [x] **C8 — Regenerate the Android screenshots for v1.0.0**
  - Problem: the phone and TV captures still showed the pre-1.0 Settings and
    Locations layout; the Windows GUI and TUI captures were current.
  - Done 2026-11-13 (v1.0.0): regenerated the English and Persian phone/TV
    captures through the API 36 emulators and refreshed
    `screenshots/android-phone/`, `screenshots/android-tv/` and the Persian
    README subset.

- [ ] **C9 — Harden the netmon coalescing test**
  - Problem: `TestCoalesceCollapsesBurst` failed once in the v0.13.0 CI race
    run and passed on rerun. The test can consume the first wake-up while the
    coalescer is still draining the burst, so its "exactly one" assertion is
    timing-dependent.
  - Next: make the burst/ack barrier deterministic without weakening the
    cap-1 guarantee.

## Decisions captured on 2026-10-29

- Target the next release, **v0.8.0**.
- Apply UI polish to all clients where the surface exists.
- “GitHub About” means the repository sidebar description/website/topics.
- ~~Use stable, version-free release asset names for direct latest links.~~
  Superseded 2026-11-11: user-facing downloads use the tagged primary names;
  stable aliases are compatibility-only.
- Update check fails on **every attempt**.
- README target: **end users only**; technical material moves to docs.
- Battery palette: green Unrestricted, amber Optimized, red Restricted, grey
  Unknown.
- Phone screenshots: Dashboard, Locations and Settings.
- Android TV screenshots get their own section.
- “What's new” shows only the latest **published** release.

## Decisions captured on 2026-11-11

- From v0.12.1 onward, user-facing download links use the tagged asset name;
  stable aliases are compatibility-only and are not advertised.
- Handoffs use the tagged GitHub asset, not an ad-hoc file share.
- The v0.12.0 README was intentionally unchanged; D3 updated it with v0.12.1.
