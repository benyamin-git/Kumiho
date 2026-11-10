/* Kumiho GUI translation and formatting foundation.
 *
 * The controller keeps its English strings and stable codes; this module is
 * the only place that turns a code/key plus raw values into Persian UI text.
 * Catalogs are flat dotted-key JSON objects, loaded same-origin so the
 * server's per-run token segment stays intact. English is always loaded as the
 * fallback first; a missing key falls back to English and then to the key
 * itself, which keeps a half-translated build usable.
 *
 * The shell stays hidden (`body.app-booting`) until initI18n resolves, so a
 * Persian first paint never flashes English. The server injects the cached
 * effective language as a meta tag; app.js may override it with `?lang=fa`
 * for captures.
 */

const EN = "en";
const FA = "fa";
const catalogs = Object.create(null);
const catalogPromises = Object.create(null);
const preloadedFonts = new Set();
let activeLanguage = EN;

const FONT_FILES = [
  "vazirmatn-regular.woff2",
  "vazirmatn-medium.woff2",
  "vazirmatn-semibold.woff2",
  "vazirmatn-bold.woff2"
];

function normalizeLanguage(lang) {
  return lang === FA ? FA : EN;
}

function has(lang, key) {
  const catalog = catalogs[lang];
  return !!catalog && Object.prototype.hasOwnProperty.call(catalog, key);
}

function loadCatalog(lang) {
  if (catalogs[lang]) return Promise.resolve(catalogs[lang]);
  if (catalogPromises[lang]) return catalogPromises[lang];
  catalogPromises[lang] = fetch("i18n/" + lang + ".json", { cache: "no-cache" })
    .then(function (response) {
      if (!response.ok) throw new Error("HTTP " + response.status);
      return response.json();
    })
    .then(function (catalog) {
      if (!catalog || typeof catalog !== "object" || Array.isArray(catalog)) {
        throw new Error("catalog is not a flat object");
      }
      catalogs[lang] = catalog;
      return catalog;
    })
    .catch(function (err) {
      console.error("kumiho: could not load the " + lang + " catalog", err);
      catalogs[lang] = {};
      return catalogs[lang];
    })
    .finally(function () {
      delete catalogPromises[lang];
    });
  return catalogPromises[lang];
}

function applyDocumentLanguage() {
  document.documentElement.lang = activeLanguage;
  document.documentElement.dir = activeLanguage === FA ? "rtl" : "ltr";
}

function interpolate(value, params) {
  if (!params) return String(value);
  return String(value).replace(/\{([A-Za-z0-9_]+)\}/g, function (match, name) {
    if (!Object.prototype.hasOwnProperty.call(params, name)) return match;
    const replacement = params[name];
    return replacement === null || replacement === undefined ? "" : String(replacement);
  });
}

function lookup(key) {
  if (has(activeLanguage, key)) return catalogs[activeLanguage][key];
  if (has(EN, key)) return catalogs[EN][key];
  return undefined;
}

function preloadFonts() {
  for (const file of FONT_FILES) {
    if (preloadedFonts.has(file)) continue;
    const link = document.createElement("link");
    link.rel = "preload";
    link.as = "font";
    link.type = "font/woff2";
    link.href = "fonts/" + file;
    link.crossOrigin = "anonymous";
    document.head.append(link);
    preloadedFonts.add(file);
  }
}

function toDate(value) {
  if (value instanceof Date) return isNaN(value.getTime()) ? null : value;
  if (typeof value === "number") {
    const ms = value < 1e12 ? value * 1000 : value;
    const date = new Date(ms);
    return isNaN(date.getTime()) ? null : date;
  }
  if (typeof value === "string" && /^\d+$/.test(value.trim())) {
    return toDate(Number(value.trim()));
  }
  if (typeof value === "string" && value.trim() !== "") {
    const date = new Date(value);
    return isNaN(date.getTime()) ? null : date;
  }
  return null;
}

/** Load English and the requested catalog, then apply the document language. */
export async function initI18n(lang) {
  const next = normalizeLanguage(lang);
  const loaded = await Promise.all([
    loadCatalog(EN),
    next === FA ? loadCatalog(FA) : Promise.resolve(null)
  ]);
  if (!loaded[0]) catalogs[EN] = {};
  if (next === FA && !loaded[1]) catalogs[FA] = {};
  activeLanguage = next;
  applyDocumentLanguage();
  applyTranslations();
  if (next === FA) preloadFonts();
  return next;
}

/** Switch language in place, update data-i18n nodes and announce the change. */
export async function setLanguage(lang) {
  const next = await initI18n(lang);
  window.dispatchEvent(new CustomEvent("kumiho:languagechange", { detail: { language: next } }));
  return next;
}

export function currentLanguage() {
  return activeLanguage;
}

