import type { InvoiceData, LedgerAccount, SplitsAdvies } from "../../api/types";
import { aiSuggestedLedger, type LedgerSource } from "./invoiceLedger";
import { formatAmountInput } from "./reviewAmounts";
import { newRegelKey, toCents, type RegelEdit } from "./regels";

/**
 * Claude's proposed split (InvoiceData.regels + splitsAdvies) as the review
 * dialog opens it: the lines become RegelEdit rows, each code resolved to a
 * ledger account of the administration, with the main suggestion as the
 * fallback for a code that does not exist there. Speedy never books on its
 * own: the dialog opens split, says why, and the user still clicks Boeken
 * or undoes the split.
 */

export interface SplitProposal {
  regels: RegelEdit[];
  advies: SplitsAdvies;
}

/** The proposal of an analysis, or null when it carries none (a plain
 *  document, an older cached analysis) or the lines do not add up to the
 *  total within a cent. `mainLedger` is the booking's single-line ledger
 *  (relation default or Claude's suggestion), used for an unknown code. */
export function proposeRegels(
  analysis: Pick<InvoiceData, "regels" | "splitsAdvies" | "bedragInclBtw">,
  ledgerAccounts: LedgerAccount[],
  mainLedger: LedgerAccount | null,
): SplitProposal | null {
  const regels = analysis.regels ?? [];
  if (regels.length < 2 || !analysis.splitsAdvies?.toelichting?.trim()) return null;
  const sum = regels.reduce((s, r) => s + toCents(r.bedragIncl), 0);
  if (Math.abs(sum - toCents(analysis.bedragInclBtw ?? 0)) > 1) return null;
  if (regels.some((r) => !(r.bedragIncl > 0))) return null;
  return {
    regels: regels.map((r) => ({
      key: newRegelKey(),
      omschrijving: r.omschrijving ?? "",
      ledgerAccount: aiSuggestedLedger(r.grootboekcode, ledgerAccounts) ?? mainLedger,
      btwCode: r.btwCode || "GEEN",
      bedragIncl: formatAmountInput(r.bedragIncl),
    })),
    advies: analysis.splitsAdvies,
  };
}

/** The single-line values the dialog keeps aside while a proposal is
 *  shown, so "Ongedaan maken" restores exactly what the analysis gave. */
export interface SingleLineValues {
  ledgerAccount: LedgerAccount | null;
  ledgerSource: LedgerSource;
  btwCode: string;
  bedragExcl: string;
  bedragIncl: string;
  btwBedrag: string;
}

/** The state after "Ongedaan maken": no lines, no proposal, and the
 *  single-line values as they were before the split. Generic over the
 *  dialog's row type so it stays testable without it. */
export function undoProposal<
  T extends SingleLineValues & { regels: RegelEdit[] | null; proposal: SplitProposal | null; proposalEdited: boolean },
>(
  row: T,
  original: SingleLineValues,
): T {
  return { ...row, ...original, regels: null, proposal: null, proposalEdited: false };
}
