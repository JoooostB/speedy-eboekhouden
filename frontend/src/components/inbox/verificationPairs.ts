import type { InboxClassification, InboxProcessResult, PaymentGroup } from "../../api/types";
import { formatMoney } from "../../hooks/mailDocumentsModel";

/**
 * Pure rules of a verification pair: a card check (−€ 1,00) and its refund
 * (+€ 1,00) that the backend linked through pairLineId. The two lines leave
 * the flat list and become one row (VerificationPairRow) that books both on
 * Bijzondere baten at once through one confirm dialog, whose state lives in
 * BookkeepingInbox so it survives the booked leg leaving the list.
 */

export interface VerificationPair {
  /** "vp-{lower id}-{higher id}", stable across renders. */
  id: string;
  /** The charge first, then the refund (by sign; by id when equal). */
  lines: [InboxClassification, InboxClassification];
}

/** Pairs of unprocessed lines that point at each other. A line whose
 *  partner is no longer in the list stays a loose row; a line in a payment
 *  group renders with the group, never in a pair. */
export function buildVerificationPairs(items: ReadonlyArray<InboxClassification>, groups: ReadonlyArray<PaymentGroup> = []): VerificationPair[] {
  const grouped = new Set(groups.flatMap((g) => g.lineIds));
  const byId = new Map(items.filter((i) => !i.paymentGroupId && !grouped.has(i.id)).map((i) => [i.id, i]));
  const placed = new Set<number>();
  const out: VerificationPair[] = [];
  for (const item of byId.values()) {
    const partnerId = item.verification?.pairLineId;
    if (!partnerId || placed.has(item.id)) continue;
    const partner = byId.get(partnerId);
    if (!partner || partner.verification?.pairLineId !== item.id) continue;
    placed.add(item.id);
    placed.add(partnerId);
    const [a, b] = item.bedrag < 0 || (item.bedrag === partner.bedrag && item.id < partnerId) ? [item, partner] : [partner, item];
    out.push({ id: pairId(item.id, partnerId), lines: [a, b] });
  }
  return out;
}

export function pairId(a: number, b: number): string {
  return `vp-${Math.min(a, b)}-${Math.max(a, b)}`;
}

/** The pairs with at least one leg in the filtered list, and that list with
 *  the other leg added right after it, so a pair shows whole whenever either
 *  leg matches the category filter. */
export function withPairPartners(
  visible: ReadonlyArray<InboxClassification>,
  pairs: ReadonlyArray<VerificationPair>,
): { items: InboxClassification[]; pairs: VerificationPair[] } {
  const visibleIds = new Set(visible.map((i) => i.id));
  const shown = pairs.filter((p) => p.lines.some((l) => visibleIds.has(l.id)));
  const partnerOf = new Map<number, InboxClassification>();
  for (const p of shown) {
    const [a, b] = p.lines;
    if (!visibleIds.has(a.id)) partnerOf.set(b.id, a);
    if (!visibleIds.has(b.id)) partnerOf.set(a.id, b);
  }
  const items: InboxClassification[] = [];
  for (const item of visible) {
    items.push(item);
    const partner = partnerOf.get(item.id);
    if (partner) items.push(partner);
  }
  return { items, pairs: shown };
}

/** "−€ 1,00 en +€ 1,00 heffen elkaar op" */
export function pairSummaryLine(lines: ReadonlyArray<Pick<InboxClassification, "bedrag">>): string {
  const [a, b] = lines;
  return `${signed(a.bedrag)} en ${signed(b.bedrag)} heffen elkaar op`;
}

function signed(bedrag: number): string {
  return `${bedrag < 0 ? "−" : "+"}${formatMoney(Math.abs(bedrag))}`;
}

/** Net amount of the pair (normally € 0,00). */
export function pairTotal(lines: ReadonlyArray<Pick<InboxClassification, "bedrag">>): number {
  return lines.reduce((acc, l) => acc + Math.round(l.bedrag * 100), 0) / 100;
}

/** "Boeken (2)", or "Opnieuw proberen (1)" after a partial failure. */
export function pairButtonLabel(count: number, retry: boolean): string {
  return retry ? `Opnieuw proberen (${count})` : `Boeken (${count})`;
}

/** Accessible name of that button; ledger is "9300 Bijzondere baten" or "". */
export function boekenPairLabel(count: number, retry: boolean, ledger: string): string {
  const op = ledger ? ` op ${ledger}` : "";
  const what = count === 1 ? "de controlebetaling" : `${count} controlebetalingen`;
  return retry ? `Probeer ${what} opnieuw te boeken${op}` : `Boek ${what}${op}`;
}

/** The lines of a pair whose booking failed, in the pair's order. */
export function failedPairLines(
  lines: ReadonlyArray<InboxClassification>,
  results: ReadonlyMap<number, InboxProcessResult>,
): InboxClassification[] {
  return lines.filter((l) => results.get(l.id)?.status === "error");
}

/** The alert in the dialog after a booking that did not fully succeed;
 *  null when every line was booked. */
export function pairResultAlert(edited: ReadonlyArray<InboxClassification>, results: ReadonlyMap<number, InboxProcessResult>): string | null {
  const failed = failedPairLines(edited, results);
  if (failed.length === 0) return null;
  const reason = results.get(failed[0].id)?.error || "onbekende fout";
  if (failed.length === edited.length) return `Boeken mislukt: ${reason}`;
  return `${edited.length - failed.length} van de ${edited.length} controlebetalingen geboekt, ${failed.length} mislukt: ${reason}.`;
}

/** Shown when the request itself failed, never the raw error. */
export const PAIR_REQUEST_FAILED = "Boeken mislukt";
