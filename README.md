# Kumiho

[![Latest release](https://img.shields.io/github/v/release/benyamin-git/Kumiho?include_prereleases&label=release&style=flat-square)](https://github.com/benyamin-git/Kumiho/releases)
[![Downloads](https://img.shields.io/github/downloads/benyamin-git/Kumiho/total?style=flat-square)](https://github.com/benyamin-git/Kumiho/releases)
[![License](https://img.shields.io/github/license/benyamin-git/Kumiho?style=flat-square)](LICENSE)

Kumiho is an unofficial desktop and mobile client for the free
Firefox-account VPN. Use your own Firefox account on **Linux**, **Windows 11**,
**Android phones** or **Android TV** — with a native app on every platform.

It is a real system tunnel, not a browser extension: a Linux TUN device or a
Windows Wintun adapter carries all traffic through Mozilla's Fastly edge over
HTTP/2 CONNECT. Android embeds the same Go engine in a `VpnService` app.
Kumiho signs in with Firefox Accounts (password or browser, 2FA aware), finds
the nearest server, and keeps the tunnel alive with an optional kill switch.

**Not affiliated with or endorsed by Mozilla.** See the
[disclaimer](#disclaimer).

## Download

| Platform | Download |
| --- | --- |
| Windows 11 (64-bit) | [![Windows x64 ZIP](https://img.shields.io/badge/ZIP-x64-0550ae?logo=windows&logoColor=white)](https://github.com/benyamin-git/Kumiho/releases/latest/download/Kumiho-Windows-amd64.zip) |
| Linux (64-bit / ARM64) | [![Linux x64 ZIP](https://img.shields.io/badge/ZIP-x64-e36209?logo=linux&logoColor=white)](https://github.com/benyamin-git/Kumiho/releases/latest/download/Kumiho-Linux-amd64.zip) [![Linux ARM64 ZIP](https://img.shields.io/badge/ZIP-ARM64-f0883e?logo=linux&logoColor=white)](https://github.com/benyamin-git/Kumiho/releases/latest/download/Kumiho-Linux-arm64.zip) |
| Android phone and TV | [![Android ARMv8 APK](https://img.shields.io/badge/APK-ARMv8-2da44e?logo=android&logoColor=white)](https://github.com/benyamin-git/Kumiho/releases/latest/download/Kumiho-Android-arm64-v8a.apk) [![Android ARMv7 APK](https://img.shields.io/badge/APK-ARMv7-4ac26b?logo=android&logoColor=white)](https://github.com/benyamin-git/Kumiho/releases/latest/download/Kumiho-Android-armeabi-v7a.apk) [![Android universal APK](https://img.shields.io/badge/APK-Universal-1a7f37?logo=android&logoColor=white)](https://github.com/benyamin-git/Kumiho/releases/latest/download/Kumiho-Android-universal.apk) |

Every file is listed in
[SHA256SUMS](https://github.com/benyamin-git/Kumiho/releases/latest/download/SHA256SUMS).
Android is sideload-only: there is no Play Store or F-Droid build. These links
follow the latest full release; v0.7.2 and earlier prereleases remain on the
[releases page](https://github.com/benyamin-git/Kumiho/releases).

## Screenshots

Captured in the Dark theme with sample data.

### Windows GUI

| Dashboard | Locations |
| --- | --- |
| ![Windows GUI dashboard: connected tunnel, quota and session totals](screenshots/windows-gui/dashboard.png) | ![Windows GUI locations: pinned Fastest, Favorites and Recent sections above flagged server rows with search and real-edge ping times](screenshots/windows-gui/locations.png) |

![Windows GUI settings: the Region relay controls and the background service rows](screenshots/windows-gui/settings.png)

### Linux TUI

| Dashboard | Locations |
| --- | --- |
| ![Linux TUI dashboard: connected state, quota bar and session totals](screenshots/linux-tui/dashboard.png) | ![Linux TUI locations: Fastest, Favorites and Recent sections above flagged rows, a locked badge and ping times](screenshots/linux-tui/locations.png) |

![Linux TUI settings: the Region relay rows with their region-only hint](screenshots/linux-tui/settings.png)

### Android phone

| Dashboard | Locations | Settings |
| --- | --- | --- |
| <img src="screenshots/android-phone/dashboard.png" width="260" alt="Android dashboard: connected tunnel with quota and session totals"> | <img src="screenshots/android-phone/locations.png" width="260" alt="Android locations: search, Ping all and server list"> | <img src="screenshots/android-phone/settings.png" width="260" alt="Android settings: app and system cards"> |

### Android TV

| Home | Locations | Settings |
| --- | --- | --- |
| <img src="screenshots/android-tv/home.png" width="420" alt="Android TV home: ten-foot dashboard with remote focus"> | <img src="screenshots/android-tv/locations.png" width="420" alt="Android TV locations: remote-first server list"> | <img src="screenshots/android-tv/settings.png" width="420" alt="Android TV settings: card-based sections"> |

## What's new in v0.10.3

- **A challenged password sign-in continues in the browser** — when Mozilla's
  edge rejects the native password API with an anti-bot challenge, Kumiho
  starts the existing browser flow for the same account instead of stopping on
  an error. Normal wrong-password errors stay on the form.

v0.10.2 fixed access-token refresh through Mozilla's OAuth form endpoint; see
the [changelog](CHANGELOG.md) for details.

## Install

### Linux

One command on Ubuntu 24.04 amd64 or arm64. It downloads the newest published
release, verifies the release `SHA256SUMS`, and runs the bundled installer:

    curl -fsSL https://raw.githubusercontent.com/benyamin-git/Kumiho/main/scripts/install-release.sh | sudo bash

Then sign in and start the TUI:

    kumiho login
    kumiho

To install from a downloaded archive instead, extract it and run:

    sudo ./install.sh

The installer adds the daemon, the systemd unit and the CLI/TUI. Kumiho runs
as a system service, so the tunnel survives SSH logouts. See the
[Linux install guide](docs/install.md) for a specific version, upgrade and
uninstall.

### Windows 11

1. Download `Kumiho-Windows-amd64.zip` and extract it.
2. In **Administrator PowerShell**, run `.\install.ps1` (or double-click
   `install.cmd`).
3. Start **Kumiho** from the Start Menu.

The installer adds the CLI, the Windows GUI and `wintun.dll`, installs the
service and opens the app at login. The app needs the WebView2 Runtime, which
ships with Windows 11.

See the [Windows install guide](docs/install.md#windows-11) and the
[GUI guide](docs/gui.md).

### Android

Download the APK for your device and open it. Allow installation from your
file manager or browser when Android asks; Kumiho is sideload-only.

- **Most phones and Android TV:** `Kumiho-Android-arm64-v8a.apk`
- **Older 32-bit phones:** `Kumiho-Android-armeabi-v7a.apk`
- **Not sure:** `Kumiho-Android-universal.apk`

The first launch walks through Firefox Accounts sign-in, VPN permission and
battery/Always-on guidance. On Android TV the same APK opens the TV shell. See
the [Android guide](docs/android.md) and [Android TV guide](docs/android-tv.md).

## Features

- **Your Firefox account** — password or browser sign-in, 2FA, up to five
  accounts with labels, quota rotation and a session-wide sign-out.
- **Mozilla's edge challenge handled** — solves the Fastly challenge and
  retries a blocked auth host; if the challenge still blocks the password
  API, the same sign-in continues in the browser instead of dead-ending.
- **Blocked-region control plane** — HTTP 451 is reported honestly, with one
  optional retry through a user-operated relay, a Worker template, and a Test
  relay action. The relay never carries VPN traffic or the browser sign-in
  page. See [the relay guide](docs/relay.md).
- **Real system tunnel** — TUN on Linux, Wintun on Windows and `VpnService` on
  Android; optional kill switch, split tunneling on Android, and a local SOCKS5
  proxy on every platform.
- **Locations that make sense** — one flat server list with real country flags,
  search, real-edge Ping all, Fastest, favorites and recents; locked rows stay
  hidden until you reveal them.
- **Strict DNS** — DNS over HTTPS inside the tunnel, with Cloudflare and Quad9
  failover; no plaintext fallback.
- **Desktop comfort** — systemd or Windows service, a Bubble Tea TUI and CLI,
  and a native Windows GUI with tray, logs and diagnostics.
- **Android everywhere** — phone and TV shells, home-screen widgets, Quick
  Settings tile, status notification, logs sharing and diagnostics.

## Documentation

- [Simple manual](docs/manual.md) — the one-screen version.
- [CLI and TUI](docs/usage.md) — commands, keys and every setting.
- [Control-plane relay](docs/relay.md) — deploy the Worker, set the token,
  test it, and understand its limits.
- [Linux install](docs/install.md) · [Windows GUI](docs/gui.md) ·
  [Android](docs/android.md) · [Android TV](docs/android-tv.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Development](docs/development.md) — build, test, release and architecture.
- [Full design and protocol](PLAN.md) · [Changelog](CHANGELOG.md)

## Known limitations

- Non-DNS UDP (QUIC/HTTP3, VoIP, games) is intentionally blocked inside the
  tunnel; applications fall back to TCP.
- The password flow cannot solve an interactive captcha. Use browser sign-in
  when Mozilla asks for one.
- The Windows binary is unsigned, so SmartScreen warns on first run.
- Android captures and blackholes IPv6 instead of tunneling it, and there is
  no in-app APK installer or auto-update.
- Using an unofficial client is a ToS gray area. Account rotation is opt-in and
  off by default.

## Disclaimer

Kumiho is unofficial and is not affiliated with or endorsed by Mozilla. It uses
your own Firefox account entitlement. Review the code before trusting it with
your credentials. Kumiho never sees or stores your Firefox password; it stores
the resulting OAuth tokens locally.

## Attribution

See [NOTICE](NOTICE). Protocol behavior reference: FoxyVPN (MIT).
