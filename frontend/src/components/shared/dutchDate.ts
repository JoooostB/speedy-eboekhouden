/**
 * Pure date rules behind DateField. Every date is an ISO string
 * `YYYY-MM-DD`. A Date is only ever built from local year/month/day parts
 * (new Date(y, m - 1, d)) and read back with getFullYear/getMonth/getDate:
 * `new Date(iso)` parses as UTC midnight and toISOString() writes UTC, so
 * either can shift a date by a day in a timezone east or west of UTC.
 *
 * Months are 1-based (1 = januari) everywhere in this module, matching the
 * ISO strings.
 */

export const MONTH_NAMES = [
  "januari", "februari", "maart", "april", "mei", "juni",
  "juli", "augustus", "september", "oktober", "november", "december",
] as const;

export const MONTH_SHORT_NAMES = [
  "jan", "feb", "mrt", "apr", "mei", "jun",
  "jul", "aug", "sep", "okt", "nov", "dec",
] as const;

/** Monday first, as in the Netherlands. */
export const WEEKDAY_NAMES = [
  "maandag", "dinsdag", "woensdag", "donderdag", "vrijdag", "zaterdag", "zondag",
] as const;

export const WEEKDAY_SHORT_NAMES = ["ma", "di", "wo", "do", "vr", "za", "zo"] as const;

export const MIN_YEAR = 1900;
export const MAX_YEAR = 2100;

export interface DateParts {
  year: number;
  /** 1..12 */
  month: number;
  day: number;
}

export interface MonthGridCell {
  iso: string;
  day: number;
  /** False for the leading and trailing days of the adjacent months. */
  inMonth: boolean;
}

const pad = (n: number, width = 2) => String(n).padStart(width, "0");

/** Number of days in a month (month 1..12). */
export function daysInMonth(year: number, month: number): number {
  // Day 0 of the next month is the last day of this one.
  return new Date(year, month, 0).getDate();
}

/** ISO from parts, without any validation. */
export function toIso(year: number, month: number, day: number): string {
  return `${pad(year, 4)}-${pad(month)}-${pad(day)}`;
}

/** A real calendar day, any year. */
function isCalendarDate(year: number, month: number, day: number): boolean {
  return (
    Number.isInteger(year) &&
    Number.isInteger(month) &&
    Number.isInteger(day) &&
    month >= 1 &&
    month <= 12 &&
    day >= 1 &&
    day <= daysInMonth(year, month)
  );
}

/** A real calendar day within the years a user may enter. */
function isRealDate(year: number, month: number, day: number): boolean {
  return year >= MIN_YEAR && year <= MAX_YEAR && isCalendarDate(year, month, day);
}

/** The parts of an ISO string that names a real calendar day, or null.
 *  Not limited to MIN_YEAR..MAX_YEAR, so date arithmetic at the edges of
 *  that range (a month grid of december 2100) keeps working. */
export function isoParts(iso: string): DateParts | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  if (!m) return null;
  const year = Number(m[1]);
  const month = Number(m[2]);
  const day = Number(m[3]);
  return isCalendarDate(year, month, day) ? { year, month, day } : null;
}

/** True for a real calendar date `YYYY-MM-DD` (2026-02-30 is not) in the
 *  years MIN_YEAR..MAX_YEAR. */
export function isValidIso(s: string): boolean {
  const p = isoParts(s);
  return p !== null && isRealDate(p.year, p.month, p.day);
}

function fromDate(d: Date): string {
  return toIso(d.getFullYear(), d.getMonth() + 1, d.getDate());
}

function toLocalDate(iso: string): Date | null {
  const p = isoParts(iso);
  return p ? new Date(p.year, p.month - 1, p.day) : null;
}

/** Today in the browser's own timezone. */
export function todayIso(now: Date = new Date()): string {
  return fromDate(now);
}

function build(year: number, month: number, day: number): string | null {
  return isRealDate(year, month, day) ? toIso(year, month, day) : null;
}

/** A 2-digit year is 20xx; anything else is taken as written. */
function fullYear(text: string): number {
  return text.length === 2 ? 2000 + Number(text) : Number(text);
}

/**
 * Reads what a Dutch user types into a date field. Accepts
 *   d-m-jjjj, d/m/jjjj, d.m.jjjj and d m jjjj (also with a 2-digit year),
 *   d-m without a year (the year of `today`),
 *   ddmmjjjj and ddmmjj as plain digits,
 *   jjjj-mm-dd (ISO).
 * Returns the ISO date, or null for anything else or an impossible date.
 */
