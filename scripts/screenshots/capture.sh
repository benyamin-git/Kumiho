#!/usr/bin/env bash
# Regenerate the README screenshots: the Windows GUI's real UI assets via the
# Linux dev harness, and the real Linux TUI views via TestCaptureViews.
#
# The GUI harness runs on 0.0.0.0:7407 (the project's registered dashboard
# port) and is restarted before each screen so no notice leaks between
# captures. The script stops it when done.
#
# The GUI binary is rebuilt with the v0.12.0 UI target label (override with
# KUMIHO_CAPTURE_VERSION) so the Settings screenshot shows the release label,
# not the git-describe development label. This does not bump the app version.
#
# Usage:
#   scripts/screenshots/capture.sh                 # English README set
#   scripts/screenshots/capture.sh --lang fa       # full Persian review set
#   scripts/screenshots/capture.sh --lang fa --commit-subset
#
# --lang fa writes the full matrix under /tmp/opencode/fa-captures and never
# touches the English screenshots. --commit-subset copies only the Persian GUI
# dashboard and settings images into screenshots/fa/windows-gui and skips the
# TUI (the TUI stays English). The Persian phone and TV images are captured by
# the Android instrumented tests, not by this script; see README.md.
#
# Prerequisites: Go, Node 22+, a primed Playwright cache (see
# scripts/visual/README.md), the real served GUI SVG flags, and Noto Color
# Emoji so the TUI flags render as flags. The TUI renderer uses npx playwright.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
cd "$repo"

usage() {
  cat <<'EOF'
Usage:
  scripts/screenshots/capture.sh                 # English README set
  scripts/screenshots/capture.sh --lang fa       # full Persian review set
  scripts/screenshots/capture.sh --lang fa --commit-subset

--lang fa writes the full matrix under /tmp/opencode/fa-captures and does not
touch the English screenshots. --commit-subset copies only the Persian GUI
dashboard and settings into screenshots/fa/windows-gui and skips the TUI.
EOF
}

lang=en
commit_subset=0
while (($#)); do
  case "$1" in
    --lang)
      [[ $# -ge 2 ]] || { echo "error: --lang needs en or fa" >&2; exit 2; }
      lang="$2"
      shift 2
      ;;
    --lang=*)
      lang="${1#*=}"
      shift
      ;;
    --commit-subset)
      commit_subset=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

case "$lang" in
  en|fa) ;;
  *) echo "error: --lang must be en or fa, got '$lang'" >&2; exit 2 ;;
esac
if ((commit_subset)) && [[ "$lang" != fa ]]; then
  echo "error: --commit-subset is only for --lang fa" >&2
  exit 2
fi

if [[ -n "${KUMIHO_CAPTURES_DIR:-}" ]]; then
  captures="$KUMIHO_CAPTURES_DIR"
elif [[ "$lang" == fa ]]; then
  captures=/tmp/opencode/fa-captures
else
  captures=/tmp/opencode/captures
fi
tui_tmp="${KUMIHO_TUI_CAPTURE_DIR:-/tmp/opencode/kumiho-tui-capture}"
version="${KUMIHO_CAPTURE_VERSION:-v0.12.0}"
commit="$(git rev-parse --short HEAD)"
date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if command -v fc-list >/dev/null 2>&1 &&
   ! fc-list : family | grep -qi 'Noto Color Emoji'; then
  echo "error: install fonts-noto-color-emoji so the flag captures are real" >&2
  exit 1
fi

export KUMIHO_CAPTURES_DIR="$captures"

if [[ "$lang" == fa ]]; then
  screens=(dashboard settings)
  if ((!commit_subset)); then
    screens=(dashboard locations settings blocked-region)
  fi
  mkdir -p "$captures"
  if ((commit_subset)); then
    mkdir -p "$repo/screenshots/fa/windows-gui"
  fi
else
  screens=(dashboard locations settings blocked-region)
  rm -rf "$tui_tmp"
  mkdir -p "$repo/screenshots/windows-gui" "$repo/screenshots/linux-tui" "$tui_tmp"
fi

cleanup() {
  "$repo/scripts/dev-gui-web.sh" stop >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Stop a harness from a previous run before replacing its binary, so the
# pidfile still matches the executable name and the dev script can stop it.
"$repo/scripts/dev-gui-web.sh" stop >/dev/null 2>&1 || true

echo "building the GUI web harness as $version ($lang)"
go build -trimpath \
  -ldflags "-s -w -X github.com/benyamin-git/kumiho/internal/version.Version=$version -X github.com/benyamin-git/kumiho/internal/version.Commit=$commit -X github.com/benyamin-git/kumiho/internal/version.Date=$date" \
  -o dist/kumiho-gui-web-linux-amd64 ./cmd/kumiho-gui-web

for screen in "${screens[@]}"; do
  echo "capturing the $screen screen ($lang)"
  "$repo/scripts/dev-gui-web.sh" start >/dev/null
  node "$repo/scripts/visual/capture.mjs" --only "$screen" --lang "$lang"
done

if [[ "$lang" == fa ]]; then
  if ((commit_subset)); then
    cp "$captures/fa/dashboard/dark-connected.png" "$repo/screenshots/fa/windows-gui/dashboard.png"
    cp "$captures/fa/settings/dark-settings-region.png" "$repo/screenshots/fa/windows-gui/settings.png"
    files=(
      "$repo/screenshots/fa/windows-gui/dashboard.png"
      "$repo/screenshots/fa/windows-gui/settings.png"
    )
  else
    echo "Persian review captures: $captures/fa"
    files=(
      "$captures/fa/dashboard/dark-connected.png"
      "$captures/fa/locations/dark-speed-order.png"
      "$captures/fa/settings/dark-settings-region.png"
      "$captures/fa/blocked-region/dark-prepared.png"
    )
  fi
else
  cp "$captures/dashboard/dark-connected.png" "$repo/screenshots/windows-gui/dashboard.png"
  cp "$captures/locations/dark-speed-order.png" "$repo/screenshots/windows-gui/locations.png"
  cp "$captures/settings/dark-settings-region.png" "$repo/screenshots/windows-gui/settings.png"
  cp "$captures/blocked-region/dark-prepared.png" "$repo/screenshots/windows-gui/blocked-region.png"

  echo "capturing the TUI views"
  KUMIHO_TUI_CAPTURE_DIR="$tui_tmp" go test ./internal/tui -run TestCaptureViews -count=1
  node "$repo/scripts/screenshots/render-tui.mjs" "$tui_tmp" "$repo/screenshots/linux-tui"

  files=(
    "$repo/screenshots/windows-gui/dashboard.png"
    "$repo/screenshots/windows-gui/locations.png"
    "$repo/screenshots/windows-gui/settings.png"
    "$repo/screenshots/windows-gui/blocked-region.png"
    "$repo/screenshots/linux-tui/dashboard.png"
    "$repo/screenshots/linux-tui/locations.png"
    "$repo/screenshots/linux-tui/settings.png"
    "$repo/screenshots/linux-tui/region.png"
  )
fi

for file in "${files[@]}"; do
  [[ -s "$file" ]] || { echo "missing capture: $file" >&2; exit 1; }
  printf '  %s (%s bytes)\n' "${file#"$repo/"}" "$(stat -c %s "$file")"
done
