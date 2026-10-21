# README screenshots

`capture.sh` regenerates the six committed images under `screenshots/`:

- `screenshots/windows-gui/` — the real GUI assets served by the Windows
  WebView2 shell, captured in the Linux dev harness at 1280×800 in the Dark
  theme. The images are the app content; the native Windows title bar and tray
  are not included.
- `screenshots/linux-tui/` — the real Bubble Tea views, rendered from
  `internal/tui/capture_test.go` with fixed sample data at 100×28 and framed
  as a terminal window.

Run from the repository root:

    scripts/screenshots/capture.sh

The script rebuilds the web harness with `Version=v0.4.0` (override with
`KUMIHO_CAPTURE_VERSION`), starts it on `0.0.0.0:7407`, and restarts it before
each screen so a notice from one state cannot leak into the next. It stops the
harness when done.

## Prerequisites

- Go 1.27.1.
- Node 22+ and a primed Playwright cache. The GUI capture uses
  `scripts/visual/capture.mjs`; the TUI renderer uses `npx playwright
  screenshot`. The resolution order and cache setup are documented in
  `scripts/visual/README.md`.
- No root and no system changes; the harness serves the mock daemon only.

## How it works

The GUI images come from the existing visual-review manifest:

| Output | Source |
| --- | --- |
| `windows-gui/dashboard.png` | `dashboard/dark-connected.png` |
| `windows-gui/locations.png` | `locations/dark-speed-test.png` |
| `windows-gui/settings.png` | `settings/dark.png` |

The TUI side runs the normal test package with `KUMIHO_TUI_CAPTURE_DIR` set,
which makes `TestCaptureViews` write raw ANSI for the dashboard, locations and
settings views. `render-tui.mjs` converts the SGR colour codes to styled spans,
draws the terminal frame, and asks Playwright for a full-page PNG. It maps the
TUI's ANSI accents (42, 203, 208, 214, 244) to the house palette so the
terminal and GUI captures sit together. The TUI sample data is fixed and uses
the reserved `example.com` domain, `user@example.com` and TEST-NET-3 addresses.

The full visual-review output stays under `/tmp/opencode/captures` and is not
committed; this script copies only the six selected images into `screenshots/`.

The script leaves `dist/kumiho-gui-web-linux-amd64` built with the release
version label. Run `make build-web` to restore the normal git-describe build.

These images are documentation assets, not test fixtures: no test reads them
and the build gate does not run this directory.
