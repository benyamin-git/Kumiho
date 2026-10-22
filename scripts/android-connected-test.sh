#!/usr/bin/env bash
# Run the Android connected test suite against a running emulator or device.
#
# This is a checked-in wrapper because the GitHub emulator action executes each
# line of its `script` input in a separate shell. Keeping the environment setup
# and Gradle invocation in one script makes the action invocation deterministic.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# shellcheck source=./android-env.sh
. "$repo_root/scripts/android-env.sh" >/dev/null
cd "$repo_root/android"

# GRADLE_FLAGS is intentionally word-split so callers can pass several flags
# (for example GRADLE_FLAGS='--offline --stacktrace').
# shellcheck disable=SC2086
exec "${GRADLE:-./gradlew}" --no-daemon ${GRADLE_FLAGS:-} \
	-PkumihoEmulator=true :app:connectedDebugAndroidTest
