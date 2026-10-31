import type { LedgerAccount, RegelPayload } from "../../api/types";
import { formatMoney } from "../../hooks/mailDocumentsModel";
import { formatAmountInput, parseAmount } from "./reviewAmounts";

/**
 * A split booking in the review dialog: one mutation with several regels,
 * each on its own tegenrekening with its own BTW code and gross amount. The
 * rules here are pure and mirror the backend's regels.go: amounts are
 * compared in cents, the BTW of a line is rounded per line the way the
 * e-boekhouden web UI does it (excl = round(incl / (1 + rate)), btw = incl −
 * excl, in integer cents), and the lines must add up to the total before a
 * booking is sent.
 */

/** The most lines one booking may have; the backend refuses more. */
export const MAX_REGELS = 10;

/** One line as the editor holds it. `key` is stable for the life of the
 *  line (React keys, focus after add/delete); the amount is text while
 *  editing ("12,50"), read with parseAmount. */
export interface RegelEdit {
  key: string;
  omschrijving: string;
  ledgerAccount: LedgerAccount | null;
  btwCode: string;
  bedragIncl: string;
}

let regelKeySeq = 0;

/** A fresh key for a new line. */
export function newRegelKey(): string {
  regelKeySeq += 1;
  return `regel-${regelKeySeq}`;
}

/** An amount as whole cents, so sums are exact. */
export function toCents(amount: number): number {
  return Math.round(amount * 100);
}

/** The gross amount of a line in cents, 0 while the field is empty or not
 *  an amount. */
export function regelCents(regel: Pick<RegelEdit, "bedragIncl">): number {
  const amount = parseAmount(regel.bedragIncl);
  return amount === null || amount <= 0 ? 0 : toCents(amount);
}

/** The BTW split of one line, per line like the e-boekhouden UI: the net
 *  amount is rounded first (half away from zero) and the BTW is what is
 *  left, so excl + btw == incl holds for every line. Integer arithmetic in
 *  basis points, the same rule as Go's regelExclCents, so both sides agree
 *  for every amount: 3580 at 9 % gives 3284 + 296. */
export function regelBtwCents(inclCents: number, rate: number): { exclCents: number; btwCents: number } {
  if (inclCents <= 0 || rate <= 0) return { exclCents: inclCents, btwCents: 0 };
  const denom = 10000 + Math.round(rate * 100);
  const exclCents = Math.floor((2 * inclCents * 10000 + denom) / (2 * denom));
  return { exclCents, btwCents: inclCents - exclCents };
}

/** What is left of the total after the lines: positive while there is
 *  still something to divide, negative when the lines exceed it. */
export function remainderCents(regels: readonly Pick<RegelEdit, "bedragIncl">[], totalCents: number): number {
  return totalCents - regels.reduce((sum, r) => sum + regelCents(r), 0);
}

/** The totals of a split booking as the submit payload needs them next to
 *  the lines: the BTW is the sum of the per-line BTW, the net amount is the
 *  total minus that BTW. */
export function regelsTotals(
  regels: readonly RegelEdit[],
  totalCents: number,
  vatRate: (code: string) => number,
): { bedragIncl: number; bedragExcl: number; btwBedrag: number } {
  const btwCents = regels.reduce((sum, r) => sum + regelBtwCents(regelCents(r), vatRate(r.btwCode)).btwCents, 0);
  return { bedragIncl: totalCents / 100, bedragExcl: (totalCents - btwCents) / 100, btwBedrag: btwCents / 100 };
}

/** The lines a single-line booking turns into when the user splits it: the
 *  current values become line 1, line 2 starts empty with the same BTW code
 *  so the common case (same rate, other ledger) is one pick and one amount. */
export function splitFromSingle(single: Pick<RegelEdit, "ledgerAccount" | "btwCode" | "bedragIncl">): RegelEdit[] {
  return [
    { key: newRegelKey(), omschrijving: "", ledgerAccount: single.ledgerAccount, btwCode: single.btwCode, bedragIncl: single.bedragIncl },
    { key: newRegelKey(), omschrijving: "", ledgerAccount: null, btwCode: single.btwCode, bedragIncl: "" },
  ];
}

