#!/usr/bin/env node
// Render the TUI capture files written by TestCaptureViews as README PNGs.
// Dev-time only: no build step and no repository dependency reads it.
//
//   node scripts/screenshots/render-tui.mjs <ansi-dir> <png-dir>
//
// The ANSI is the real Bubble Tea view (see internal/tui/capture_test.go);
// this script only draws a terminal frame around it and converts the SGR
// colour codes to spans. Playwright is resolved through npx, the same cache
// scripts/visual/README.md documents.

import { execFileSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import path from "node:path";
import process from "node:process";
import { pathToFileURL } from "node:url";

const [inputDirArg, outputDirArg] = process.argv.slice(2);
if (!inputDirArg || !outputDirArg) {
  console.error("usage: node scripts/screenshots/render-tui.mjs <ansi-dir> <png-dir>");
  process.exit(2);
}
const inputDir = path.resolve(inputDirArg);
const outputDir = path.resolve(outputDirArg);

const SCREENS = [
  { name: "dashboard", title: "kumiho — TUI" },
  { name: "locations", title: "kumiho — TUI" },
  { name: "settings", title: "kumiho — TUI" },
];

// The TUI's dark theme emits ANSI 208/42/214/203/244. Map those to the house
// palette so the terminal screenshots sit beside the GUI ones; any other
// 256-colour value falls back to the standard xterm cube.
const HOUSE = new Map([
  [42, "#6dd58c"],
  [203, "#f2b8b5"],
  [208, "#e29572"],
  [214, "#ffdf9e"],
  [244, "#938f99"],
]);

function xterm256(n) {
  if (HOUSE.has(n)) return HOUSE.get(n);
  if (n < 16) {
    const basic = [
      "#000000", "#800000", "#008000", "#808000",
      "#000080", "#800080", "#008080", "#c0c0c0",
      "#808080", "#ff0000", "#00ff00", "#ffff00",
      "#0000ff", "#ff00ff", "#00ffff", "#ffffff",
    ];
    return basic[n] || "#e6e1e5";
  }
  if (n < 232) {
    const i = n - 16;
    const r = Math.floor(i / 36);
    const g = Math.floor((i % 36) / 6);
    const b = i % 6;
    const c = (v) => (v === 0 ? 0 : 55 + v * 40);
    return `rgb(${c(r)},${c(g)},${c(b)})`;
  }
  const v = 8 + (n - 232) * 10;
  return `rgb(${v},${v},${v})`;
}

function escapeHTML(s) {
  return s
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function styleOf(state) {
  const css = [];
  if (state.fg) css.push(`color:${state.fg}`);
  if (state.bold) css.push("font-weight:600");
  if (state.dim) css.push("opacity:.72");
  return css.join(";");
}

function applySGR(state, raw) {
  const codes = raw === "" ? [0] : raw.split(";").map(Number);
  for (let i = 0; i < codes.length; i++) {
    const c = codes[i];
    if (c === 0) {
      state.fg = null;
      state.bold = false;
      state.dim = false;
    } else if (c === 1) {
      state.bold = true;
    } else if (c === 2) {
      state.dim = true;
    } else if (c === 22) {
      state.bold = false;
      state.dim = false;
    } else if (c === 39) {
      state.fg = null;
    } else if (c === 38 && codes[i + 1] === 5) {
      state.fg = xterm256(codes[i + 2]);
      i += 2;
    } else if (c === 38 && codes[i + 1] === 2) {
      state.fg = `rgb(${codes[i + 2]},${codes[i + 3]},${codes[i + 4]})`;
      i += 4;
    }
  }
}

function lineHTML(line, state) {
  const re = /\x1b\[([0-9;]*)m/g;
  let html = "";
  let start = 0;
  let match;
  const flush = (end) => {
    if (end <= start) return;
    const text = escapeHTML(line.slice(start, end));
    html += `<span style="${styleOf(state)}">${text}</span>`;
  };
  while ((match = re.exec(line))) {
    flush(match.index);
    applySGR(state, match[1]);
    start = re.lastIndex;
  }
  flush(line.length);
  return html || "&nbsp;";
}

function pageHTML(title, ansi) {
  const state = { fg: null, bold: false, dim: false };
  const lines = ansi.replace(/\r\n/g, "\n").replace(/\n$/, "").split("\n");
  const body = lines.map((line) => `<div class="line">${lineHTML(line, state)}</div>`).join("\n");
  return `<!doctype html>
<meta charset="utf-8">
<title>${escapeHTML(title)}</title>
<style>
  html, body { margin: 0; padding: 0; background: #0f0d13; }
  body {
    display: flex;
    justify-content: center;
    padding: 36px;
    font-family: "DejaVu Sans", -apple-system, "Segoe UI", sans-serif;
  }
  .window {
    background: #1c1b1f;
    border: 1px solid #36343b;
    border-radius: 14px;
    overflow: hidden;
    box-shadow: 0 24px 60px rgba(0, 0, 0, .45);
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 14px 18px;
    background: #222026;
    border-bottom: 1px solid #36343b;
    color: #938f99;
    font-size: 15px;
    line-height: 1;
  }
  .dots { display: flex; gap: 7px; }
  .dot { width: 11px; height: 11px; border-radius: 50%; }
  .red { background: #f2b8b5; }
  .amber { background: #ffdf9e; }
  .green { background: #6dd58c; }
  .title { margin-left: 6px; letter-spacing: .02em; }
  .tag { margin-left: auto; color: #938f99; }
  .terminal {
    padding: 26px 30px 30px;
    color: #e6e1e5;
    font-family: "DejaVu Sans Mono", ui-monospace, monospace;
    font-size: 26px;
    line-height: 1.35;
    white-space: normal;
    text-rendering: geometricPrecision;
    -webkit-font-smoothing: antialiased;
  }
  .line { min-height: 1.35em; white-space: pre; }
</style>
<div class="window">
  <div class="bar">
    <div class="dots"><span class="dot red"></span><span class="dot amber"></span><span class="dot green"></span></div>
    <div class="title">${escapeHTML(title)}</div>
    <div class="tag">linux</div>
  </div>
  <div class="terminal">
${body}
  </div>
</div>
`;
}

mkdirSync(outputDir, { recursive: true });
for (const screen of SCREENS) {
  const ansiPath = path.join(inputDir, `${screen.name}.ansi`);
  if (!existsSync(ansiPath)) {
    console.error(`missing ${ansiPath}; run TestCaptureViews first`);
    process.exit(1);
  }
  const htmlPath = path.join(inputDir, `${screen.name}.html`);
  writeFileSync(htmlPath, pageHTML(screen.title, readFileSync(ansiPath, "utf8")));
  const pngPath = path.join(outputDir, `${screen.name}.png`);
  execFileSync(
    "npx",
    [
      "-y",
      "playwright",
      "screenshot",
      "--full-page",
      "--viewport-size=1800,600",
      "--wait-for-timeout=200",
      pathToFileURL(htmlPath).href,
      pngPath,
    ],
    { stdio: "inherit" },
  );
  console.log(`wrote ${path.relative(process.cwd(), pngPath)}`);
}
