import type { InvoiceSubmitFullResponse } from "../../api/types";

/**
 * Pure rules for the result line of a booking in the review dialog: which
 * warnings a submit answer carries and whether the booking stands. No React
 * and no API calls, so it is unit-tested.
 */

/** The archive warning for an upload that should have been archived but
 *  was not, per document kind. */
export const ARCHIVE_MISSING_INVOICE =
  "De factuur is geboekt, maar het bestand kon niet naar je digitale archief. Upload het zelf in e-Boekhouden; boek het niet opnieuw.";
export const ARCHIVE_MISSING_RECEIPT =
  "Het bonnetje is geboekt, maar het bestand kon niet naar je digitale archief. Upload het zelf in e-Boekhouden; boek het niet opnieuw.";

export type SubmitOutcomeToken = "payment_warning" | "archive_warning" | "payment_and_archive_warning";

export interface SubmitOutcome {
  status: "ok" | "warning";
  /** Every warning of the answer, the payment first; empty when "ok". */
  warnings: string[];
  /** Analytics token for a warning, undefined when "ok". */
  outcome?: SubmitOutcomeToken;
}

export interface SubmitOutcomeInput {
  isReceipt: boolean;
  /** An upload was inlined with a folder picked, so the backend should
   *  have archived it. A mail-folder document never sets this: its archive
   *  side is reported through archiveWarning. */
  archiveExpected: boolean;
  /** The submit answer; a receipt answer has no paymentWarning. */
  resp: Pick<InvoiceSubmitFullResponse, "archived" | "archiveWarning" | "paymentWarning">;
}

/**
 * Collects the warnings of a submit answer. A booking can carry a payment
 * warning (invoice booked, payment not) and an archive warning (document not
 * linked, renamed or moved, or an upload that did not reach the archive) at
 * the same time; both are shown, because each asks the user to do something
 * different in e-Boekhouden.
 */
export function submitOutcome({ isReceipt, archiveExpected, resp }: SubmitOutcomeInput): SubmitOutcome {
  const payment = resp.paymentWarning?.trim() ? resp.paymentWarning : undefined;
  let archive = resp.archiveWarning?.trim() ? resp.archiveWarning : undefined;
  if (!archive && archiveExpected && resp.archived === false) {
    archive = isReceipt ? ARCHIVE_MISSING_RECEIPT : ARCHIVE_MISSING_INVOICE;
  }

  const warnings = [payment, archive].filter((w): w is string => !!w);
  if (warnings.length === 0) return { status: "ok", warnings };
  const outcome: SubmitOutcomeToken =
    payment && archive ? "payment_and_archive_warning" : payment ? "payment_warning" : "archive_warning";
  return { status: "warning", warnings, outcome };
}

/** Whether a result line stands for a booking that exists: a booking with a
 *  warning is booked too, so the lists behind the dialog must reload. */
export function isBooked(status: "ok" | "error" | "skipped" | "warning"): boolean {
  return status === "ok" || status === "warning";
}
