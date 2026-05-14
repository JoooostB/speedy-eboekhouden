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
 * This helper keeps the first occurrence per (omschrijving, percentage)
 * AFTER stripping sales codes, so the user sees one option per label.
 */
export function dedupePurchaseVatCodes(codes: VATCode[]): VATCode[] {
  const seen = new Set<string>();
  const out: VATCode[] = [];
  for (const c of codes) {
    if (isSalesCode(c.code)) continue;
    const key = `${c.omschrijving}::${c.percentage}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(c);
  }
  return out;
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
