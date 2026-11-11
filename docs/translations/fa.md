# Persian translation guide

This file is the vocabulary and style reference for Kumiho's Persian UI. It
covers the Windows GUI and the Android phone/TV app. The TUI, the CLI, the
engine logs and the English documentation stay English; `README.fa.md` is the
only translated document.

English is the source of truth. When an English string changes, update the
Persian catalog in the same change and keep the key sets identical. A missing
Persian runtime key falls back to English, but the catalog-parity tests fail
the build so a missing key is never treated as a release-ready fallback.

## Tone

- Formal, neutral Persian. Use the second person plural (`شما`) and avoid
  colloquialisms, slang and contractions.
- Prefer short, direct sentences. UI labels are noun phrases; actions are
  imperatives or verbal nouns that fit the control.
- Do not translate product or protocol names, code identifiers, file names,
  commands, flags, URLs, IP addresses, ports, versions or email addresses.
- Keep the meaning of an error actionable: say what failed and what the user
  can do next. Put raw engine detail in the English details block.

## Fixed vocabulary

| English | Persian | Notes |
|---|---|---|
| VPN | VPN | Latin. Never `وی‌پی‌ان` in UI. |
| tunnel | تونل | |
| proxy | پروکسی | |
| relay | رله | The control-plane relay, not the tunnel. |
| kill switch | قطع اضطراری | Use the same phrase everywhere. |
| account | حساب | |
| server | سرور | |
| location | مکان | A country/city/server selection. |
| connect | اتصال | As a state/action noun. |
| session | اتصال | The VPN session/card; a browser sign-in session is `ورود`. |
| upload | آپلود | Data sent through the tunnel; never `بالا`. |
| download | دانلود | Data received through the tunnel; never `پایین`. |
| disconnect | قطع اتصال | |
| sign in | ورود | Sign-in flow. |
| sign out | خروج | |
| confirm | تأیید | |
| settings | تنظیمات | |
| dashboard | داشبورد | |
| More (menu or sheet) | بیشتر | The overflow menu and the More sheet title. |
| logs | گزارش‌ها | |
| diagnostics | عیب‌یابی | |
| quota | سهمیه | |
| subscription | اشتراک | |
| pass | گذر | The short-lived Guardian pass. |
| setup | راه‌اندازی | |
| browser sign-in | ورود مرورگری | |
| password sign-in | ورود با رمز | |
| lock switch / lockdown | قفل / قطع اضطراری | Android's lockdown is described as `قفل` where the OS owns it; the engine kill switch is `قطع اضطراری`. |
| Always-on VPN | VPN همیشه‌فعال | Android setting. |
| split tunneling | تونل‌شکنی | |
| exit IP | IP خروجی | `IP` stays Latin. |
| country | کشور | |
| city | شهر | |
| fastest | سریع‌ترین | |
| recommended | پیشنهادی | |
| locked location | مکان قفل‌شده | |
| ping | پینگ | |
| timeout | زمان‌بندی تمام شد | For a probe result; or `مهلت تمام شد` in prose. |
| token expiry | انقضای توکن | |
| pass expiry | انقضای گذر | |
| autoconnect | اتصال خودکار | |
| log level | سطح گزارش | |
| redact targets | پوشاندن مقصدها | |
| upstream proxy | پروکسی بالادست | |
| HTTP proxy | پروکسی HTTP | `HTTP` stays Latin. |
| TLS split | تقسیم TLS | |
| keepalive | نگه‌داشتن اتصال | |
| solve challenges | حل چالش‌ها | |
| region | منطقه | |
| blocked region | منطقهٔ مسدود | |
| quarantine | قرنطینه | |
| widget | ویجت | |
| Quick Settings tile | کاشی تنظیمات سریع | |
| notification | اعلان | |
| channel | کانال | |
| theme | تم | The color theme; `پوسته` is only the TV/phone shell. |
| accent | رنگ اصلی | Not `رنگ تأکید`. |
| version | نسخه | |
| update | به‌روزرسانی | |
| release | انتشار | |
| draft | پیش‌نویس | |
| SHA-256 | SHA-256 | Latin. |

