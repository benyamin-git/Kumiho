# Vazirmatn

Vazirmatn v33.003, redistributed unmodified under the SIL Open Font License
1.1 (`OFL.txt`). Kumiho uses the four static weights to render Persian UI text
in the Windows GUI and the Android app; English keeps the system font.

- Project: <https://github.com/rastikerdar/vazirmatn>
- Release: <https://github.com/rastikerdar/vazirmatn/releases/tag/v33.003>
- Source archive:
  <https://github.com/rastikerdar/vazirmatn/releases/download/v33.003/vazirmatn-v33.003.zip>
- Archive SHA-256:
  `0a9afd41967e6f57096a56a181a23f81a2b999b62f1f2a4e4b26736580854fdb`

## Committed files

The archive is extracted from:

- `fonts/ttf/Vazirmatn-{Regular,Medium,SemiBold,Bold}.ttf` — Android
  `res/font/` resources.
- `fonts/webfonts/Vazirmatn-{Regular,Medium,SemiBold,Bold}.woff2` — Windows
  GUI `@font-face` assets.
- `OFL.txt` — the full license text.

Both formats are kept here because the two clients ship different formats
(D32). The weights are 400 (Regular), 500 (Medium), 600 (SemiBold) and 700
(Bold).

Verify a checkout:

```
cd third_party/vazirmatn && sha256sum -c SHA256SUMS
```

## Replacing the font

1. Download the release archive and verify the archive checksum above.
2. Extract the eight font files and `OFL.txt` with the same paths.
3. Replace the files in this directory, keeping the names unchanged.
4. Regenerate `SHA256SUMS`:

   ```
   sha256sum OFL.txt Vazirmatn-*.ttf Vazirmatn-*.woff2 > SHA256SUMS
   ```

5. Rebuild the GUI web assets and the Android font resources; run both client
   gates. Update `NOTICE` only if the license or attribution changes.
