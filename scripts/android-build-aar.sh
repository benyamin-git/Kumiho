#!/usr/bin/env bash
#
# Builds the gomobile AAR the Android app links against (plan T6).
#
# The AAR is a build output: it lands in android/app/libs/ (git-ignored) and is
# never committed. Gradle never runs Go; this script does.
#
#   scripts/android-build-aar.sh             # android/arm + android/arm64 -> kumiho.aar
#   scripts/android-build-aar.sh --emulator  # android/amd64 (x86_64)      -> kumiho-emulator.aar
#
# The release AAR is arm-only (C3); the emulator AAR exists so the Compose app
# and the AAR probe can run on the x86_64 CI emulator. Version metadata mirrors
# the Makefile: VERSION / COMMIT / DATE, overridable from the environment.
#
# After the bind the script verifies the archive layout (classes.jar plus one
# jni/<abi>/libgojni.so per target) and that the arm64 shared object is aligned
# for 16 KB pages (p_align >= 0x4000), which Android 15+ devices require.
set -euo pipefail

usage() {
	cat <<'EOF'
usage: scripts/android-build-aar.sh [--emulator]

  (no flag)    build android/arm + android/arm64 -> android/app/libs/kumiho.aar
  --emulator   build android/amd64               -> android/app/libs/kumiho-emulator.aar
  -h, --help   show this help

Environment overrides: ANDROID_HOME, ANDROID_NDK_HOME, JAVA_HOME (see
scripts/android-env.sh), VERSION / COMMIT / DATE (see the Makefile).
EOF
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

targets="android/arm,android/arm64"
out="$repo_root/android/app/libs/kumiho.aar"
emulator=0

while [ $# -gt 0 ]; do
	case "$1" in
	--emulator)
		emulator=1
		targets="android/amd64"
		out="$repo_root/android/app/libs/kumiho-emulator.aar"
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "android-build-aar: unknown argument '$1'" >&2
		usage >&2
		exit 2
		;;
	esac
	shift
done

# shellcheck source=./android-env.sh
. "$repo_root/scripts/android-env.sh"

for tool in gomobile go unzip; do
	command -v "$tool" >/dev/null 2>&1 || {
		echo "android-build-aar: $tool not found on PATH (run scripts/android-setup.sh)" >&2
		exit 1
	}
done
[ -n "${ANDROID_NDK_HOME:-}" ] && [ -d "$ANDROID_NDK_HOME" ] || {
	echo "android-build-aar: ANDROID_NDK_HOME is not a directory; run scripts/android-setup.sh" >&2
	exit 1
}

# Same version injection as `make build`.
VERSION="${VERSION:-$(git -C "$repo_root" describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="${COMMIT:-$(git -C "$repo_root" rev-parse --short HEAD 2>/dev/null || echo unknown)}"
DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
module="github.com/benyamin-git/kumiho"
ldflags="-s -w -X ${module}/internal/version.Version=${VERSION} -X ${module}/internal/version.Commit=${COMMIT} -X ${module}/internal/version.Date=${DATE}"

mkdir -p "$(dirname "$out")"
rm -f "$out"

# gomobile resolves the module graph with `go list -m -json all` in this
# directory. Vendor mode cannot answer that query ("can't compute 'all' using
# the vendor directory"), so the bind runs with -mod=readonly: it reads
# go.mod/go.sum instead of the vendor tree and never writes either file. The
# vendored repo build and the Go gate are unaffected; gomobile's own generated
# module is built outside the repository with its own go.mod.
export GOFLAGS="${GOFLAGS:+$GOFLAGS }-mod=readonly"

echo "android-build-aar: bind $targets -> $out"
echo "android-build-aar: version $VERSION ($COMMIT, $DATE)"

set -x
gomobile bind \
	-target "$targets" \
	-androidapi 26 \
	-javapkg=dev.benyamin.kumiho.go \
	-trimpath \
	-ldflags "$ldflags" \
	-o "$out" \
	"${module}/mobile"
set +x

[ -f "$out" ] || {
	echo "android-build-aar: gomobile produced no $out" >&2
	exit 1
}

# --- archive layout ---------------------------------------------------------

listing="$(unzip -l "$out")"
echo "$listing"

fail=0
if ! printf '%s\n' "$listing" | grep -q ' classes.jar$'; then
	echo "android-build-aar: missing classes.jar in $out" >&2
	fail=1
fi

abis=""
if [ "$emulator" -eq 1 ]; then
	abis="x86_64"
else
	abis="armeabi-v7a arm64-v8a"
fi
for abi in $abis; do
	if ! printf '%s\n' "$listing" | grep -q "jni/$abi/libgojni.so\$"; then
		echo "android-build-aar: missing jni/$abi/libgojni.so in $out" >&2
		fail=1
	fi
done
[ "$fail" -eq 0 ] || exit 1

# --- 16 KB page alignment (arm64 only; the emulator AAR is x86_64) ----------

if [ "$emulator" -eq 0 ]; then
	# llvm-readelf is a symlink to llvm-readobj in the NDK, so accept links too.
	readelf_bin="$(find "$ANDROID_NDK_HOME/toolchains/llvm/prebuilt" -maxdepth 5 \
		\( -type f -o -type l \) -name llvm-readelf 2>/dev/null | head -n 1)"
	[ -n "$readelf_bin" ] || {
		echo "android-build-aar: llvm-readelf not found under $ANDROID_NDK_HOME" >&2
		exit 1
	}
	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT
	unzip -q -o -j "$out" jni/arm64-v8a/libgojni.so -d "$tmp"
	so="$tmp/libgojni.so"
	aligns="$("$readelf_bin" -lW "$so" | awk '$1 == "LOAD" { print $NF }')"
	echo "android-build-aar: arm64 LOAD p_align values: $(echo "$aligns" | tr '\n' ' ')"
	for align in $aligns; do
		if [ $((align)) -lt $((0x4000)) ]; then
			echo "android-build-aar: arm64 LOAD p_align $align < 0x4000 (16 KB pages)" >&2
			exit 1
		fi
	done
fi

echo "android-build-aar: OK $out"
