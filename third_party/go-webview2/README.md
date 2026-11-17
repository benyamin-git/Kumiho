# go-webview2 vendor patch

`vendor/github.com/jchv/go-webview2` is not pristine upstream. Kumiho patches
it additively so the Windows shell can fence its message bridge:

- `pkg/edge/ICoreWebView2NavigationStartingEventArgs.go` and
  `...EventHandler.go` (new files): the typed COM plumbing for WebView2's
  `NavigationStarting` event.
- `pkg/edge/corewebview2.go`: `AddNavigationStarting` helper; `GetSource` is
  read for every web message.
- `pkg/edge/chromium.go`: `NavigationStartingCallback`, the
  `NavigationStarting` handler, and `MessageCallback` now receives the source
  document URI.
- `webview.go` / `common.go`: `SetNavigationGuard` and
  `SetMessageSourceFilter` on the `WebView` interface.

The shell installs both from `internal/gui/host_windows.go`. The guard allows
only the tokenized asset-server URL (plus the initial `about:blank`); the
source filter drops any message whose document URL does not carry that token.

**Re-vendoring or upgrading the module overwrites these files.** Re-apply the
same changes (or move them upstream) before building Windows again, and run
the full gate's `GOOS=windows` steps.

The patch was introduced on 2026-11-17 for the deferred G1 finding in
`docs/code-review-2026-11-16.md`.
