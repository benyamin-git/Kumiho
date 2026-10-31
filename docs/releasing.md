# Release checklist

Run this before tagging `v*`. The tag pushes `.github/workflows/release.yml`,
which publishes a full release with stable asset names from v0.8.0 onward.

## Before tagging

- [ ] Move the changelog entries out of **Unreleased** and date the new
  `CHANGELOG.md` section.
- [ ] Update the fallback version in `android/app/build.gradle.kts` for the
  new tag.
- [ ] Check the README **What's new** section names the latest published
  release, not an unreleased one.
- [ ] Regenerate the screenshots and confirm the README paths exist.
- [ ] Review the repository **About** panel: description, website and topics
  still describe the current capabilities (Android and Android TV included).
- [ ] Run the full Go gate. For changes under `android/`, `mobile/` or
  `internal/platform/android/`, the `android` and `android-tv` CI jobs are
  the Android gate in a T3 release thread: do **not** run Gradle, the
  emulator or a local connected suite there. Locally, outside T3,
  `make android-test` is still the documented Android check.
- [ ] Confirm the five `ANDROID_*` signing secrets exist:
  `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`,
  `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD` and `ANDROID_CERT_SHA256`. Stop
  before tagging if any is missing; the release workflow cannot publish signed
  APKs without them.
- [ ] Confirm no relay token, redirect URL, keystore or generated release
  material is staged or tracked.

## Tag and publish

- [ ] `git tag vX.Y.Z && git push origin vX.Y.Z`
- [ ] Watch the release workflow: signed APKs, stable-named assets,
  `SHA256SUMS`, formatted release notes and the latest-release badge.
- [ ] Verify `/releases/latest/download/<asset>` links resolve after publishing.
- [ ] Update the README **What's new** section (and remove any temporary
  download note) to the release that was just published.
- [ ] If the app gained a new platform or major capability, update the About
  description and topics again after the release.

The workflow publishes full releases so `/releases/latest` works. Do not add
`--prerelease` unless you deliberately want to break the stable download links
until the next full release.
