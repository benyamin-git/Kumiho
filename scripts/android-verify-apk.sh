#!/usr/bin/env bash
#
# Verify a Kumiho Android APK before it can be called a release artifact.
#
# Checks: package/version/sdk badging, expected ABI contents (no x86), 16 KB
# zipalign, arm64 ELF p_align, signature (unless explicitly unsigned), and the
# universal size cap. The same script runs locally and in CI.
set -euo pipefail

usage() {
	cat <<'EOF'
usage: scripts/android-verify-apk.sh <apk> [<apk> ...]

Checks the APK's package, version, SDK levels, ABI contents, 16 KB alignment,
arm64 ELF p_align, signature and size. Exits non-zero on the first failure.

Environment:
  KUMIHO_EXPECT_PACKAGE        expected package (default dev.benyamin.kumiho)
  KUMIHO_EXPECT_VERSION        optional expected versionName (without v)
  KUMIHO_EXPECT_VERSION_CODE   optional expected versionCode
  KUMIHO_EXPECT_MIN_SDK        default 26
  KUMIHO_EXPECT_TARGET_SDK     default 36
  KUMIHO_ABI                   arm64-v8a|armeabi-v7a|universal; default inferred
  KUMIHO_ALLOW_UNSIGNED=1      skip apksigner (CI assembleRelease check)
  KUMIHO_MAX_UNIVERSAL_BYTES   universal cap (default 62914560, 60 MiB)
  KUMIHO_BUILD_TOOLS           build-tools version (default 36.0.0)

Examples:
  scripts/android-verify-apk.sh dist/kumiho-v0.5.0-android-universal.apk
  KUMIHO_ALLOW_UNSIGNED=1 scripts/android-verify-apk.sh \
    android/app/build/outputs/apk/release/app-universal-release-unsigned.apk
EOF
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ $# -eq 0 ]; then
	usage >&2
	exit 2
fi

case "${1:-}" in
-h | --help)
	usage
	exit 0
	;;
esac

# shellcheck source=./android-env.sh
. "$repo_root/scripts/android-env.sh" >/dev/null

find_build_tools() {
	local version="${KUMIHO_BUILD_TOOLS:-36.0.0}"
	if [ -x "$ANDROID_HOME/build-tools/$version/aapt2" ]; then
		printf '%s\n' "$ANDROID_HOME/build-tools/$version"
		return 0
	fi
	local latest
	latest=$(find "$ANDROID_HOME/build-tools" -mindepth 1 -maxdepth 1 -type d 2>/dev/null |
		sort -V | tail -n 1 || true)
	[ -n "$latest" ] && [ -x "$latest/aapt2" ] && [ -x "$latest/zipalign" ] && [ -x "$latest/apksigner" ] && {
		printf '%s\n' "$latest"
		return 0
	}
	return 1
}

bt="$(find_build_tools)" || {
	echo "android-verify-apk: build-tools not found under $ANDROID_HOME (run scripts/android-setup.sh)" >&2
	exit 1
}
aapt2="$bt/aapt2"
zipalign="$bt/zipalign"
apksigner="$bt/apksigner"
expect_pkg="${KUMIHO_EXPECT_PACKAGE:-dev.benyamin.kumiho}"
expect_version="${KUMIHO_EXPECT_VERSION:-}"
expect_code="${KUMIHO_EXPECT_VERSION_CODE:-}"
expect_min="${KUMIHO_EXPECT_MIN_SDK:-26}"
expect_target="${KUMIHO_EXPECT_TARGET_SDK:-36}"
max_universal="${KUMIHO_MAX_UNIVERSAL_BYTES:-62914560}"
allow_unsigned="${KUMIHO_ALLOW_UNSIGNED:-0}"

readelf_bin=""
if [ -n "${ANDROID_NDK_HOME:-}" ] && [ -d "$ANDROID_NDK_HOME" ]; then
	readelf_bin="$(find "$ANDROID_NDK_HOME/toolchains/llvm/prebuilt" -maxdepth 5 \
		\( -type f -o -type l \) -name llvm-readelf 2>/dev/null | head -n 1 || true)"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "android-verify-apk: FAIL $*" >&2
	exit 1
}

infer_abi() {
	local apk=$1
	if [ -n "${KUMIHO_ABI:-}" ]; then
		printf '%s\n' "$KUMIHO_ABI"
		return 0
	fi
	case "$(basename "$apk")" in
	*arm64-v8a*) printf '%s\n' arm64-v8a ;;
	*armeabi-v7a*) printf '%s\n' armeabi-v7a ;;
	*universal* | *debug*) printf '%s\n' universal ;;
	*) return 1 ;;
	esac
}

