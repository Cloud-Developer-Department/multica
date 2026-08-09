// Shared formatting helpers for the engineering-analytics platform
// (`/{slug}/analytics`, CLO-240). Follows the API contract's empty-value
// semantics: `null → MISSING`, `0 → numeric zero`, `[] → empty state`.

/** The "missing data" glyph used across the page (API §0.5 `null → '-'`). */
export const MISSING = "—";

/** 0–1 ratio → "55.6%" (one decimal, per contract §0.5 ratio rendering). */
export function formatPercent(ratio: number | null | undefined): string {
  if (ratio == null || !Number.isFinite(ratio)) return MISSING;
  return `${(ratio * 100).toFixed(1)}%`;
}

/** 0–1 ratio → whole-percent "66%" (used for progress bars / coarse shares). */
export function formatPercentInt(ratio: number | null | undefined): string {
  if (ratio == null || !Number.isFinite(ratio)) return MISSING;
  return `${Math.round(ratio * 100)}%`;
}

/** Compact number: 48200 → "48.2k", 1600 → "1.6k", 950 → "950". */
export function formatCompact(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return MISSING;
  if (Math.abs(n) >= 1000) {
    const scaled = n / 1000;
    return `${Number.isInteger(scaled) ? scaled : scaled.toFixed(1)}k`;
  }
  return String(Math.round(n));
}

/** Seconds → compact 天/时/分, contract §0.6 duration rendering.
 *  `null` → MISSING; sub-minute values render as "<1分". */
export function formatDurationZh(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds)) return MISSING;
  if (seconds < 0) return MISSING;
  if (seconds < 60) return "<1分";
  const totalMinutes = Math.floor(seconds / 60);
  const hours = Math.floor(totalMinutes / 60);
  const days = Math.floor(hours / 24);
  if (days > 0) {
    const h = hours % 24;
    return h > 0 ? `${days}天${h}时` : `${days}天`;
  }
  if (hours > 0) {
    const m = totalMinutes % 60;
    return m > 0 ? `${hours}时${m}分` : `${hours}时`;
  }
  return `${totalMinutes}分`;
}

/** Seconds → compact "1d 2h" / "12h" / "45m" (neutral locale, for durations
 *  that appear next to mono data). */
export function formatDurationEn(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds)) return MISSING;
  if (seconds < 0) return MISSING;
  if (seconds < 60) return "<1m";
  const totalMinutes = Math.floor(seconds / 60);
  const hours = Math.floor(totalMinutes / 60);
  const days = Math.floor(hours / 24);
  if (days > 0) {
    const h = hours % 24;
    return h > 0 ? `${days}d ${h}h` : `${days}d`;
  }
  if (hours > 0) {
    const m = totalMinutes % 60;
    return m > 0 ? `${hours}h ${m}m` : `${hours}h`;
  }
  return `${totalMinutes}m`;
}

/** RFC3339 → "YYYY-MM-DD" (date-only, contract date format). */
export function formatDateOnly(iso: string | null | undefined): string {
  if (!iso) return MISSING;
  return iso.slice(0, 10);
}

/** RFC3339 → "MM-DD" short date for badges (e.g. "数据截至 08-05"). */
export function formatDateShort(iso: string | null | undefined): string {
  if (!iso) return MISSING;
  return iso.slice(5, 10);
}

/** RFC3339 → "YYYY-MM-DD HH:mm" wall-clock display. */
export function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return MISSING;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return MISSING;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Generic seconds→compact that the page-level i18n uses. Kept locale-agnostic
 *  so the four locale bundles only choose which compact formatter to call. */
export type DurationFormatter = (seconds: number | null | undefined) => string;

export const DURATION_ZH: DurationFormatter = formatDurationZh;
export const DURATION_EN: DurationFormatter = formatDurationEn;
