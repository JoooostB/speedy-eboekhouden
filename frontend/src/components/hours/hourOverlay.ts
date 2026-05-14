import type { HourOverviewEntry } from "../../api/types";

/** Aggregated view of previously-booked hours, optimized for the bulk
 *  entry calendar's lookups:
 *   - byDate: total hours per ISO date (YYYY-MM-DD) across all entries
 *     for the calendar badge ("4u al geboekt")
 *   - strictKeys: a Set of "date|employeeName|project|activity" entries
 *     so the pre-submit confirmation can detect exact duplicates in O(1)
 *   - byDateDetails: per-date list of entries for the tooltip
 *
 * Kept pure (no React) so it's straightforward to unit-test — see
 * hourOverlay.test.ts. */
export interface HourOverlay {
  byDate: Map<string, number>;
  byDateDetails: Map<string, HourOverviewEntry[]>;
  strictKeys: Set<string>;
}

/** Normalize the API's ISO datetime ("2026-04-01T00:00:00") to a plain
 *  YYYY-MM-DD date string, which is what the calendar uses as its key. */
export function isoToDate(iso: string): string {
  // Slice the date portion directly — Date parsing would introduce
  // timezone drift for midnight-anchored values near a DST boundary.
  if (iso.length >= 10) return iso.slice(0, 10);
  return iso;
}

/** Build the lookup key used to detect strict duplicates: same employee,
 *  same date, same project label, same activity label. The fields match
 *  what e-boekhouden returns in the overview grid AND what the form
 *  ships when submitting (after we resolve IDs to display strings). */
export function strictMatchKey(args: {
  date: string;
  employeeName: string;
  projectLabel: string;
  activityLabel: string;
}): string {
  return [args.date, args.employeeName, args.projectLabel, args.activityLabel]
    .map((s) => s.trim().toLowerCase())
    .join("|");
}

/** Build an aggregated overlay from the raw API response entries. */
export function buildHourOverlay(entries: HourOverviewEntry[]): HourOverlay {
  const byDate = new Map<string, number>();
  const byDateDetails = new Map<string, HourOverviewEntry[]>();
  const strictKeys = new Set<string>();

  for (const e of entries) {
    const date = isoToDate(e.datum);
    byDate.set(date, (byDate.get(date) ?? 0) + (e.aantalUren ?? 0));
    const existing = byDateDetails.get(date) ?? [];
    existing.push(e);
    byDateDetails.set(date, existing);
    strictKeys.add(
      strictMatchKey({
        date,
        employeeName: e.medewerker,
        projectLabel: e.project,
        activityLabel: e.activiteit,
      }),
    );
  }

  return { byDate, byDateDetails, strictKeys };
}

/** Format a number of hours for display ("8u", "4,5u"). Strips trailing
 *  ".00" so the common 8-hour case stays compact. */
export function formatHours(hours: number): string {
  if (!Number.isFinite(hours)) return "0u";
  const rounded = Math.round(hours * 100) / 100;
  if (Number.isInteger(rounded)) return `${rounded}u`;
  return `${rounded.toString().replace(".", ",")}u`;
}
