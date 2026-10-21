#!/usr/bin/env bash
# Install the Android toolchain for kumiho: JDK 17, SDK 36 + 37.2, NDK 28.2,
# gomobile and the Gradle 9.6.0 bootstrap used by T2 to generate the wrapper.
#
#   bash scripts/android-setup.sh           install anything missing
#   bash scripts/android-setup.sh --check   verify resolved versions only, no installs
#   bash scripts/android-setup.sh --ci      non-interactive: no prompts, no apt
#
# Everything installs under $HOME/Android (downloads in a temp dir); nothing
# lands in the repository. Idempotent: re-running skips installed pieces.
# apt's openjdk-17-jdk-headless is used when passwordless sudo is available;
# otherwise a pinned Temurin JDK 17 is installed under $HOME/Android/jdk-17.
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/android-env.sh
source "$SCRIPT_DIR/android-env.sh"

SDK_PLATFORM="android-36"
# The app compiles against API 37.2 because the approved Compose BOM 2026.09.00,
# Core, Lifecycle and Navigation pins require minCompileSdk 37 (controller
# ruling R9); targetSdk stays 36. android-36 stays installed for the emulator
# system image and the T1 contract.
COMPILE_SDK_PLATFORM="android-37.2"
BUILD_TOOLS="36.0.0"
NDK_VER="28.2.13676358"
GOMOBILE_REF="golang.org/x/mobile/cmd/gomobile@v0.0.0-20261011034528-197770444867"
GOMOBILE_REV="197770444867"
GRADLE_VER="9.6.0"

# Pinned downloads; each SHA-256 is verified before extraction.
CMDTOOLS_URL="https://dl.google.com/android/repository/commandlinetools-linux-13114758_latest.zip"
CMDTOOLS_SHA256="7ec965280a073311c339e571cd5de778b9975026cfcbe79f2b1cdcb1e15317ee"
JDK_URL="https://github.com/adoptium/temurin17-binaries/releases/download/jdk-17.0.20.1%2B1/OpenJDK17U-jdk_x64_linux_hotspot_17.0.20.1_1.tar.gz"
JDK_SHA256="3808d1d15e3ec6bd5b84057fb5d84c33d8a1536a258146bcea2e603fc726e08e"
GRADLE_URL="https://services.gradle.org/distributions/gradle-${GRADLE_VER}-bin.zip"
GRADLE_SHA256="bbaeb2fef8710818cf0e261201dab964c572f92b942812df0c3620d62a529a01"

MODE_CHECK=""
MODE_CI=""
for arg in "$@"; do
	case "$arg" in
	--check) MODE_CHECK=check ;;
	--ci) MODE_CI=ci ;;
	*)
		echo "usage: $0 [--check] [--ci]" >&2
		exit 2
		;;
	esac
done

ANDROID_ROOT="$HOME/Android"
JDK_LOCAL_DIR="$ANDROID_ROOT/jdk-17"
TOOLS_DIR="$ANDROID_ROOT/tools"
GRADLE_DIR="$TOOLS_DIR/gradle-${GRADLE_VER}"
GRADLE_BIN="$GRADLE_DIR/bin/gradle"
REPO_DIR=$(cd "$SCRIPT_DIR/.." && pwd)

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

ok() { echo "[ok] $*"; }
say() { echo "[..] $*"; }
err() { echo "[fail] $*" >&2; }
fail() { err "$*"; exit 1; }

fetch() {
	local url=$1 dest=$2 label=$3
	say "downloading $label"
	curl -fsSL "$url" -o "$dest"
}

verify_sha256() {
	local file=$1 want=$2 label=$3
	local got
	got=$(sha256sum "$file" | awk '{print $1}')
	if [ "$got" != "$want" ]; then
		err "sha256 mismatch for $label"
		err "  want $want"
		err "  got  $got"
		exit 1
	fi
	ok "sha256 verified for $label"
}

# $1 = java binary path; succeeds when it reports major version 17.
java_ok() {
	[ -x "$1" ] || return 1
	"$1" -version 2>&1 | grep -o '"17\.' >/dev/null
}

# $1 = JAVA_HOME candidate; succeeds when it holds a JDK 17.
jdk_ok() {
	[ -n "$1" ] && [ -d "$1" ] && java_ok "$1/bin/java"
}

# Prepend $1/bin to PATH unconditionally (android-env.sh only appends existing
# directories, and pieces are installed below it).
path_prepend() {
	case ":$PATH:" in
	*":$1:"*) ;;
	*) PATH="$1:$PATH" ;;
	esac
}

# Add the jar/java bin of a JAVA_HOME candidate.
path_prepend_jdk() {
	path_prepend "$1/bin"
}

# Use the resolved JAVA_HOME; when it is invalid, fall back to the user-local
# Temurin directory and put it on PATH.
resolve_jdk() {
	if jdk_ok "$JAVA_HOME"; then
		return 0
	fi
	if jdk_ok "$JDK_LOCAL_DIR"; then
		JAVA_HOME="$JDK_LOCAL_DIR"
		path_prepend_jdk "$JAVA_HOME"
		return 0
	fi
	return 1
}

