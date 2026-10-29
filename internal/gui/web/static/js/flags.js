/**
 * Real flag assets for the graphical clients. The source pack is flag-icons
 * 7.6.0 (MIT, third_party/flag-icons); the GUI serves the 4:3 SVGs from
 * static/flags/4x3. Paths stay relative to the page so the Windows shell's
 * per-run token segment is preserved.
 *
 * The sprite fallback is a project asset, not upstream flag-icons: a star for
 * the Recommended pseudo-country, a globe for an unknown or malformed code.
 * Both are decorative (aria-hidden) and inherit the surrounding colour.
 */

/** The ISO alpha-2 codes bundled under static/flags/4x3. */
const KNOWN = new Set(
  `
ad ae af ag ai al am ao aq ar as at au aw ax az ba bb bd be bf bg bh bi bj
bl bm bn bo bq br bs bt bv bw by bz ca cc cd cf cg ch ci ck cl cm cn co cr
cu cv cw cx cy cz de dj dk dm do dz ec ee eg eh er es et fi fj fk fm fo fr
ga gb gd ge gf gg gh gi gl gm gn gp gq gr gs gt gu gw gy hk hm hn hr ht hu
id ie il im in io iq ir is it je jm jo jp ke kg kh ki km kn kp kr kw ky kz
la lb lc li lk lr ls lt lu lv ly ma mc md me mf mg mh mk ml mm mn mo mp mq
mr ms mt mu mv mw mx my mz na nc ne nf ng ni nl no np nr nu nz om pa pe pf
pg ph pk pl pm pn pr ps pt pw py qa re ro rs ru rw sa sb sc sd se sg sh si
sj sk sl sm sn so sr ss st sv sx sy sz tc td tf tg th tj tk tl tm tn to tr
tt tv tw tz ua ug um us uy uz va vc ve vg vi vn vu wf ws ye yt za zm zw );
  `.trim().split(/\s+/),
);

/** Trim/uppercase, then accept exactly two ASCII letters other than REC. */
function normalize(code) {
  const value = String(code == null ? "" : code).trim().toUpperCase();
  return /^[A-Z]{2}$/.test(value) && value !== "REC" ? value : "";
}

/**
 * The relative flag asset for a country code, or null for REC, blank and
 * malformed codes. The caller detects an unlisted two-letter code with
 * [flagIcon]'s globe fallback.
 */
export function flagAsset(code) {
  const value = normalize(code);
  return value ? "flags/4x3/" + value.toLowerCase() + ".svg" : null;
}

function sprite(kind) {
  const NS = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(NS, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("width", "24");
  svg.setAttribute("height", "18");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  svg.classList.add("flag-icon", "flag-icon--" + kind);
  const path = document.createElementNS(NS, "path");
  path.setAttribute("fill", "currentColor");
  path.setAttribute(
    "d",
    kind === "star"
      ? "M12 17.27 18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2 9.19 8.63 2 9.24l5.46 4.73L5.82 21z"
      : "M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm6.93 6h-2.95c-.32-1.25-.78-2.45-1.38-3.56 1.84.63 3.37 1.91 4.33 3.56zM12 4.04c.83 1.2 1.48 2.53 1.91 3.96h-3.82c.43-1.43 1.08-2.76 1.91-3.96zM4.26 14C4.1 13.36 4 12.69 4 12s.1-1.36.26-2h3.38c-.08.66-.14 1.32-.14 2s.06 1.34.14 2H4.26zm.82 2h2.95c.32 1.25.78 2.45 1.38 3.56-1.84-.63-3.37-1.9-4.33-3.56zm2.95-8H5.08c.96-1.66 2.49-2.93 4.33-3.56C8.81 5.55 8.35 6.75 8.03 8zM12 19.96c-.83-1.2-1.48-2.53-1.91-3.96h3.82c-.43 1.43-1.08 2.76-1.91 3.96zM14.34 14H9.66c-.09-.66-.16-1.32-.16-2s.07-1.35.16-2h4.68c.09.65.16 1.32.16 2s-.07 1.34-.16 2zm.25 5.56c.6-1.11 1.06-2.31 1.38-3.56h2.95c-.96 1.65-2.49 2.93-4.33 3.56zM16.36 14c.08-.66.14-1.32.14-2s-.06-1.34-.14-2h3.38c.16.64.26 1.31.26 2s-.1 1.36-.26 2h-3.38z",
  );
  svg.append(path);
  return svg;
}

/**
 * A decorative flag node: an <img> for a bundled country, a star for REC, or
 * a globe for an unknown/malformed code. The caller adds position classes.
 */
export function flagIcon(code) {
  const value = normalize(code);
  if (value === "") {
    // normalize() rejects REC too; distinguish it from a malformed code.
    const raw = String(code == null ? "" : code).trim().toUpperCase();
    return sprite(raw === "REC" ? "star" : "globe");
  }
  if (!KNOWN.has(value.toLowerCase())) return sprite("globe");
  const img = document.createElement("img");
  img.className = "flag-icon";
  img.src = flagAsset(value);
  img.alt = "";
  img.setAttribute("aria-hidden", "true");
  img.setAttribute("draggable", "false");
  img.width = 24;
  img.height = 18;
  return img;
}
