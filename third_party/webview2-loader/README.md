# WebView2 loader

`LICENSE.txt` is the verbatim Microsoft 3-Clause BSD license text that covers
the WebView2Loader DLLs embedded in `kumiho-gui.exe`. The
`github.com/jchv/go-webview2` module vendors those DLLs under
`webviewloader/sdk/`; Kumiho's GUI build embeds the amd64 payload into the
executable (D15/A5), so no loader DLL ships as a separate file.

- Upstream: <https://github.com/jchv/go-webview2>
- Vendored module:
  `github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808`
- Embedded payload source (in the vendored tree):
  `vendor/github.com/jchv/go-webview2/webviewloader/sdk/x64/WebView2Loader.dll`

`go mod vendor` prunes `webviewloader/sdk/LICENSE.txt` (it only guarantees
module-root licenses and embedded assets), so the text is retained here
byte-for-byte for binary-redistribution compliance; `NOTICE` points at it. The
release zip carries this `LICENSE.txt` next to `third_party/wintun/LICENSE.txt`.

Only the amd64 payload is built and embedded: Kumiho ships Windows 11 amd64
only (D5). The arm64 and x86 DLLs stay in the vendored tree so their module
builds remain intact.

Verify the committed vendored payloads against the recorded hashes:

```
cd third_party/webview2-loader && sha256sum -c SHA256SUMS
```

To reclaim the text from the module cache if it is ever re-vendored:

```
cp "$(go env GOMODCACHE)"/github.com/jchv/go-webview2@*/webviewloader/sdk/LICENSE.txt .
```