# Install the user-local pinned Temurin JDK under $JDK_LOCAL_DIR.
install_local_jdk() {
	fetch "$JDK_URL" "$TMP_DIR/jdk.tar.gz" "Temurin JDK 17"
	verify_sha256 "$TMP_DIR/jdk.tar.gz" "$JDK_SHA256" "Temurin JDK 17"
	say "extracting to $JDK_LOCAL_DIR"
	local top
	top=$(tar -tzf "$TMP_DIR/jdk.tar.gz" | head -1 || true)
	rm -rf "$JDK_LOCAL_DIR"
	mkdir -p "$ANDROID_ROOT"
	tar -xzf "$TMP_DIR/jdk.tar.gz" -C "$ANDROID_ROOT"
	mv "$ANDROID_ROOT/$(basename "$top")" "$JDK_LOCAL_DIR"
	resolve_jdk || fail "installed JDK does not report 17 at $JDK_LOCAL_DIR"
}

# Install a JDK 17: prefer $JAVA_HOME / $JDK_LOCAL_DIR, then apt's
# openjdk-17-jdk-headless (skipped in --ci and when sudo needs a password),
# then the user-local pinned Temurin JDK.
ensure_jdk() {
	if resolve_jdk; then
		ok "java: $(java -version 2>&1 | head -1) (JAVA_HOME=$JAVA_HOME)"
		return 0
	fi
	if [ -z "$MODE_CI" ] && command -v apt-get >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
		say "installing openjdk-17-jdk-headless via apt"
		sudo -n apt-get update -y
		sudo -n apt-get install -y openjdk-17-jdk-headless
		local cand
		for cand in /usr/lib/jvm/java-17-openjdk-amd64 "$JDK_LOCAL_DIR" /usr/lib/jvm/java-17-*; do
			if jdk_ok "$cand"; then
				JAVA_HOME="$cand"
				path_prepend_jdk "$JAVA_HOME"
				ok "java: $(java -version 2>&1 | head -1) (JAVA_HOME=$JAVA_HOME)"
				return 0
			fi
		done
		err "apt install finished but no JDK 17 found on PATH or /usr/lib/jvm"
		install_local_jdk
	else
		[ -n "$MODE_CI" ] && say "--ci: skipping apt; installing user-local JDK"
		install_local_jdk
	fi
}

# Install the Google command-line tools with sdkmanager on the "latest" path
# required by its bundler layout.
ensure_cmdline_tools() {
	if [ -x "$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" ]; then
		path_prepend "$ANDROID_HOME/cmdline-tools/latest/bin"
		return 0
	fi
	fetch "$CMDTOOLS_URL" "$TMP_DIR/cmdline-tools.zip" "Android command-line tools"
	verify_sha256 "$TMP_DIR/cmdline-tools.zip" "$CMDTOOLS_SHA256" "Android command-line tools"
	say "extracting to $ANDROID_HOME/cmdline-tools/latest"
	mkdir -p "$ANDROID_HOME/cmdline-tools"
	unzip -q -d "$TMP_DIR" "$TMP_DIR/cmdline-tools.zip"
	rm -rf "$ANDROID_HOME/cmdline-tools/latest"
	mv "$TMP_DIR/cmdline-tools" "$ANDROID_HOME/cmdline-tools/latest"
	path_prepend "$ANDROID_HOME/cmdline-tools/latest/bin"
	ok "sdkmanager: $(sdkmanager --version 2>/dev/null | head -1)"
}

# $1 = sdkmanager package id; succeeds when already installed.
pkg_installed() {
	sdkmanager --list_installed 2>/dev/null | grep -F "$1" >/dev/null 2>&1
}

