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
- [ ] Run the full Go gate and, for Android changes, `make android-test`.

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
