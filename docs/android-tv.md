# Android TV

Kumiho ships one APK for phones and televisions. On a TV it uses the same
engine, `VpnService`, account store, split tunneling and diagnostics as the
phone app, with a ten-foot Compose for TV shell: a left rail, a large focus
ring and remote-only controls.

## Requirements

- Android TV / Google TV on Android 8.0 (API 26) or newer.
- An `arm64-v8a` or `armeabi-v7a` device with the standard Android TV
  launcher. The APK is sideloaded; there is no Play or F-Droid listing.
- A Firefox Account with an active Mozilla VPN entitlement.
- A remote with a D-pad and a select button. No touchscreen is required.

## Install

Build the debug APK on the development machine:

    make android-debug

Copy it to the TV and install it over ADB, or use a file manager:

    adb connect <tv-ip>:5555
    adb install -r android/app/build/outputs/apk/debug/app-debug.apk

The application id is `dev.benyamin.kumiho` for release builds and
`dev.benyamin.kumiho.debug` for debug builds. A release APK is produced by
`make android-release` with the signing certificate as described in
[docs/android-testing.md](docs/android-testing.md).

## Launcher presence

The manifest declares both `LAUNCHER` and `LEANBACK_LAUNCHER` and an
`android:banner`. The TV launcher therefore shows the Kumiho banner in the
apps row and on the home screen. `tv_banner.png` is 320×180 and
`ic_launcher_tv.png` is the 160×160 TV icon asset; keep both at their intended
aspect ratios when replacing them.

On Google TV the app also publishes a preview channel named **Kumiho** with
two cards:

- **Open Kumiho** — opens the dashboard.
- **Connect** — opens Kumiho and sends one connect request to the dashboard.

The channel is app-owned and best-effort: the launcher decides whether to show
it, and some launchers hide new channels until the app has been opened once.
The cards use the `kumiho://tv/open` and `kumiho://tv/connect` deep links.
Publishing never blocks startup and never crashes the app if the launcher
rejects it.

## First run

1. Accept the consent step.
2. Sign in with Firefox Accounts. **Continue in browser** opens the in-app
   WebView; the `urn:` redirect is intercepted and returns to the app. If the
   WebView cannot start, use **Paste redirect URL instead** or **Use
   password**.
3. Grant the VPN permission and allow notifications.
4. Review Android's Always-on guidance. Always-on and lockdown are Android
   settings; Kumiho can open the right system page but cannot enable them.

After setup the TV shell opens on **Home**. The same account and settings are
shared with the phone app only if both use the same application id and the
same app-private data; a debug TV install is separate from a release phone
install.

## Remote navigation

- **Up/Down** moves inside the left rail and inside every list or dialog.
- **Left/Right** moves between the rail and the page, or between a row's
  actions.
- **Center/Select** activates the focused control.
- **Back** returns from a dialog or the More page. From a top-level tab,
  Back exits to the TV launcher.
- **Home / Locations / Account / Settings** are the four rail tabs.
  **More** opens Logs and Diagnostics.
- Text fields (search, settings editors) open the system on-screen keyboard.
  Enter/Search closes the keyboard; the field keeps its value.

Every action on Home, Locations, Account, Settings, split tunneling, Logs and
Diagnostics is reachable without a pointer. Focus is never drawn only with
colour: rows scale and gain a border when focused.

## TV behaviour

- The tunnel, watchdog, quota polling and account rotation run in the same
  foreground `VpnService` as on a phone. The TV app has no daemon.
- Split tunneling works exactly as on the phone, including the
  Kumiho-always-outside rule.
- The update check opens the release page in an in-app WebView, so a TV
  without a browser still shows the release notes and download link.
- Logs can be viewed, copied line by line and shared with Android's share
  sheet. Diagnostics can be copied and re-run.
- The Locations tab is one flat server list with real 32×24 flags, a
  two-line `Country · City` / `host:port · protocols` row, search and one
  **Ping all** action. There are no carets or expand/collapse controls, and
  D-pad focus stays on the list after a search or a speed-order switch.
- Proxy-only mode remains available but needs a local client; a TV usually
  has none, so the normal use is a full tunnel.
- IPv6 is blackholed, non-DNS UDP is blocked, and DNS runs as DoH inside the
  tunnel, exactly as on the phone. See
  [docs/android.md](android.md#limitations).

## Performance notes

- The TV shell renders on the device GPU with Compose for TV. Use the
  launcher's own frame rate as the reference; there is no TV-specific
  rendering path or WebView in the tunnel path.
- The flat location list and log console use lazy lists. Very large server
  lists or a log ring at its cap stay scrollable; focus movement is bounded
  by the visible list.
- The TV channel publish happens once per activity start on a background
  coroutine and does not touch the engine.

## Tests

Create and boot the Google TV AVD once:

    sdkmanager "system-images;android-36;google-tv;x86_64"
    avdmanager create avd -n kumiho-tv-api36 \
      -k "system-images;android-36;google-tv;x86_64" -d tv_1080p
    emulator -avd kumiho-tv-api36 -no-snapshot -gpu swiftshader_indirect &

Then run:

    make android-tv-emulator-test   # TV connected suite; phone tests skipped
    make android-emulator-test      # phone connected suite; TV tests skipped
    make android-test               # JVM tests, lint and debug APK

The TV screenshots are written by `TvScreenshotsTest` to
`/sdcard/Android/data/dev.benyamin.kumiho.debug/files/tv-screenshots` for
Home, setup, Locations and Settings in Light, Dark and OLED. Pull them with:

    adb pull /sdcard/Android/data/dev.benyamin.kumiho.debug/files/tv-screenshots

## Physical TV checklist (Sony X90L, API 31)

Run this after the app is installed and the TV is awake. It is the acceptance
list for a real device; the emulator covers navigation and rendering, not the
TV's own launcher or Wi-Fi behaviour.

- [ ] The Kumiho banner appears in the apps row and the home screen.
- [ ] The Google TV **Kumiho** channel shows **Open Kumiho** and **Connect**
      (allow time for the launcher to refresh, and open the app once).
- [ ] **Open Kumiho** opens the dashboard; **Connect** opens it and starts a
      tunnel when an account is signed in.
- [ ] Setup: consent, browser sign-in (WebView), VPN permission,
      notifications and Always-on guidance all complete with the remote.
- [ ] Dashboard: connect, disconnect, reconnect and the options page.
- [ ] Locations: search and no-match copy, real 32×24 flags, flat focusable
      rows, select a server, then Ping all and inspect the speed order
      (Recommended first, failures last); confirm there are no carets or
      expand/collapse controls.
- [ ] Account: add, rename, remove, rotate and sign out.
- [ ] Settings: theme, accent, update check, split tunneling, bypass and every
      engine row/dialog.
- [ ] Logs: filter, search, follow and copy/share.
- [ ] Diagnostics: re-run, copy, shortcuts and reset.
- [ ] Sleep/wake and a Wi-Fi drop keep the service and the watchdog healthy
      (the background probe's redial step).
