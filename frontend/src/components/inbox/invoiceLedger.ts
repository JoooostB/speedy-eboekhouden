import type { LedgerAccount } from "../../api/types";

/** Where the tegenrekening in the review dialog came from. "relation" is
 *  the supplier relation's default ledger account in e-Boekhouden, "ai" is
 *  Claude's grootboekcode, "manual" is the user's own pick. */
export type LedgerSource = "relation" | "ai" | "manual" | null;

/** Categories that can never be the tegenrekening of a purchase invoice:
 *  a relation whose default points at the crediteuren account (or a bank
 *  account) says nothing about the cost account. */
const NON_COST_CATEGORIES = new Set(["CRED", "DEB", "BANK", "KAS"]);

/** The relation's default ledger account as a tegenrekening, or null when
 *  it has none, it is not among the active accounts, or it is not a
 *  sensible counter account. `grootboekrekeningId` is e-Boekhouden's
 *  internal id, the same id as LedgerAccount.id. */
export function relationDefaultLedger(
  relation: { grootboekrekeningId?: number | null } | null | undefined,
  ledgerAccounts: LedgerAccount[],
): LedgerAccount | null {
  const id = relation?.grootboekrekeningId;
  if (!id || id <= 0) return null;
  const account = ledgerAccounts.find((la) => la.id === id);
  if (!account || NON_COST_CATEGORIES.has(account.rekeningCategorie.toUpperCase())) return null;
  return account;
}

/** Claude's suggested grootboekcode resolved against the active accounts
 *  (by code, or by description when Claude answered with a name). */
export function aiSuggestedLedger(grootboekcode: string | undefined, ledgerAccounts: LedgerAccount[]): LedgerAccount | null {
  if (!grootboekcode) return null;
  const wanted = grootboekcode.toLowerCase();
  return ledgerAccounts.find((la) => la.code === grootboekcode || la.omschrijving.toLowerCase() === wanted) ?? null;
}

/** The tegenrekening a freshly analysed invoice starts with: the matched
 *  relation's default wins over Claude's suggestion, because it is what
 *  the user configured for this supplier. */
export function initialLedger(
  matchedRelation: { grootboekrekeningId?: number | null } | null | undefined,
  grootboekcode: string | undefined,
  ledgerAccounts: LedgerAccount[],
): { account: LedgerAccount | null; source: LedgerSource } {
  const fromRelation = relationDefaultLedger(matchedRelation, ledgerAccounts);
  if (fromRelation) return { account: fromRelation, source: "relation" };
  const fromAi = aiSuggestedLedger(grootboekcode, ledgerAccounts);
  return fromAi ? { account: fromAi, source: "ai" } : { account: null, source: null };
}

/** The ledger state after the user picks another relation. A relation with
 *  a default takes over, unless the user chose the account by hand; a
 *  relation without one leaves the current account alone. */
export function ledgerAfterRelationChange(
  current: { account: LedgerAccount | null; source: LedgerSource },
  relation: { grootboekrekeningId?: number | null } | null,
  ledgerAccounts: LedgerAccount[],
): { account: LedgerAccount | null; source: LedgerSource } {
  if (current.source === "manual") return current;
  const fromRelation = relationDefaultLedger(relation, ledgerAccounts);
  if (fromRelation) return { account: fromRelation, source: "relation" };
  // The previous relation's default no longer applies to this relation,
  // but the account itself is still a valid pick: keep it without a source,
  // so no caption names the wrong relation and a next pick can still apply.
  return current.source === "relation" ? { account: current.account, source: null } : current;
}
