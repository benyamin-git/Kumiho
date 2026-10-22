#!/usr/bin/env bash
# Regenerate the README screenshots: the Windows GUI's real UI assets via the
# Linux dev harness, and the real Linux TUI views via TestCaptureViews.
#
# The GUI harness runs on 0.0.0.0:7407 (the project's registered dashboard
# port) and is restarted before each screen so no notice leaks between
# captures. The script stops it when done.
#
# The GUI binary is rebuilt with Version=v0.5.1 (override with
# KUMIHO_CAPTURE_VERSION) so the Settings screen shows the release version,
# not the git-describe development label.
#
# Prerequisites: Go, Node 22+, a primed Playwright cache (see
# scripts/visual/README.md), and Noto Color Emoji so flags render as flags.
# The TUI renderer uses npx playwright.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
cd "$repo"

captures="${KUMIHO_CAPTURES_DIR:-/tmp/opencode/captures}"
tui_tmp="${KUMIHO_TUI_CAPTURE_DIR:-/tmp/opencode/kumiho-tui-capture}"
version="${KUMIHO_CAPTURE_VERSION:-v0.5.1}"
commit="$(git rev-parse --short HEAD)"
date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if command -v fc-list >/dev/null 2>&1 &&
   ! fc-list : family | grep -qi 'Noto Color Emoji'; then
  echo "error: install fonts-noto-color-emoji so the flag captures are real" >&2
  exit 1
fi

export KUMIHO_CAPTURES_DIR="$captures"
rm -rf "$tui_tmp"
mkdir -p "$repo/screenshots/windows-gui" "$repo/screenshots/linux-tui" "$tui_tmp"

cleanup() {
  "$repo/scripts/dev-gui-web.sh" stop >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Stop a harness from a previous run before replacing its binary, so the
# pidfile still matches the executable name and the dev script can stop it.
"$repo/scripts/dev-gui-web.sh" stop >/dev/null 2>&1 || true

echo "building the GUI web harness as $version"
go build -trimpath \
  -ldflags "-s -w -X github.com/benyamin-git/kumiho/internal/version.Version=$version -X github.com/benyamin-git/kumiho/internal/version.Commit=$commit -X github.com/benyamin-git/kumiho/internal/version.Date=$date" \
  -o dist/kumiho-gui-web-linux-amd64 ./cmd/kumiho-gui-web

for screen in dashboard locations settings; do
  echo "capturing the $screen screen"
  "$repo/scripts/dev-gui-web.sh" start >/dev/null
  node "$repo/scripts/visual/capture.mjs" --only "$screen"
done

cp "$captures/dashboard/dark-connected.png" "$repo/screenshots/windows-gui/dashboard.png"
cp "$captures/locations/dark-speed-test.png" "$repo/screenshots/windows-gui/locations.png"
cp "$captures/settings/dark.png" "$repo/screenshots/windows-gui/settings.png"

echo "capturing the TUI views"
KUMIHO_TUI_CAPTURE_DIR="$tui_tmp" go test ./internal/tui -run TestCaptureViews -count=1
node "$repo/scripts/screenshots/render-tui.mjs" "$tui_tmp" "$repo/screenshots/linux-tui"

for file in \
  "$repo/screenshots/windows-gui/dashboard.png" \
  "$repo/screenshots/windows-gui/locations.png" \
  "$repo/screenshots/windows-gui/settings.png" \
  "$repo/screenshots/linux-tui/dashboard.png" \
  "$repo/screenshots/linux-tui/locations.png" \
  "$repo/screenshots/linux-tui/settings.png"; do
  [[ -s "$file" ]] || { echo "missing capture: $file" >&2; exit 1; }
  printf '  %s (%s bytes)\n' "${file#"$repo/"}" "$(stat -c %s "$file")"
done
