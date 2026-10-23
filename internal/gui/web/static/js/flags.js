/**
 * The ISO 3166-1 alpha-2 flag emoji for a country code, or "" for REC,
 * blank, or anything that is not exactly two ASCII letters after trimming.
 */
export function flagFor(code) {
  const normalized = String(code == null ? "" : code).trim().toUpperCase();
  if (normalized === "REC" || !/^[A-Z]{2}$/.test(normalized)) return "";
  return String.fromCodePoint(
    0x1F1E6 + normalized.charCodeAt(0) - 65,
    0x1F1E6 + normalized.charCodeAt(1) - 65,
  );
}
