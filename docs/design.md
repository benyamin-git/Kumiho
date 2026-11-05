# Kumiho Design Language

**Version:** 1.0 (v0.11.0)
**Date:** 2026-11-05

## Purpose and precedence

The `design` skill and the house-style reference are the rulebook: six
accents, three themes, density, component patterns, the twelve banned patterns
and the accessibility floor. This document records how Kumiho applies that
rulebook. It is the platform-neutral reference for every client — the Android
phone app today, the Windows GUI and the TV shell in later releases.

Where this document and the skill disagree, the skill wins and this document is
the bug. Where Kumiho makes an approved departure, the departure is named here
and dated in the change log.

Tokens are the contract, not the code: any client that renders the roles, the
gauge rules, the motion values and the chart encoding below is the same design
language.

## Palette

The palette is generated once, in Go (`internal/gui/theme`), and consumed by the
Android bridge and the GUI's CSS custom properties. Values are opaque
`#rrggbb`, so one token string is valid in CSS, in Compose and in a vector
drawable.

There are 29 roles in every theme × accent pair:

| Group | Roles |
| --- | --- |
| Primary | `primary`, `on-primary`, `primary-container`, `on-primary-container`, `surface-tint`, `inverse-primary` |
| Secondary | `secondary`, `on-secondary`, `secondary-container`, `on-secondary-container` |
| Tertiary | `tertiary`, `on-tertiary`, `tertiary-container`, `on-tertiary-container` |
| Neutrals | `background`, `on-surface`, `outline`, `surface-container-1` … `surface-container-5` |
| Status | `profit`, `loss`, `warning` |
| Data | `data-line`, `data-fill`, `data-grid`, `glow` |

The six accents are blue (default), teal, green, orange, rose and violet. Roles
derive from the accent seed in OKLCH; the neutrals and status colours are the
house table's exact hexes and are never accent-tinted.

### Data roles

| Role | Meaning | Derivation |
| --- | --- | --- |
| `data-line` | The stroke of a data trace | the accent's `primary` |
| `data-fill` | The filled area under a data trace | `primary` at 18% |
| `data-grid` | Axis, baseline and grid lines | `outline` at 32% |
| `glow` | The hero glow behind the gauge | `primary` at 16% |

**The background-only rule.** All four data roles are pre-composited on
`background` and emitted opaque. A chart or glow must therefore sit on the page
background, never on a raised `surface-container` fill. A consumer that needs a
trace on another surface derives the colour locally from the theme's `primary`
or `outline` and the surface it is actually painting on; reusing the token
there would double-tint.

This is why the cards that host numbers do not host charts: the gauge and the
Diagnostics charts are drawn directly on the page.

## Themes

Three themes, one palette shape:

- **Light** — the house light neutrals.
- **Dark** — the house dark neutrals; the default on a dark system.
- **OLED** — true black `#000000` background and a fixed five-step container
  ladder; opt-in.

First run follows the system `prefers-color-scheme`. OLED is never chosen
automatically. Theme and accent are per-device settings, and every screen must
be reviewed in all three themes; a screen that only reads well in Dark is not
finished.

## Gauge

The dashboard hero is **one** gauge. It replaces the old power button, and no
second circular element competes with it.

Anatomy, outermost to innermost:

1. **Glow** — a radial brush from `glow` to transparent, radius `size / 2`,
   drawn outside the arc. Only while connected and the app is resumed.