/** Whether a key exists in the active catalog or the English fallback. */
export function hasTranslation(key) {
  return lookup(key) !== undefined;
}

/**
 * The Persian (or English) summary for a protocol error code. Known
 * `error.<code>` entries win; everything else gets the generic summary. The
 * raw engine error belongs in the LTR details block, never here.
 */
export function errorSummary(code) {
  const key = code ? "error." + code : "";
  return key && hasTranslation(key) ? t(key) : t("error.generic");
}

/**
 * Present one structured controller notice: the localized summary from
 * `notice.<code>` (with its raw params) and the raw engine error for the LTR
 * details block. Unknown codes keep the controller's English message.
 */
export function noticeText(notice) {
  if (!notice) return { text: "", detail: "", tone: "" };
  const code = notice.code || "";
  const key = code ? "notice." + code : "";
  const params = notice.params || {};
  const text = key && hasTranslation(key)
    ? t(key, params)
    : (notice.message || "");
  const detail = typeof params.error === "string" ? params.error : "";
  return { text: text, detail: detail, tone: notice.tone || "" };
}

/** Translate a key: active catalog, then English, then the key itself. */
export function t(key, params) {
  const value = lookup(key);
  return value === undefined ? String(key) : interpolate(value, params);
}

/** Plural helper: prefer `<key>.one`/`<key>.other` when provided. */
export function tp(key, count, params) {
  const n = Number(count);
  let chosen = key;
  if (n === 1 && (has(activeLanguage, key + ".one") || has(EN, key + ".one"))) {
    chosen = key + ".one";
  } else if (n !== 1 && (has(activeLanguage, key + ".other") || has(EN, key + ".other"))) {
    chosen = key + ".other";
  }
  return t(chosen, Object.assign({}, params || {}, { count: n }));
}

export function formatNumber(value, options) {
  if (value === null || value === undefined || value === "") return "";
  const number = Number(value);
  if (!Number.isFinite(number)) return String(value);
  return new Intl.NumberFormat(activeLanguage === FA ? "fa-IR" : "en", options || {}).format(number);
}

export function formatDate(value, options) {
  const date = toDate(value);
  if (!date) return "";
  const locale = activeLanguage === FA ? "fa-IR-u-ca-persian" : "en";
  return new Intl.DateTimeFormat(locale, options || { dateStyle: "medium" }).format(date);
}

export function formatDateTime(value, options) {
  return formatDate(value, options || { dateStyle: "medium", timeStyle: "short" });
}

export function formatBytes(value) {
  if (value === null || value === undefined || value === "") return "";
  let number = Number(value);
  if (!Number.isFinite(number)) return String(value);
  const units = [
    t("unit.bytes"),
    t("unit.kib"),
    t("unit.mib"),
    t("unit.gib"),
    t("unit.tib")
  ];
  let unit = 0;
  while (number >= 1024 && unit < units.length - 1) {
    number /= 1024;
    unit += 1;
  }
  const digits = unit === 0 ? 0 : number < 10 ? 1 : 0;
  return formatNumber(number, { maximumFractionDigits: digits, minimumFractionDigits: digits }) + " " + units[unit];
}

export function formatDuration(seconds) {
  let total = Math.max(0, Math.floor(Number(seconds) || 0));
  const days = Math.floor(total / 86400);
  total -= days * 86400;
  const hours = Math.floor(total / 3600);
  total -= hours * 3600;
  const minutes = Math.floor(total / 60);
  const secs = total - minutes * 60;
  if (days > 0) {
    return formatNumber(days) + t("unit.days_short") + " " + formatNumber(hours) + t("unit.hours_short");
  }
  if (hours > 0) {
    return formatNumber(hours) + t("unit.hours_short") + " " + formatNumber(minutes) + t("unit.minutes_short");
  }
  if (minutes > 0) {
    return formatNumber(minutes) + t("unit.minutes_short") + " " + formatNumber(secs) + t("unit.seconds_short");
  }
  return formatNumber(secs) + t("unit.seconds_short");
}

/** Apply data-i18n text and common attribute translations under root. */
export function applyTranslations(root) {
  const scope = root || document;
  for (const node of scope.querySelectorAll("[data-i18n]")) {
    node.textContent = t(node.getAttribute("data-i18n"));
  }
  for (const node of scope.querySelectorAll("[data-i18n-aria-label]")) {
    node.setAttribute("aria-label", t(node.getAttribute("data-i18n-aria-label")));
  }
  for (const node of scope.querySelectorAll("[data-i18n-placeholder]")) {
    node.setAttribute("placeholder", t(node.getAttribute("data-i18n-placeholder")));
  }
  for (const node of scope.querySelectorAll("[data-i18n-title]")) {
    node.setAttribute("title", t(node.getAttribute("data-i18n-title")));
  }
}