/** The lines of the "Splits in fooi" shortcut: the receipt as line 1 and the
 *  difference with the bank line as a tip without BTW on the same ledger. */
export function splitWithTip(
  single: Pick<RegelEdit, "ledgerAccount" | "btwCode" | "bedragIncl">,
  tipCents: number,
): RegelEdit[] {
  return [
    { key: newRegelKey(), omschrijving: "", ledgerAccount: single.ledgerAccount, btwCode: single.btwCode, bedragIncl: single.bedragIncl },
    {
      key: newRegelKey(),
      omschrijving: "Fooi",
      ledgerAccount: single.ledgerAccount,
      btwCode: "GEEN",
      bedragIncl: formatAmountInput(tipCents / 100),
    },
  ];
}

/** Appends a line that takes what is still to divide, with the BTW code of
 *  the line above it. Returns the lines unchanged at the maximum. */
export function addRegel(regels: readonly RegelEdit[], totalCents: number): RegelEdit[] {
  if (regels.length >= MAX_REGELS) return [...regels];
  const rest = remainderCents(regels, totalCents);
  const last = regels[regels.length - 1];
  return [
    ...regels,
    {
      key: newRegelKey(),
      omschrijving: "",
      ledgerAccount: null,
      btwCode: last?.btwCode ?? "GEEN",
      bedragIncl: rest > 0 ? formatAmountInput(rest / 100) : "",
    },
  ];
}

/** Where focus goes after line `deletedIndex` was removed from a list that
 *  now has `lengthAfter` lines: the line that took its place, or the new
 *  last line when the last one was removed; null when nothing is left. */
export function focusAfterDelete(deletedIndex: number, lengthAfter: number): number | null {
  if (lengthAfter <= 0) return null;
  return Math.min(deletedIndex, lengthAfter - 1);
}

/** The Dutch reason the lines cannot be booked yet, or null when they can:
 *  every line needs an amount above zero and a tegenrekening, and together
 *  they must add up to the total. */
export function validateRegels(regels: readonly RegelEdit[], totalCents: number): string | null {
  if (regels.length < 2) return "Een splitsing heeft minstens twee regels nodig";
  if (regels.length > MAX_REGELS) return `Een boeking kan maximaal ${MAX_REGELS} regels hebben`;
  for (let i = 0; i < regels.length; i++) {
    const r = regels[i];
    const amount = parseAmount(r.bedragIncl);
    if (amount === null || amount <= 0) return `Regel ${i + 1}: vul een bedrag in`;
    if (!r.ledgerAccount) return `Regel ${i + 1}: kies een tegenrekening`;
  }
  const rest = remainderCents(regels, totalCents);
  if (rest > 0) return `Nog ${formatMoney(rest / 100)} te verdelen`;
  if (rest < 0) return `${formatMoney(-rest / 100)} te veel`;
  return null;
}

/** The live status line under the editor. */
export function remainderStatus(rest: number): string {
  if (rest > 0) return `Nog te verdelen: ${formatMoney(rest / 100)}`;
  if (rest < 0) return `${formatMoney(-rest / 100)} te veel`;
  return "Verdeeld";
}

/** The lines as the backend takes them. "id" sends the tegenrekening's
 *  internal id (the review dialog), "code" its grootboekcode (the bank-line
 *  split, resolved by the backend). Call after validateRegels. */
export function toRegelPayload(regels: readonly RegelEdit[], mode: "id" | "code" = "id"): RegelPayload[] {
  return regels.map((r) => ({
    ...(r.omschrijving.trim() ? { omschrijving: r.omschrijving.trim() } : {}),
    ...(mode === "id" ? { tegenRekeningId: r.ledgerAccount?.id ?? 0 } : { grootboekcode: r.ledgerAccount?.code ?? "" }),
    btwCode: r.btwCode,
    bedragIncl: regelCents(r) / 100,
  }));
}