ensure_sdk_packages() {
	local missing=()
	local pkg
	for pkg in platform-tools "platforms;$SDK_PLATFORM" "platforms;$COMPILE_SDK_PLATFORM" "build-tools;$BUILD_TOOLS" "ndk;$NDK_VER" emulator "system-images;$SDK_PLATFORM;google_apis;x86_64"; do
		if pkg_installed "$pkg"; then
			ok "installed: $pkg"
		else
			missing+=("$pkg")
		fi
	done
	[ ${#missing[@]} -eq 0 ] && return 0
	say "accepting licenses"
	# `yes` dies of SIGPIPE once sdkmanager accepts everything; that is fine.
	set +o pipefail
	yes | sdkmanager --licenses >/dev/null
	say "installing ${missing[*]}"
	yes | sdkmanager --install "${missing[@]}" >/dev/null
	set -o pipefail
	for pkg in "${missing[@]}"; do
		ok "installed: $pkg"
	done
}

ensure_gomobile() {
	if ! command -v gomobile >/dev/null 2>&1; then
		say "installing gomobile $GOMOBILE_REF"
		go install "$GOMOBILE_REF"
	fi
	say "running gomobile init (NDK $NDK_VER)"
	gomobile init
	# `gomobile version` probes the enclosing Go module, so it misreports in
	# directories without golang.org/x/mobile; `go version -m` is cwd-proof.
	local gv
	gv=$(go version -m "$(command -v gomobile)" 2>/dev/null | awk '$1=="mod" && $2=="golang.org/x/mobile" {print $3}')
	ok "gomobile: $gv"
}

# Find the gradle wrapper after T2 commits it. The Gradle root is `android/`
# (the wrapper is expected at android/gradlew); also accept a legacy wrapper
# at the repository root.
find_gradlew() {
	local w
	for w in "$REPO_DIR/android/gradlew" "$REPO_DIR/gradlew"; do
		if [ -x "$w" ]; then
			printf '%s\n' "$w"
			return 0
		fi
	done
	return 1
}

# Bootstrap Gradle into $TOOLS_DIR only while the repository has no wrapper;
# T2 generates and commits the wrapper, after which this is a no-op.
ensure_gradle() {
	local wrapper
	if wrapper=$(find_gradlew); then
		ok "gradle wrapper present ($wrapper); bootstrap skipped"
		return 0
	fi
	if [ -x "$GRADLE_BIN" ]; then
		ok "gradle bootstrap present: $GRADLE_BIN"
		return 0
	fi
	fetch "$GRADLE_URL" "$TMP_DIR/gradle.zip" "Gradle $GRADLE_VER"
	verify_sha256 "$TMP_DIR/gradle.zip" "$GRADLE_SHA256" "Gradle $GRADLE_VER"
	mkdir -p "$TOOLS_DIR"
	unzip -q -d "$TOOLS_DIR" "$TMP_DIR/gradle.zip"
	ok "gradle bootstrap installed: $GRADLE_BIN"
}

#------------------------------ check mode ------------------------------
# Verify only: print resolved versions, exit non-zero on any mismatch.
check_mode() {
	local check_fail=0
	report() {
		if [ "$1" = 0 ]; then ok "$2"; else err "$2"; check_fail=1; fi
	}
	local java_bin=${JAVA_HOME:-}/bin/java
	if java_ok "$java_bin"; then
		report 0 "java: $(java -version 2>&1 | head -1) (JAVA_HOME=$JAVA_HOME)"
	else
		report 1 "JDK 17 not found (JAVA_HOME=$JAVA_HOME)"
	fi
	local v wrapper
	local sdkmgr_found=1
	command -v sdkmanager >/dev/null 2>&1 || sdkmgr_found=0
	if [ "$sdkmgr_found" = 1 ]; then
		v=$(sdkmanager --version 2>/dev/null | head -1 || true)
		[ -n "$v" ] || sdkmgr_found=0
	fi
	if [ "$sdkmgr_found" = 1 ]; then
		report 0 "sdkmanager: $v"
		local pkg
		for pkg in platform-tools "platforms;$SDK_PLATFORM" "platforms;$COMPILE_SDK_PLATFORM" "build-tools;$BUILD_TOOLS" "ndk;$NDK_VER" emulator "system-images;$SDK_PLATFORM;google_apis;x86_64"; do
			if pkg_installed "$pkg"; then
				report 0 "sdk package: $pkg"
			else
				report 1 "sdk package missing: $pkg"
			fi
		done
	else
		report 1 "sdkmanager: not found"
		report 1 "sdk packages not checked without sdkmanager"
	fi
	if command -v gomobile >/dev/null 2>&1; then
		v=$(go version -m "$(command -v gomobile)" 2>/dev/null | awk '$1=="mod" && $2=="golang.org/x/mobile" {print $3}')
		if [[ "$v" == *"$GOMOBILE_REV"* && -n "$v" ]]; then
			report 0 "gomobile: $v"
		else
			report 1 "gomobile not at pinned revision $GOMOBILE_REV: $v"
		fi
	else
		report 1 "gomobile: not on PATH"
	fi
	if command -v adb >/dev/null 2>&1; then
		report 0 "adb: $(adb version 2>&1 | head -1)"
	else
		report 1 "adb: not on PATH"
	fi
	wrapper=$(find_gradlew || true)
	if [ -x "$GRADLE_BIN" ]; then
		report 0 "gradle bootstrap: $GRADLE_BIN"
	elif [ -n "$wrapper" ]; then
		report 0 "gradle wrapper: $wrapper"
	else
		report 1 "gradle 9.6.0 bootstrap missing"
	fi
	if [ "$check_fail" = 0 ]; then
		ok "all toolchain components verified"
	else
		err "toolchain verification failed"
	fi
	exit "$check_fail"
}

#----------------------------- main flow -----------------------------
if [ -n "$MODE_CHECK" ]; then
	check_mode
fi

ensure_jdk
ensure_cmdline_tools
ensure_sdk_packages
ensure_gomobile
ensure_gradle

ok "android toolchain ready:"
ok "  JAVA_HOME=$JAVA_HOME"
ok "  ANDROID_HOME=$ANDROID_HOME"
ok "  ANDROID_NDK_HOME=$ANDROID_NDK_HOME"
ok "  gradle bootstrap=$GRADLE_BIN"
