/**
 * Per-row validation for the invoice review batch. Returns the Dutch reason
 * a row can't be booked yet, or null when the row is ready.
 *
 * Used in two places:
 *   1. The submit loop, to skip-instead-of-block incomplete rows.
 *   2. The action bar, to count blockers and adapt the button label
 *      ("Boek 11 van 15") and the inline hint.
 *
 * Kept in its own file (vs inline in the dialog) so it's straightforward
 * to unit-test without spinning up the React tree. See
 * invoiceValidation.test.ts for the regression tests covering the
 * factuur-vs-bonnetje branching.
 */
export interface InvoiceValidationInput {
  /** Whether a tegenrekening / grootboekrekening has been selected. */
  hasLedgerAccount: boolean;
  /** Whether a relation has been selected (only required for factuur mode). */
  hasRelation: boolean;
  /** Whether a bank line has been linked (only required for bonnetje mode). */
  hasImportId: boolean;
  /** True when the user picked the Bonnetje toggle for this row. */
  isReceipt: boolean;
}

export function invoiceBlocker(inv: InvoiceValidationInput): string | null {
  if (!inv.hasLedgerAccount) return "Geen tegenrekening";
  if (inv.isReceipt && !inv.hasImportId) return "Geen afschriftregel gekoppeld";
  if (!inv.isReceipt && !inv.hasRelation) return "Geen relatie";
  return null;
}
