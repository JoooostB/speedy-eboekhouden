import type { VATCode } from "../../api/types";

/**
 * Filter and dedupe the BTW codes shown to users when reviewing a supplier
 * invoice. e-Boekhouden returns:
 *   - both purchase (*_INK_*) and sales (*_VERK_*) codes,
 *   - multiple historical / tariff variants of the same display label
 *     (e.g. LAAG_INK_6 + LAAG_INK_6_OLD, both rendering as "Btw laag 6%"),
 *   - duplicate "Btw verlegd 21%" entries from different tariff groups.
 *
 * The dropdown only shows omschrijving + percentage to the user, so two
 * codes with identical visible text are indistinguishable and confusing.
 *
 * Dedupe strategy: prefer canonical codes (the ones Claude's prompt uses
 * — HOOG_INK_21, LAAG_INK_9, VERL_INK, etc.) over legacy / variant codes
 * with the same display label. Without this, when the API returns a
 * legacy code first, the dedupe would drop the canonical one — leaving
 * the BTW picker showing nothing for AI suggestions like VERL_INK
 * (booking still worked because e-Boekhouden accepts the canonical code
 * globally, but the user couldn't see the selection).
 */
export function dedupePurchaseVatCodes(codes: VATCode[]): VATCode[] {
  // Group purchase codes by their (omschrijving, percentage) bucket,
  // then pick the canonical one per bucket. Canonical preference order:
  //   1. Exact match against a known canonical code
  //   2. Shortest code (legacy variants tend to have _OLD / _2 suffixes)
  //   3. Original API order
  const byKey = new Map<string, VATCode[]>();
  for (const c of codes) {
    if (isSalesCode(c.code)) continue;
    const key = `${c.omschrijving}::${c.percentage}`;
    const bucket = byKey.get(key) ?? [];
    bucket.push(c);
    byKey.set(key, bucket);
  }

  const out: VATCode[] = [];
  for (const bucket of byKey.values()) {
    out.push(bucket.sort(byCanonicalPreference)[0]);
  }
  return out;
}

/** The canonical purchase-side codes Claude's invoice prompt uses. Codes
 *  matching this set sort before non-canonical variants in the dedupe so
 *  the picker stays in sync with what the AI returns. */
const canonicalPurchaseCodes = new Set<string>([
  "HOOG_INK_21",
  "LAAG_INK_9",
  "GEEN",
  "VERL_INK",
  "VERL_INK_L9",
  "BU_EU_INK",
  "BI_EU_INK",
]);

/** Sort comparator: canonical codes first, then by code length (shorter
 *  wins — legacy variants tend to have suffixes like _OLD / _2), then
 *  alphabetical for determinism. */
function byCanonicalPreference(a: VATCode, b: VATCode): number {
  const aCanon = canonicalPurchaseCodes.has(a.code) ? 0 : 1;
  const bCanon = canonicalPurchaseCodes.has(b.code) ? 0 : 1;
  if (aCanon !== bCanon) return aCanon - bCanon;
  if (a.code.length !== b.code.length) return a.code.length - b.code.length;
  return a.code.localeCompare(b.code);
}

/** True when the BTW code is a sales (verkoop) variant. Supplier invoices
 *  are always purchases so these get filtered out before deduping. */
export function isSalesCode(code: string): boolean {
  return (
    code.includes("_VERK_") ||
    code.startsWith("HOOG_VERK") ||
    code.startsWith("LAAG_VERK")
  );
}

/** True when the BTW code represents a reverse-charge or intra-/extra-EU
 *  acquisition treatment. The dialog uses this to decide whether to keep
 *  showing the "Verlegde BTW" warning after the user has changed the
 *  dropdown — if they explicitly picked GEEN or a regular HOOG/LAAG code,
 *  the warning would lie about what's about to be booked, so we hide it. */
export function isReverseChargeCode(code: string): boolean {
  // VERL_INK, VERL_INK_L9, BU_EU_INK, BI_EU_INK and any future variants
  // sharing those prefixes. GEEN and the regular tariff codes are excluded.
  return (
    code.startsWith("VERL_") ||
    code.startsWith("BU_EU_") ||
    code.startsWith("BI_EU_")
  );
}
