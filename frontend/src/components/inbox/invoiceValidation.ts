import { CREDITNOTA_NOT_BOOKABLE } from "../../hooks/mailDocumentsModel";

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
  /** True when the analysis says the document is a credit note, which
   *  Speedy cannot book yet (in either mode). */
  isCreditnota?: boolean;
  /** False when an amount does not read as an amount (parseAmount) or the
   *  total is not above zero; such a row would book a wrong amount. */
  amountsValid?: boolean;
  /** For a split booking: why its lines cannot be booked yet (validateRegels
   *  in regels.ts), null when they add up. Undefined for a single line. */
  splitBlocker?: string | null;
  /** False when the date is empty or not a real date (isValidIso). */
  hasDatum?: boolean;
  /** True when another row in the same review links the same bank line
   *  (holdsSharedBankLine in bankLineAssignment.ts); booking both would pay
   *  one bank line twice. */
  sharesBankLine?: boolean;
}

export const AMOUNT_BLOCKER = "Bedrag ontbreekt of klopt niet";
export const SHARED_BANK_LINE_BLOCKER = "Afschriftregel is ook aan een ander document gekoppeld";
export const DATE_BLOCKER = "Geen datum";

export function invoiceBlocker(inv: InvoiceValidationInput): string | null {
  if (inv.isCreditnota) return CREDITNOTA_NOT_BOOKABLE;
  if (inv.amountsValid === false) return AMOUNT_BLOCKER;
  if (inv.splitBlocker) return inv.splitBlocker;
  if (!inv.hasLedgerAccount) return "Geen tegenrekening";
  if (inv.sharesBankLine) return SHARED_BANK_LINE_BLOCKER;
  if (inv.isReceipt && !inv.hasImportId) return "Geen afschriftregel gekoppeld";
  if (!inv.isReceipt && !inv.hasRelation) return "Geen relatie";
  if (inv.hasDatum === false) return DATE_BLOCKER;
  return null;
}
