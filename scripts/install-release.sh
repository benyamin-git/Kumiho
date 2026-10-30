#!/usr/bin/env bash
# One-command Linux installer: download the newest published Kumiho release,
# verify it against the release SHA256SUMS, extract it and run its install.sh.
#
#   curl -fsSL https://raw.githubusercontent.com/benyamin-git/Kumiho/main/scripts/install-release.sh | sudo bash
#   curl -fsSL .../scripts/install-release.sh | sudo bash -s -- --version v0.7.2
#
# The release archive carries install.sh, the systemd unit and the polkit rule,
# so the downloaded copy is the only file this script needs.
set -euo pipefail

repo="benyamin-git/Kumiho"
api="https://api.github.com/repos/$repo/releases?per_page=20"
version=""
forward=()

usage() {
	cat <<'EOF'
usage: install-release.sh [--version vX.Y.Z] [install.sh arguments]

Downloads the newest published Kumiho Linux release (prereleases included),
verifies SHA256SUMS and runs its install.sh as root.

Options:
  -v, --version vX.Y.Z   install a specific tag instead of the newest release
  -h, --help             show this help

Any other arguments are passed to the bundled install.sh, for example
`uninstall` or `uninstall --purge`.

Typical use:
  curl -fsSL https://raw.githubusercontent.com/benyamin-git/Kumiho/main/scripts/install-release.sh | sudo bash
EOF
}

die() {
	echo "install-release: $*" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
	-v | --version)
		shift
		[ $# -gt 0 ] || die "--version needs a tag"
		version="$1"
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		forward+=("$1")
		;;
	esac
	shift
done

[ "$(id -u)" -eq 0 ] || die "must run as root (try: curl ... | sudo bash)"

for tool in curl unzip sha256sum; do
	command -v "$tool" >/dev/null 2>&1 || die "$tool is required (apt install $tool)"
done

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported architecture: $(uname -m)" ;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if [ -z "$version" ]; then
	curl -fsSL "$api" -o "$tmp/releases.json" ||
		die "could not query GitHub releases; pass --version vX.Y.Z"
	version="$(grep -m1 '"tag_name"' "$tmp/releases.json" |
		sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')"
	[ -n "$version" ] || die "could not resolve the newest release; pass --version vX.Y.Z"
fi
version="${version#v}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "invalid version: $version"

tag="v$version"
stable_asset="Kumiho-Linux-$arch.zip"
legacy_asset="kumiho-$tag-linux-$arch.zip"
base="https://github.com/$repo/releases/download/$tag"

# Releases from v0.8.0 use stable, version-free asset names. Older releases
# keep the versioned names, so try the stable name first and fall back.
asset="$stable_asset"
echo "install-release: downloading $asset"
if ! curl -fsSL "$base/$asset" -o "$tmp/$asset"; then
	rm -f "$tmp/$asset"
	asset="$legacy_asset"
	echo "install-release: downloading $asset"
	curl -fsSL "$base/$asset" -o "$tmp/$asset"
fi
curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS"

expected="$(awk -v name="$asset" '{ n = $2; sub(/^\*/, "", n); if (n == name) { print $1; exit } }' "$tmp/SHA256SUMS")"
[ -n "$expected" ] || die "SHA256SUMS has no entry for $asset"
actual="$(sha256sum "$tmp/$asset" | awk '{ print $1 }')"
[ "$expected" = "$actual" ] || die "checksum mismatch for $asset"
echo "install-release: checksum verified"

unzip -q "$tmp/$asset" -d "$tmp/pkg"
installer="$tmp/pkg/install.sh"
[ -f "$installer" ] || die "release archive has no install.sh"

echo "install-release: running the bundled installer"
bash "$installer" "${forward[@]+"${forward[@]}"}"
