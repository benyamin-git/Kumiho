# Wintun

`amd64/wintun.dll` is the prebuilt, signed Wintun 0.14.1 driver, committed
here so Windows builds stay hermetic (D11). It is redistributed unmodified and
side-by-side with `kumiho.exe`; Kumiho never embeds the DLL or loads it from
memory.

- Source archive: <https://www.wintun.net/builds/wintun-0.14.1.zip>
- Archive SHA-256 (published at wintun.net):
  `07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51`
- `amd64/wintun.dll` SHA-256:
  `e5da8447dc2c320edc0fc52fa01885c103de8c118481f683643cacc3220dafce`
  (427552 bytes)
- Authenticode signer: WireGuard LLC, timestamped 2021-10-17.

Only the amd64 build is committed: Kumiho ships Windows 11 amd64 only (D5).
The prebuilt DLL is covered by the Prebuilt Binaries License in
`LICENSE.txt`, not by Wintun's GPL-2.0 source license; the source is not used
or distributed. The Go API binding (`golang.zx2c4.com/wintun`, MIT) is
vendored under `vendor/`.

Verify a checkout:

```
cd third_party/wintun && sha256sum -c SHA256SUMS
```

Verify the upstream archive before extracting a replacement:

```
curl -fsSLO https://www.wintun.net/builds/wintun-0.14.1.zip
echo '07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51  wintun-0.14.1.zip' | sha256sum -c -
unzip -p wintun-0.14.1.zip wintun/bin/amd64/wintun.dll | sha256sum
```