## Do not translate

Keep these exactly as supplied, including capitalization:

- `Kumiho`, `Mozilla`, `Firefox`, `Firefox Accounts`, `Windows`, `Android`,
  `Google TV`, `Fastly`, `Cloudflare`, `Quad9`.
- Protocol and product words: `VPN`, `HTTP`, `HTTP/2`, `HTTP/3`, `HTTPS`,
  `TCP`, `UDP`, `DNS`, `DoH`, `QUIC`, `TLS`, `SOCKS5`, `CIDR`, `IP`, `IPv4`,
  `IPv6`, `MTU`, `API`, `JSON`, `JEXL`, `OAuth`, `PKCE`, `2FA`, `TOTP`.
- Commands, flags, file names, paths, environment variables and service names,
  for example `kumiho doctor`, `--json`, `config.toml`, `tokens.json`,
  `pass.json`, `relay.token`, `gui.toml`, `KUMIHO_*`.
- Country names, city names, server hostnames, protocol names inside server
  rows, email addresses, URLs, IP addresses, ports and version strings.
- Log lines, diagnostic check titles and raw engine messages.

## Digits and dates

- Use Persian digits (`۰۱۲۳۴۵۶۷۸۹`) for user-facing counts, percentages,
  durations, byte sizes, quotas and dates.
- Keep technical values Latin: IP addresses, ports, versions, token fragments,
  file sizes in logs, hostnames and command examples.
- Format long dates in the Jalali calendar with Persian digits and the Persian
  month names, for example `۱۹ آذر ۱۴۰۵`. A time may follow with a Persian
  colon-separated clock, for example `۱۹ آذر ۱۴۰۵، ۱۳:۰۸`.
- Byte sizes use the localized unit (`بایت`, `کیلوبایت`, `مگابایت`,
  `گیگابایت`, `ترابایت`) and trim a trailing `.0` in Persian. Data rates keep
  one decimal when the value is not whole.
- Percentages use the Persian percent sign (`٪`) in prose and the same
  localized form in cards where space allows.

## Mixed text and bidi

- Persian is the paragraph direction in Persian UI. Latin runs inside a
  Persian sentence are isolated so they cannot reorder the surrounding text.
- Use the platform's isolate helpers: the GUI wraps technical runs in
  `<bdi>`; Android uses `BidiText.isolateLtr` (LRI/PDI). Do not solve mixed
  text by inserting LRM/RLM characters by hand unless the platform helper is
  unavailable.
- Technical rows that are entirely Latin — a `host:port`, a server summary, a
  log line, a diagnostic detail — are isolated as a whole line.
- Parentheses around a Latin value stay with that value. Do not split a
  `Country · City · host:port · protocols` line into separately aligned
  fragments.
- Flags, the Kumiho logo, the power glyph and platform logos are never
  mirrored. Directional icons (back, chevrons, arrows, progress) follow the
  layout direction.
- Lists, navigation rails, progress bars, tabs and carousels mirror in RTL.
  Numbers inside a chart or table keep their own local order.

## Errors and empty states

- A Persian error has a short summary and, when useful, one action:
  `اتصال برقرار نشد` / `دوباره تلاش کنید`.
- The raw engine message remains available as English detail in an
  expandable, left-to-right block. Do not translate stack traces, HTTP
  status text or server messages.
- Unknown error codes use the generic Persian error and keep the English
  detail. Known codes map to a specific Persian summary.
- Empty states say what is missing and what the user can do; they do not
  blame the user and do not use exclamation marks.

## Review checklist

- Catalog parity: every English key has a Persian key and vice versa.
- No untranslated English sentence remains in Compose or GUI JavaScript
  except the approved Latin terms above.
- All numbers, dates and byte sizes use the rules above.
- Every screen was checked in Light, Dark and OLED where the platform supports
  them, at the smallest supported window or phone width, with the rail on the
  correct side.
- Verbs, buttons and status words match the English source's intent; no
  sentence is a literal word-for-word translation that reads awkwardly.
- The Persian README uses the same vocabulary and links back to the English
  source of truth.
