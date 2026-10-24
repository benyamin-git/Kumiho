#!/usr/bin/env bash
#
# Build and verify the signed Kumiho Android release APKs.
#
# Requires:
#   - KUMIHO_VERSION=0.6.0 (the v prefix is accepted)
#   - android/keystore.properties (copy android/keystore.properties.example)
#   - the Android toolchain from scripts/android-setup.sh
#
# Produces in dist/:
#   kumiho-v<version>-android-arm64-v8a.apk
#   kumiho-v<version>-android-armeabi-v7a.apk
#   kumiho-v<version>-android-universal.apk
#
# The script never prints the keystore passwords. It is also used by the
# tagged GitHub release workflow.
set -euo pipefail

usage() {
	cat <<'EOF'
usage: KUMIHO_VERSION=0.6.0 scripts/android-release.sh

Builds the arm/arm64 AAR and signed per-ABI + universal release APKs, verifies
each APK, and leaves them in dist/. Requires android/keystore.properties.

Environment:
  KUMIHO_VERSION                 required, MAJOR.MINOR.PATCH; a leading v is stripped
  KUMIHO_EXPECT_CERT_SHA256      required signing certificate SHA-256
  KUMIHO_ALLOW_DIRTY=1           allow a dirty worktree (local experiments only)
  GRADLE_FLAGS                   optional extra Gradle flags (for example --offline)
EOF
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

case "${1:-}" in
-h | --help)
	usage
	exit 0
	;;
esac

version="${KUMIHO_VERSION:-}"
if [ -z "$version" ]; then
	echo "android-release: KUMIHO_VERSION is required (for example KUMIHO_VERSION=0.6.0)" >&2
	usage >&2
	exit 2
fi
version="${version#v}"
if ! [[ "$version" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
	echo "android-release: KUMIHO_VERSION must be MAJOR.MINOR.PATCH, got '$version'" >&2
	exit 2
fi
minor=${BASH_REMATCH[2]}
patch=${BASH_REMATCH[3]}
if [ $((10#$minor)) -ge 100 ] || [ $((10#$patch)) -ge 100 ]; then
	echo "android-release: minor and patch must be < 100 (versionCode scheme), got '$version'" >&2
	exit 2
fi
version_code=$((10#${BASH_REMATCH[1]} * 10000 + 10#$minor * 100 + 10#$patch))
expect_cert="${KUMIHO_EXPECT_CERT_SHA256:-}"
if [ -z "$expect_cert" ]; then
	echo "android-release: KUMIHO_EXPECT_CERT_SHA256 is required" >&2
	echo "  set it to the SHA-256 of the release certificate; it is public metadata" >&2
	exit 2
fi

props="$repo_root/android/keystore.properties"
if [ ! -f "$props" ]; then
	echo "android-release: missing android/keystore.properties" >&2
	echo "  cp android/keystore.properties.example android/keystore.properties" >&2
	echo "  then fill in the four values; never commit it" >&2
	exit 1
fi
for key in storeFile storePassword keyAlias keyPassword; do
	line=$(grep -E "^${key}=" "$props" | head -n 1 || true)
	value="${line#*=}"
	if [ -z "$line" ] || [ -z "$value" ]; then
		echo "android-release: android/keystore.properties is missing a non-empty $key" >&2
		exit 1
	fi
done
store_file=$(sed -n 's/^storeFile=//p' "$props" | head -n 1)
case "$store_file" in
/*) ;;
*) store_file="$repo_root/android/$store_file" ;;
esac
if [ ! -f "$store_file" ]; then
	echo "android-release: storeFile does not exist: $store_file" >&2
	exit 1
fi

if [ "${KUMIHO_ALLOW_DIRTY:-0}" != "1" ] && ! git diff --quiet --ignore-submodules --; then
	echo "android-release: worktree has unstaged changes; commit them or set KUMIHO_ALLOW_DIRTY=1" >&2
	exit 1
fi
if [ "${KUMIHO_ALLOW_DIRTY:-0}" != "1" ] && ! git diff --cached --quiet --ignore-submodules --; then
	echo "android-release: index has staged changes; commit them or set KUMIHO_ALLOW_DIRTY=1" >&2
	exit 1
fi
if [ "${KUMIHO_ALLOW_DIRTY:-0}" != "1" ] && [ -n "$(git ls-files --others --exclude-standard)" ]; then
	echo "android-release: worktree has untracked files; commit them or set KUMIHO_ALLOW_DIRTY=1" >&2
	exit 1
fi

# shellcheck source=./android-env.sh
. "$repo_root/scripts/android-env.sh"

for tool in go gomobile unzip java; do
	command -v "$tool" >/dev/null 2>&1 || {
		echo "android-release: $tool not found (run scripts/android-setup.sh)" >&2
		exit 1
	}
done
[ -n "${ANDROID_NDK_HOME:-}" ] && [ -d "$ANDROID_NDK_HOME" ] || {
	echo "android-release: ANDROID_NDK_HOME is not a directory (run scripts/android-setup.sh)" >&2
	exit 1
}

echo "android-release: building AAR and signed APKs for v$version (versionCode $version_code)"
VERSION="v$version" \
	COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" \
	DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
	bash "$repo_root/scripts/android-build-aar.sh"

rm -rf "$repo_root/android/app/build/outputs/apk/release"
(
	cd "$repo_root/android"
	# GRADLE_FLAGS is intentionally word-split so callers can pass several
	# flags (for example GRADLE_FLAGS='--offline --stacktrace').
	# shellcheck disable=SC2086
	./gradlew --no-daemon ${GRADLE_FLAGS:-} -PkumihoVersion="$version" :app:assembleRelease
)

out="$repo_root/android/app/build/outputs/apk/release"
mkdir -p "$repo_root/dist"
apks=()
copy_apk() {
	local src=$1 dest=$2 abi=$3
	[ -f "$src" ] || {
		echo "android-release: expected Gradle output missing: $src" >&2
		exit 1
	}
	cp -f "$src" "$dest"
	apks+=("$dest:$abi")
	echo "android-release: packaged $(basename "$dest")"
}

copy_apk "$out/app-arm64-v8a-release.apk" \
	"$repo_root/dist/kumiho-v$version-android-arm64-v8a.apk" arm64-v8a
copy_apk "$out/app-armeabi-v7a-release.apk" \
	"$repo_root/dist/kumiho-v$version-android-armeabi-v7a.apk" armeabi-v7a
copy_apk "$out/app-universal-release.apk" \
	"$repo_root/dist/kumiho-v$version-android-universal.apk" universal

for entry in "${apks[@]}"; do
	apk="${entry%:*}"
	abi="${entry##*:}"
	KUMIHO_EXPECT_PACKAGE=dev.benyamin.kumiho \
		KUMIHO_EXPECT_VERSION="$version" \
		KUMIHO_EXPECT_VERSION_CODE="$version_code" \
		KUMIHO_EXPECT_MIN_SDK=26 \
		KUMIHO_EXPECT_TARGET_SDK=36 \
		KUMIHO_EXPECT_CERT_SHA256="$expect_cert" \
		KUMIHO_ALLOW_UNSIGNED=0 \
		KUMIHO_MAX_UNIVERSAL_BYTES=62914560 \
		KUMIHO_ABI="$abi" \
		bash "$repo_root/scripts/android-verify-apk.sh" "$apk"
done

echo "android-release: SHA-256"
for entry in "${apks[@]}"; do
	apk="${entry%:*}"
	sha256sum "$apk"
done

echo "android-release: OK v$version"
