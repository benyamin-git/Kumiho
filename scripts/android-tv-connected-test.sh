#!/usr/bin/env bash
# Run the Android TV connected test suite against a running TV emulator or
# device. Mirrors android-connected-test.sh, but filters instrumentation to the
# dev.benyamin.kumiho.tv package so phone tests stay in the phone job.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# shellcheck source=./android-env.sh
. "$repo_root/scripts/android-env.sh" >/dev/null
cd "$repo_root/android"

# GRADLE_FLAGS is intentionally word-split so callers can pass several flags
# (for example GRADLE_FLAGS='--offline --stacktrace').
# shellcheck disable=SC2086
exec "${GRADLE:-./gradlew}" --no-daemon ${GRADLE_FLAGS:-} \
	-PkumihoEmulator=true \
	-Pandroid.testInstrumentationRunnerArguments.package=dev.benyamin.kumiho.tv \
	:app:connectedDebugAndroidTest
