# README screenshots

`capture.sh` regenerates the six desktop images under `screenshots/`:

- `screenshots/windows-gui/` — the real GUI assets served by the Windows
  WebView2 shell, captured in the Linux dev harness at 1280×800 in the Dark
  theme. The images are the app content; the native Windows title bar and tray
  are not included.
- `screenshots/linux-tui/` — the real Bubble Tea views, rendered from
  `internal/tui/capture_test.go` with fixed sample data at 100×28 and framed
  as a terminal window.

Run from the repository root:

    scripts/screenshots/capture.sh

The script rebuilds the web harness with the v0.8.0 UI target label (override
with `KUMIHO_CAPTURE_VERSION`), starts it on `0.0.0.0:7407`, and restarts it
before each screen so a notice from one state cannot leak into the next. It
stops the harness when done. The label is a screenshot target, not an app
version bump.

## Prerequisites

- Go 1.27.1.
- Node 22+ and a primed Playwright cache. The GUI capture uses
  `scripts/visual/capture.mjs`; the TUI renderer uses `npx playwright
  screenshot`. The resolution order and cache setup are documented in
  `scripts/visual/README.md`.
- The real 4:3 GUI SVG flags under
  `internal/gui/web/static/flags/4x3/` and Noto Color Emoji, so the TUI
  country flags render as flags instead of fallback letters:

      sudo apt-get install -y fonts-noto-color-emoji
      fc-cache -f
      fc-list : family | grep -i 'Noto Color Emoji'

  The script fails early when `fc-list` cannot find the font. The harness
  itself serves the mock daemon only and needs no other system change.

## How it works

The GUI images come from the existing visual-review manifest:

| Output | Source |
| --- | --- |
| `windows-gui/dashboard.png` | `dashboard/dark-connected.png` |
| `windows-gui/locations.png` | `locations/dark-speed-order.png` |
| `windows-gui/settings.png` | `settings/dark.png` |

The TUI side runs the normal test package with `KUMIHO_TUI_CAPTURE_DIR` set,
which makes `TestCaptureViews` write raw ANSI for the dashboard, locations and
settings views. `render-tui.mjs` converts the SGR colour codes to styled spans,
draws the terminal frame, and asks Playwright for a full-page PNG. It maps the
TUI's ANSI accents (42, 203, 208, 214, 244) to the house palette so the
terminal and GUI captures sit together. The TUI sample data is fixed and uses
the reserved `example.com` domain, `user@example.com` and TEST-NET-3 addresses.

The full visual-review output stays under `/tmp/opencode/captures` and is not
committed; this script copies only the six selected desktop images into
`screenshots/`.

## Android captures

Run the phone screenshots on the phone emulator and the TV screenshots on the
TV emulator. The AGP connected task uninstalls the app after the run, which
deletes its external files, so install the already-built APKs and run the
instrumentation directly:

    source scripts/android-env.sh
    adb -s emulator-5554 install -r android/app/build/outputs/apk/debug/app-debug.apk
    adb -s emulator-5554 install -r android/app/build/outputs/apk/androidTest/debug/app-debug-androidTest.apk
    adb -s emulator-5554 shell am instrument -w \
      -e class 'dev.benyamin.kumiho.ui.dashboard.DashboardScreenshotsTest,dev.benyamin.kumiho.ui.locations.LocationsScreenshotsTest,dev.benyamin.kumiho.ui.settings.SettingsScreenshotsTest' \
      dev.benyamin.kumiho.debug.test/androidx.test.runner.AndroidJUnitRunner
    adb -s emulator-5554 pull \
      /sdcard/Android/data/dev.benyamin.kumiho.debug/files/dashboard-screenshots
    adb -s emulator-5554 pull \
      /sdcard/Android/data/dev.benyamin.kumiho.debug/files/locations-screenshots
    adb -s emulator-5554 pull \
      /sdcard/Android/data/dev.benyamin.kumiho.debug/files/settings-screenshots

Copy the Dark captures `t11-connected-dark.png`, `t12-tree-dark.png` and
`t13-app-dark.png` to `screenshots/android-phone/{dashboard,locations,settings}.png`.
For the TV, run `TvScreenshotsTest` on `emulator-5556`, pull `tv-screenshots`,
and copy `t13-{home,locations,settings}-dark.png` to
`screenshots/android-tv/{home,locations,settings}.png`.

The tests render the real Compose screens with fixed sample data at the
emulators' native sizes (1080×2400 phone, 1920×1080 TV).

The script leaves `dist/kumiho-gui-web-linux-amd64` built with the release
version label. Run `make build-web` to restore the normal git-describe build.

These images are documentation assets, not test fixtures: no test reads them
and the build gate does not run this directory.
