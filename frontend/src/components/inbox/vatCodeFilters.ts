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
