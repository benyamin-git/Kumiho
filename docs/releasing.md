# Release checklist

Run this before tagging `v*`. The tag pushes
[`.github/workflows/release.yml`](../.github/workflows/release.yml), which
builds a **draft** release. Test the draft's Android phone and TV APKs, then
publish it manually. The workflow never publishes a release itself.

From v0.12.0 onward every downloadable primary asset carries the exact release
tag. Stable aliases stay attached for `/releases/latest` compatibility. From
the next release onward, user-facing download links (README, `README.fa.md`,
release notes and handoffs) use the versioned primary names, never the aliases
or an ad-hoc file share. The v0.12.0 README still uses the stable aliases and
is intentionally unchanged; update it with the next release.

## Before tagging

- [ ] Move the changelog entries out of **Unreleased** and date the new
  `CHANGELOG.md` section.
- [ ] Update the fallback version in `android/app/build.gradle.kts` for the
  new tag.
- [ ] Check the README **What's new** section names the latest *published*
  release, not an unreleased one. The new release block is updated after
  publication.
- [ ] Update `README.fa.md` with the same user-facing changes as the English
  README and confirm its `screenshots/fa/` paths exist.
- [ ] Regenerate the English and Persian screenshots. The committed Persian
  subset is the GUI dashboard and settings, the phone dashboard and settings,
  and the TV home screen; the full review matrix stays in
  `/tmp/opencode/fa-captures`.
- [ ] Complete the Persian UI review gate: every changed catalog string and
  every committed screenshot must be checked in the running GUI, phone and TV
  before the tag.
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
- [ ] Confirm the worktree is clean on the release commit and that `main`
  points at it (or at the commit the tag will use).

## Versioned assets and aliases

For tag `vX.Y.Z`, the workflow stages both filename sets from the same build
outputs. The versioned primary is the only user-facing name; the stable alias
is a compatibility convenience for `/releases/latest` and must not be
advertised in the README, release notes or a handoff.

| Versioned primary | Stable alias |
| --- | --- |
| `Kumiho-vX.Y.Z-Linux-amd64.zip` | `Kumiho-Linux-amd64.zip` |
| `Kumiho-vX.Y.Z-Linux-arm64.zip` | `Kumiho-Linux-arm64.zip` |
| `Kumiho-vX.Y.Z-Windows-amd64.zip` | `Kumiho-Windows-amd64.zip` |
| `Kumiho-vX.Y.Z-Android-arm64-v8a.apk` | `Kumiho-Android-arm64-v8a.apk` |
| `Kumiho-vX.Y.Z-Android-armeabi-v7a.apk` | `Kumiho-Android-armeabi-v7a.apk` |
| `Kumiho-vX.Y.Z-Android-universal.apk` | `Kumiho-Android-universal.apk` |
| `SHA256SUMS-vX.Y.Z` | `SHA256SUMS` |

`SHA256SUMS-vX.Y.Z` and `SHA256SUMS` have identical contents. Each file lists
both the versioned primary names and the stable aliases, so a download can be
verified whichever name it used. `scripts/install-release.sh` reads
`SHA256SUMS` and the stable Linux archive; `sha256sum --ignore-missing -c`
accepts a checksum file that lists names not present in the current download
directory.

The upload includes all twelve archives/APKs, both checksum files, and no
other generated material. The release notes download table uses the versioned
primary names and links the versioned checksum; a short note explains the
stable aliases, and the notes link the Persian README. The README and
`README.fa.md` download tables must use the same versioned primary names and
link `SHA256SUMS-vX.Y.Z`.

## Tag and build the draft

- [ ] `git tag vX.Y.Z && git push origin vX.Y.Z`
- [ ] Watch the release workflow: the signed APKs pass the certificate, ABI,
  16 KB-alignment and arm64 `p_align` checks; versioned primaries and stable
  aliases are all attached; both checksum files are identical; the release
  notes use the matching `CHANGELOG.md` section and link the Persian summary.
- [ ] `gh release view vX.Y.Z --json isDraft,isPrerelease,url,assets` confirms
  `isDraft: true`, the tag matches the manifest version, and every expected
  asset is attached.
- [ ] Install the draft's APKs on a phone and an Android TV device and run the
  smoke checks in [android-testing.md](android-testing.md).
- [ ] Leave the draft untouched for the user's review. Publishing is a manual
  decision: either the GitHub UI (**Publish release** with **Set as the latest
  release**) or `gh release edit vX.Y.Z --draft=false --latest`.
- [ ] After publication, verify `/releases/latest` resolves to the new release
  for compatibility, update the README **What's new** block and the Persian
  README to the just-published release, and switch both README download tables
  and their checksum link to the versioned `vX.Y.Z` assets.
- [ ] If the app gained a new platform or major capability, update the About
  description and topics again after the release.

While a release is a draft, `/releases/latest` still points at the previous
published release, so the README download table and the compatibility links
keep working during testing. After publication, switch the README table to the
new versioned assets. Do not add `--prerelease` unless you deliberately want
to break the `/releases/latest` compatibility links until the next full
release.