export function parseDutchDate(text: string, today: string = todayIso()): string | null {
  const s = text.trim();
  if (s === "") return null;

  // ISO first: its 4-digit lead cannot be a day.
  const iso = /^(\d{4})-(\d{1,2})-(\d{1,2})$/.exec(s);
  if (iso) return build(Number(iso[1]), Number(iso[2]), Number(iso[3]));

  const digits = /^(\d{2})(\d{2})(\d{4}|\d{2})$/.exec(s);
  if (digits) return build(fullYear(digits[3]), Number(digits[2]), Number(digits[1]));

  // A separator (-, / or .) between the parts, optionally padded by
  // spaces, or just spaces.
  const sep = String.raw`\s*[-/.]\s*|\s+`;
  const dmy = new RegExp(String.raw`^(\d{1,2})(?:${sep})(\d{1,2})(?:(?:${sep})(\d{4}|\d{2}))?$`).exec(s);
  if (dmy) {
    const year = dmy[3] !== undefined ? fullYear(dmy[3]) : isoParts(today)?.year;
    if (year === undefined) return null;
    return build(year, Number(dmy[2]), Number(dmy[1]));
  }
  return null;
}

/** `15-11-2026`; "" for an empty or invalid date. */
export function formatDutchDate(iso: string): string {
  const p = isoParts(iso);
  return p ? `${pad(p.day)}-${pad(p.month)}-${p.year}` : "";
}

/** Day of the week, 0 = maandag .. 6 = zondag. */
export function weekdayIndex(iso: string): number {
  const d = toLocalDate(iso);
  return d ? (d.getDay() + 6) % 7 : -1;
}

/** `zondag 15 november 2026`; "" for an empty or invalid date. */
export function formatLongDutchDate(iso: string): string {
  const p = isoParts(iso);
  if (!p) return "";
  return `${WEEKDAY_NAMES[weekdayIndex(iso)]} ${p.day} ${MONTH_NAMES[p.month - 1]} ${p.year}`;
}

/** `november 2026` (month 1..12). */
export function formatMonthYear(year: number, month: number): string {
  return `${MONTH_NAMES[month - 1]} ${year}`;
}

/** iso shifted by n days (n may be negative). Invalid input is returned as is. */
export function addDays(iso: string, n: number): string {
  const d = toLocalDate(iso);
  if (!d) return iso;
  // Date normalises overflowing days across months and years, and a local
  // date has no DST problem at day granularity because we never read hours.
  return fromDate(new Date(d.getFullYear(), d.getMonth(), d.getDate() + n));
}

/** iso shifted by n months, the day clamped to the target month
 *  (31 januari + 1 month = 28 or 29 februari). */
export function addMonths(iso: string, n: number): string {
  const p = isoParts(iso);
  if (!p) return iso;
  const index = p.year * 12 + (p.month - 1) + n;
  const year = Math.floor(index / 12);
  const month = index - year * 12 + 1;
  return toIso(year, month, Math.min(p.day, daysInMonth(year, month)));
}

/** The Monday of iso's week. */
export function startOfWeek(iso: string): string {
  const i = weekdayIndex(iso);
  return i < 0 ? iso : addDays(iso, -i);
}

/** The Sunday of iso's week. */
export function endOfWeek(iso: string): string {
  const i = weekdayIndex(iso);
  return i < 0 ? iso : addDays(iso, 6 - i);
}

/**
 * The days of a month view: always 6 rows of 7 (Monday first), so the
 * popover never changes height between months. The first row starts on the
 * Monday on or before the 1st; leading and trailing days of the adjacent
 * months have inMonth false.
 */
export function buildMonthGrid(year: number, month: number): MonthGridCell[][] {
  const first = toIso(year, month, 1);
  let cursor = startOfWeek(first);
  const rows: MonthGridCell[][] = [];
  for (let r = 0; r < 6; r++) {
    const row: MonthGridCell[] = [];
    for (let c = 0; c < 7; c++) {
      const p = isoParts(cursor);
      row.push({ iso: cursor, day: p?.day ?? 0, inMonth: p?.year === year && p?.month === month });
      cursor = addDays(cursor, 1);
    }
    rows.push(row);
  }
  return rows;
}