2. **Track** — a full 360° arc at `outlineVariant`, 8 dp.
3. **Quota arc** — 8 dp, starting at −90° (twelve o'clock) and filling
   clockwise to `remaining / limit`. A null fraction draws nothing: an empty
   arc plus **Quota unknown**, never a fake zero or a full ring.
4. **State sweep** — while connecting or reconnecting, a 90° `data-line` arc
   rotates around the track. It settles into the quota position on connect.
   Reduced motion draws a static 90° sweep at the top instead.
5. **Throughput trace** — two concentric bands inside the arc
   (`TRACE_BAND_DP` = 18 dp in total). Download is a filled band at
   `data-fill`; upload is a 2 dp line at `data-line`. No samples draws nothing.
6. **Centre** — the power glyph on a 96 dp `primary` / `primary-container`
   circle. This circle is the touch target: at least 48 dp, and the whole gauge
   is clickable when enabled.

Default size is 248 dp; the arc stroke is 8 dp and the sweep starts at −90°.
The state is carried four ways — arc behaviour, colour, icon and the state word
below the gauge — so colour is never the only signal.

Below the state word sit the current rates, `↓ 45.6 MiB/s ↑ 12.3 MiB/s`, in
tabular figures. They are the TalkBack numbers; the trace is context. While not
connected the rate line is absent.

**Degradation:** no quota information → empty arc and **Quota unknown**; no
samples → no trace; reduced motion → no rotating sweep; a chart that cannot be
drawn never becomes an empty axis with a fake zero.

The gauge's touch target stays the power control. Tapping it connects or
disconnects; when disabled or busy it does nothing, and TalkBack announces the
state instead of an action.

## Charts

Charts are Compose Canvas drawings. No charting library is used, and nothing is
persisted: charts are a view of the live session and vanish with it.

**Throughput.** A 60 s window fed by the engine's 2 s status pushes — at most 30
samples, held in memory by a pure Kotlin `RateBuffer`. A gap wider than 5 s
clears the buffer, because the trace must never draw a straight line across a
pause. Download is a filled area (`data-fill`), upload is a stroked line
(`data-line`), the baseline is `data-grid`. Axis labels use `bodySmall` with
tabular figures. The chart is accompanied by a text summary
(`Down … · Up … · Peak …`), so the numbers are readable without the drawing.

**Latency.** Horizontal bars, one per host, longest first, `data-line`, host
label at `bodySmall`, RTT right-aligned in tabular figures. The chart reads the
engine's cached round trips only; it never starts a ping. When the cache cannot
be read, the chart is replaced by an honest line — never an empty axis.

**Encoding rules.** One accent: download and upload differ by form and label,
not by hue. Nothing is encoded by colour alone.

## Motion

Four motion moments, all in the same reduced-motion wrapper:

| Moment | Spec |
| --- | --- |
| Arc settle | 900 ms linear sweep; snap under reduced motion |
| Glow | 6 s breathing cycle, 55% → 100% of the `glow` token, reversing; steady 60% under reduced motion |
| Phase change | 180 ms cross-fade of the fact block; snap under reduced motion |
| Connect pulse | centre scales 1.0 → 1.06 → 1.0 in 180 ms, plus one haptic tick |

The glow runs only while the app is resumed: leaving the foreground stops the
transition, so the gauge never animates off-screen. The haptic fires once on a
successful connect — never on failure, never on reconnect churn.

Under system "remove animations", every animated value snaps and the glow
settles to a steady low alpha rather than disappearing, so the gauge keeps its
shape. The haptic stays: it is feedback, not motion.

## Brand

The brand presence inside the app is the **wordmark only**: **Kumiho** set in
the system family (Roboto SemiBold on Android) with 0.06 em tracking. No fox
artwork appears inside the app; the launcher icon and TV art are unchanged.

The wordmark is outlined to a vector drawable and tinted from `on-surface`, so
it is crisp at every density and correct in all three themes. It appears on the
launch screen only, through the Android 12+ system splash branding image.
Android 8–11 keep the icon-only splash; no in-app frame is added and no startup
work is done to fake one.

## Components

**Cards.** `surface-container` fill, 1 dp `outlineVariant` hairline, 12 dp
radius, 16 dp content padding. Cards separate by hairline, not shadow. The
container ladder supplies raised steps only where they mean hierarchy; the page
behind charts is always `background`.

**Rows.** 56 dp list rows, 48 dp minimum touch target, leading icon, title and
optional trailing value. A row never carries a second line of explanation that
a section header could carry better.

**Chips.** Pills. A selectable chip is Material's filter chip at the 6 dp
radius; a status chip is a 16% tint with a matching 50% border and always
carries an icon plus text, so its meaning survives without colour (`Profit`,
`Loss`, `Warning`, `Neutral`).

**Segmented control.** House component for two to four exclusive choices; the
selected segment takes the primary tint and the group keeps the 8 dp radius.
It is never used for navigation.

**State views.** Empty, error and loading states are explicit composables:
glyph, one-sentence cause, one primary action. An error keeps the engine's own
wording rather than rewording it, and it never replaces a live tunnel because a
refresh failed.

**Navigation.** Five destinations — Home, Locations, Account, Settings, More.
Under 600 dp the shell uses a bottom bar with a More sheet; at 600 dp and wider
it uses a rail. More holds Logs and Diagnostics and has no route of its own.
The top bar is 64 dp. Navigation structure is not part of this language
version; it is described here so adaptations keep the same shape.

**Density.** A 4 dp spacing scale, 16 dp screen padding, reading content capped
at 720 dp, and the radius scale 6 / 8 / 12 / 16 / full.

## Accessibility

The floor is non-negotiable and applies to every client:

- Body text at least 4.5:1 in all three themes and every accent; non-text
  elements at least 3:1.
- Every touch target at least 48 dp.
- Meaning is never colour-only: state carries a word and an icon as well.
- Every new control is labelled for screen readers; charts are summarised in
  words with the exact numbers rendered as text.
- The glow is never the sole indicator of a connection.
- Reduced motion is honoured everywhere, and focus order is unchanged.

## Adaptation notes

This document is written for the phone first because the phone is the reference
implementation. Other clients are **adaptations of this language**, not second
design systems:

- **Android TV** is a ten-foot adaptation: larger type, a visible focus ring,
  remote-first focus and an overscan-safe frame are approved form-factor
  departures. The palette, gauge rules, motion values and chart encoding stay
  the same. TV adoption is a separate release.
- **Windows GUI** is a desktop adaptation of this language, scheduled for
  v0.12.0. It consumes the same CSS custom properties, including the four data
  roles, and keeps the same component anatomy at desktop density.
- **Platform-specifics** that do not travel: the Android splash API, home-screen
  `RemoteViews` widgets, the bottom bar / rail breakpoint, and system intents.
  They are called out here so a port does not mistake them for language.

## Change log

| Date | Decision | Spec |
| --- | --- | --- |
| 2026-11-05 | v1.0: four data roles, gauge anatomy, 60 s charts, motion specs, wordmark, UX reworks | [spec](superpowers/specs/2026-11-05-v0.11.0-android-design-language-design.md) |
