# flag-icons 7.6.0

Pinned source for Kumiho's real 4:3 country flags.

- Upstream: <https://github.com/lipis/flag-icons>
- Package: `flag-icons@7.6.0`
  (<https://registry.npmjs.org/flag-icons/-/flag-icons-7.6.0.tgz>)
- Tarball SHA-256:
  `99d6feb80562dcaa83700ef184094d7c7b6587cc216b3fab1546cb1643e611a9`
- License: MIT, copied verbatim in `LICENSE`.

Only entries with `"iso": true` in the package's `country.json` are copied:
249 ISO 3166-1 alpha-2 flags. Non-ISO, regional and organisation flags
(`xk`, `xx`, `eu`, `un`, `gb-*`, `arab`, `asean`, `cefta` and similar) are
excluded, so an unknown country code uses the globe fallback.

The GUI serves the SVGs from
`internal/gui/web/static/flags/4x3/<cc>.svg`. Android ships generated
192×144 lossless WebP derivatives in `res/drawable-nodpi`; regenerate them
with:

```sh
scripts/flags/generate-android-flags.sh
```

`SHA256SUMS` covers the committed source SVGs and the license. Verify from the
repository root:

```sh
sha256sum -c third_party/flag-icons/SHA256SUMS
```