check_one() {
	local apk=$1
	[ -f "$apk" ] || fail "not an APK: $apk"
	[ -s "$apk" ] || fail "empty APK: $apk"
	unzip -tqq "$apk" >/dev/null || fail "not an APK (invalid zip): $apk"

	local badging pkg version code min target
	badging="$("$aapt2" dump badging "$apk" 2>&1)" || fail "aapt2 could not read $apk"
	pkg=$(printf '%s\n' "$badging" | sed -n "s/^package: name='\([^']*\)'.*/\1/p" | head -n 1)
	code=$(printf '%s\n' "$badging" | sed -n "s/^package: name='[^']*' versionCode='\([^']*\)'.*/\1/p" | head -n 1)
	version=$(printf '%s\n' "$badging" | sed -n "s/^package: name='[^']*' versionCode='[^']*' versionName='\([^']*\)'.*/\1/p" | head -n 1)
	min=$(printf '%s\n' "$badging" | sed -n "s/^minSdkVersion:'\([^']*\)'.*/\1/p" | head -n 1)
	# aapt (not aapt2) calls the same field sdkVersion; accept both.
	if [ -z "$min" ]; then
		min=$(printf '%s\n' "$badging" | sed -n "s/^sdkVersion:'\([^']*\)'.*/\1/p" | head -n 1)
	fi
	target=$(printf '%s\n' "$badging" | sed -n "s/^targetSdkVersion:'\([^']*\)'.*/\1/p" | head -n 1)

	[ -n "$pkg" ] || fail "no package in $apk"
	[ "$pkg" = "$expect_pkg" ] || fail "$apk package=$pkg, want $expect_pkg"
	if [ -n "$expect_version" ] && [ "$version" != "$expect_version" ]; then
		fail "$apk versionName=$version, want $expect_version"
	fi
	if [ -n "$expect_code" ] && [ "$code" != "$expect_code" ]; then
		fail "$apk versionCode=$code, want $expect_code"
	fi
	[ "$min" = "$expect_min" ] || fail "$apk minSdk=$min, want $expect_min"
	[ "$target" = "$expect_target" ] || fail "$apk targetSdk=$target, want $expect_target"

	local abi
	abi="$(infer_abi "$apk")" || fail "$apk: cannot infer ABI; set KUMIHO_ABI"
	case "$abi" in
	arm64-v8a | armeabi-v7a | universal) ;;
	*) fail "$apk: unsupported KUMIHO_ABI=$abi" ;;
	esac

	local libs native_libs
	libs=$(unzip -Z1 "$apk" | grep -E '^lib/(arm64-v8a|armeabi-v7a|x86|x86_64)/libgojni\.so$' || true)
	native_libs=$(unzip -Z1 "$apk" | grep -E '^lib/[^/]+/.*\.so$' || true)
	if printf '%s\n' "$native_libs" | grep -Eq '^lib/(x86|x86_64)/'; then
		fail "$apk contains x86/x86_64 native libraries"
	fi
	case "$abi" in
	arm64-v8a)
		[ "$libs" = "lib/arm64-v8a/libgojni.so" ] || fail "$apk arm64 libs: ${libs:-none}"
		;;
	armeabi-v7a)
		[ "$libs" = "lib/armeabi-v7a/libgojni.so" ] || fail "$apk armeabi-v7a libs: ${libs:-none}"
		;;
	universal)
		printf '%s\n' "$libs" | grep -qx 'lib/arm64-v8a/libgojni.so' || fail "$apk missing arm64 lib"
		printf '%s\n' "$libs" | grep -qx 'lib/armeabi-v7a/libgojni.so' || fail "$apk missing armeabi-v7a lib"
		if printf '%s\n' "$libs" | grep -Eq '^lib/(x86|x86_64)/'; then
			fail "$apk contains an x86 library"
		fi
		;;
	esac

	"$zipalign" -c -P 16 -v 4 "$apk" >/dev/null || fail "$apk failed zipalign -P 16"

	local aligns="" align align_display
	if [ "$abi" = "arm64-v8a" ] || [ "$abi" = "universal" ]; then
		[ -n "$readelf_bin" ] || fail "llvm-readelf not found under $ANDROID_NDK_HOME"
		unzip -p "$apk" lib/arm64-v8a/libgojni.so >"$tmp/libgojni.so" ||
			fail "$apk could not extract arm64 libgojni.so"
		aligns="$("$readelf_bin" -lW "$tmp/libgojni.so" | awk '$1 == "LOAD" { print $NF }')"
		[ -n "$aligns" ] || fail "$apk arm64 lib has no LOAD segments"
		for align in $aligns; do
			[ $((align)) -ge $((0x4000)) ] || fail "$apk arm64 LOAD p_align $align < 0x4000"
		done
	fi
	align_display=$(printf '%s\n' "$aligns" | tr '\n' ' ' | sed 's/[[:space:]]*$//')

	local sig="skipped (unsigned allowed)"
	if [ "$allow_unsigned" != "1" ]; then
		local certs
		certs="$("$apksigner" verify --print-certs "$apk" 2>&1)" || fail "$apk is not signed"
		sig=$(printf '%s\n' "$certs" | sed -n 's/^Signer #1 certificate SHA-256 digest: //p' | head -n 1)
		[ -n "$sig" ] || sig="verified"
	fi

	local bytes mib
	bytes=$(stat -c '%s' "$apk")
	if [ "$abi" = "universal" ] && [ "$bytes" -gt "$max_universal" ]; then
		fail "$apk size $bytes exceeds universal cap $max_universal"
	fi
	mib=$(awk -v b="$bytes" 'BEGIN { printf "%.2f", b / 1048576 }')

	echo "android-verify-apk: OK $apk"
	echo "  package=$pkg versionName=$version versionCode=$code minSdk=$min targetSdk=$target"
	echo "  abi=$abi libs=$(printf '%s' "$libs" | tr '\n' ' ' | sed 's/ $//')"
	echo "  zipalign=ok p_align=${align_display:-n/a} signature=$sig size=${mib}MiB"
}

for apk in "$@"; do
	check_one "$apk"
done
