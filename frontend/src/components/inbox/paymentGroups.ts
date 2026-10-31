import type {
  InboxClassification,
  InboxProcessItem,
  InboxProcessResult,
  OpenPostGridItem,
  PaymentGroup,
  PaymentGroupCandidate,
  PaymentGroupPost,
  PaymentGroupState,
} from "../../api/types";
import { formatMoney, parseDate } from "../../hooks/mailDocumentsModel";
import { toInboxProcessItem } from "./inboxBooking";
import type { VerificationPair } from "./verificationPairs";

/**
 * Pure rules of a payment group: several incoming bank lines that together
 * pay one sales invoice. Shared by PaymentGroupRow, PaymentGroupDialog and
 * BookkeepingInbox so the row, the dialog and the snackbar agree on sums,
 * states and copy. Amounts are compared in whole cents, like the backend.
 */

/** The invoice a group is booked on. It comes from the group itself, from a
 *  chosen candidate of an ambiguous group, or from an open post the user
 *  picked by hand. */
export type GroupInvoice = PaymentGroupPost;

/** How the included lines relate to what is outstanding. */
export type FitState = "complete" | "partial" | "over";

/** One cent off still fits (the backend's openPostAmountToleranceCents). */
const TOLERANCE_CENTS = 2;

function cents(v: number): number {
  return Math.round(v * 100);
}

export function sumLines(lines: ReadonlyArray<{ bedrag: number }>): number {
  return lines.reduce((acc, l) => acc + cents(Math.abs(l.bedrag)), 0) / 100;
}

/** What stays open after the lines are booked; negative when they pay too much. */
export function remainder(invoice: Pick<GroupInvoice, "teOntvangen">, sum: number): number {
  return (cents(invoice.teOntvangen) - cents(sum)) / 100;
}

export function fitState(invoice: Pick<GroupInvoice, "teOntvangen">, sum: number): FitState {
  const diff = cents(invoice.teOntvangen) - cents(sum);
  if (Math.abs(diff) < TOLERANCE_CENTS) return "complete";
  return diff > 0 ? "partial" : "over";
}

export function isOverpayment(invoice: Pick<GroupInvoice, "teOntvangen">, sum: number): boolean {
  return fitState(invoice, sum) === "over";
}

export function invoiceFromOpenPost(p: OpenPostGridItem): GroupInvoice {
  return {
    relatieId: p.relatieId,
    relatieNaam: p.bedrijf,
    factuurnummer: p.mutFactuur,
    factuurdatum: p.mutDatum,
    sBedrag: p.sBedrag,
    sBetaald: p.sBetaald,
    teOntvangen: p.teOntvangen,
  };
}

export function invoiceFromCandidate(c: PaymentGroupCandidate): GroupInvoice {
  const { lineIds: _lineIds, ...post } = c;
  return post;
}

/** The invoice of a complete or partial group; null for an ambiguous one. */
export function groupInvoice(g: PaymentGroup): GroupInvoice | null {
  if (g.state === "ambiguous") return null;
  return {
    relatieId: g.relatieId,
    relatieNaam: g.relatieNaam,
    factuurnummer: g.factuurnummer,
    factuurdatum: g.factuurdatum,
    sBedrag: g.sBedrag,
    sBetaald: g.sBetaald,
    teOntvangen: g.teOntvangen,
  };
}

// Copy

export const GROUP_CHIP_LABELS: Record<PaymentGroupState, string> = {
  complete: "Betaling in delen",
  partial: "Deelbetaling",
  ambiguous: "Kies factuur",
};

export const GROUP_CHIP_TOOLTIPS: Record<PaymentGroupState, string> = {
  complete:
    "Deze betalingen zijn samen precies het openstaande bedrag van één factuur. Met Koppelen boek je ze allemaal op die factuur.",
  partial:
    "Deze betalingen horen bij één factuur, maar dekken nog niet het hele bedrag. Het restant blijft openstaan in e-Boekhouden.",
  ambiguous: "Deze betalingen passen bij meer dan één openstaande factuur. Kies zelf welke het is.",
};

export function betalingen(n: number): string {
  return n === 1 ? "1 betaling" : `${n} betalingen`;
}

/** Header of a group row: "Voorbeeld Bouw · factuur VF-2026-0017", or for an
 *  ambiguous group the relation when all candidates share one. */
export function groupTitle(g: PaymentGroup): string {
  if (g.state !== "ambiguous") {
    return `${g.relatieNaam || "Onbekende relatie"} · factuur ${g.factuurnummer || "?"}`;
  }
  const names = new Set(g.candidates.map((c) => c.relatieNaam.trim()).filter(Boolean));
  return names.size === 1 ? [...names][0] : "Meerdere relaties";
}

/** "3 betalingen · € 14.520,00 van € 14.520,00 · factuur volledig betaald"
 *  / "… · nog open € 3.720,00"; for an ambiguous group "Past bij 2
 *  openstaande facturen van Voorbeeld Bouw". */
export function groupSummaryLine(g: PaymentGroup, count = g.lineIds.length): string {
  if (g.state === "ambiguous") {
    const names = new Set(g.candidates.map((c) => c.relatieNaam.trim()).filter(Boolean));
    const van = names.size === 1 ? ` van ${[...names][0]}` : "";
    return `Past bij ${g.candidates.length} openstaande facturen${van}`;
  }
  const rest = remainder(g, g.total);
  const tail = fitState(g, g.total) === "complete" ? "factuur volledig betaald" : `nog open ${formatMoney(rest)}`;
  return `${betalingen(count)} · ${formatMoney(g.total)} van ${formatMoney(g.teOntvangen)} · ${tail}`;
}

export function groupButtonLabel(g: PaymentGroup): string {
  return g.state === "ambiguous" ? "Kiezen" : `Koppelen (${g.lineIds.length})`;
}

/** Percentage of the outstanding amount the group covers, for the bar. */
export function groupProgress(g: PaymentGroup): number {
  if (g.state === "ambiguous" || !(g.teOntvangen > 0)) return 0;
  return Math.max(0, Math.min(100, Math.round((g.total / g.teOntvangen) * 100)));
}

export type DialogMode = PaymentGroupState | "manual";

export const DIALOG_TITLES: Record<DialogMode, string> = {
  complete: "Betaling in delen koppelen",
  partial: "Deelbetaling koppelen",
  ambiguous: "Kies de factuur",
  manual: "Koppelen aan openstaande factuur",
};

export const DIALOG_INTRO =
  "Je boekt elke betaling apart als factuurbetaling op dezelfde factuur, net zoals in e-Boekhouden.";

/** The sticky summary of the dialog. blocking is true when the lines pay
 *  more than is outstanding, which the primary button must refuse. */
export function summaryStatus(invoice: GroupInvoice | null, sum: number): { text: string; blocking: boolean } {
  if (!invoice) return { text: `Samen ${formatMoney(sum)}`, blocking: false };
  const head = `Samen ${formatMoney(sum)} van ${formatMoney(invoice.teOntvangen)} open`;
  switch (fitState(invoice, sum)) {
    case "complete":
      return { text: `${head}. Na koppelen is de factuur volledig betaald.`, blocking: false };
    case "partial":
      return { text: `${head}. Na koppelen blijft ${formatMoney(remainder(invoice, sum))} open.`, blocking: false };
    case "over":
      return {
        text: `Samen ${formatMoney(sum)}, dat is ${formatMoney(-remainder(invoice, sum))} meer dan er openstaat. Haal een betaling weg of kies een andere factuur.`,
        blocking: true,
      };
  }
}

export function primaryLabel(count: number, retry: boolean): string {
  if (retry) return `Opnieuw proberen (${count})`;
  return count === 1 ? "Betaling koppelen" : `${count} betalingen koppelen`;
}

/** Why the primary button is disabled, or null when it is not. */
export function primaryBlocker(invoice: GroupInvoice | null, count: number, sum: number): string | null {
  if (!invoice) return "Kies eerst een factuur.";
  if (count === 0) return "Selecteer minstens één betaling.";
  if (isOverpayment(invoice, sum)) return summaryStatus(invoice, sum).text;
  return null;
}

/** Accessible name of a line's include checkbox. */
export function includeLabel(line: InboxClassification, accountLabel: string): string {
  const op = accountLabel ? ` op ${accountLabel}` : "";
  return `Betaling van ${formatLineDate(line.datum)}, ${formatMoney(line.bedrag)}${op} meenemen`;
}

/** "20 okt 2026" from the grid's "2026-10-20T00:00:00". */
export function formatLineDate(s: string): string {
  const d = parseDate(s);
  if (!d) return s;
  return new Intl.DateTimeFormat("nl-NL", { day: "numeric", month: "short", year: "numeric" }).format(d).replace(/\./g, "");
}

/** "1022 G-rekening" for a line's own bank account. */
export function accountLabel(line: Pick<InboxClassification, "grootboekId">, accounts: ReadonlyArray<{ id: number; code: string; omschrijving: string }>): string {
  const acc = accounts.find((a) => a.id === line.grootboekId);
  return acc ? `${acc.code} ${acc.omschrijving}`.trim() : "";
}

export function candidateFitLabel(c: PaymentGroupCandidate, lines: ReadonlyArray<InboxClassification>): "Past precies" | "Past deels" {
  const sum = sumLines(lines.filter((l) => c.lineIds.includes(l.id)));
  return fitState(c, sum) === "complete" ? "Past precies" : "Past deels";
}

// Booking

/** One factuurbetaling per line, all on the same invoice. */
export function buildGroupPayload(lines: ReadonlyArray<InboxClassification>, invoice: GroupInvoice): InboxProcessItem[] {
  return lines.map((line) =>
    toInboxProcessItem(
      { ...line, soort: "FactuurbetalingOntvangen", factuurnummer: invoice.factuurnummer, btwCode: "GEEN", grootboekcode: "" },
      invoice.relatieId,
    ),
  );
}

/** The lines whose booking failed, in the order given. */
export function failedLines(lines: ReadonlyArray<InboxClassification>, results: ReadonlyMap<number, InboxProcessResult>): InboxClassification[] {
  return lines.filter((l) => results.get(l.id)?.status === "error");
}

export function countResults(results: ReadonlyMap<number, InboxProcessResult>): { ok: number; failed: number } {
  let ok = 0;
  let failed = 0;
  results.forEach((r) => (r.status === "ok" ? ok++ : failed++));
  return { ok, failed };
}

/** The alert after a booking that did not fully succeed. */
export function resultAlert(results: ReadonlyMap<number, InboxProcessResult>): string | null {
  const { ok, failed } = countResults(results);
  if (failed === 0) return null;
  const reason = [...results.values()].find((r) => r.status === "error")?.error || "onbekende fout";
  if (ok === 0) return `Koppelen mislukt: ${reason}. Er is niets geboekt.`;
  return `${ok} van de ${ok + failed} betalingen zijn geboekt. ${failed} niet: ${reason}. De geboekte betalingen blijven staan.`;
}

/** Per-line status after a booking attempt. */
export function lineStatus(result: InboxProcessResult | undefined): string | null {
  if (!result) return null;
  return result.status === "ok" ? "Geboekt" : `Niet geboekt: ${result.error || "onbekende fout"}`;
}

/** "3 betalingen gekoppeld aan factuur VF-2026-0017. De factuur is volledig
 *  betaald." / "… Nog open: € 3.720,00." */
export function snackbarMessage(count: number, invoice: GroupInvoice, bookedSum: number): string {
  const head = `${betalingen(count)} gekoppeld aan factuur ${invoice.factuurnummer}.`;
  const rest = remainder(invoice, bookedSum);
  return fitState(invoice, bookedSum) === "partial" ? `${head} Nog open: ${formatMoney(rest)}.` : `${head} De factuur is volledig betaald.`;
}

// Picking lines and invoices

/** Other unprocessed incoming lines that could be added to the group:
 *  dated on or after the invoice date (when there is one). */
export function addableLines(
  all: ReadonlyArray<InboxClassification>,
  current: ReadonlySet<number>,
  invoice: GroupInvoice | null,
): InboxClassification[] {
  const since = invoice ? parseDate(invoice.factuurdatum) : null;
  return all.filter((l) => {
    if (l.bedrag <= 0 || current.has(l.id)) return false;
    if (!since) return true;
    const d = parseDate(l.datum);
    return !d || d.getTime() >= since.getTime();
  });
}

/** Filters lines on an amount ("1.200", "1200,00") or a description word. */
export function filterAddable(lines: ReadonlyArray<InboxClassification>, query: string): InboxClassification[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...lines];
  const digits = q.replace(/[^\d,.]/g, "");
  return lines.filter((l) => {
    if (l.omschrijving.toLowerCase().includes(q)) return true;
    if (!digits) return false;
    const amount = formatMoney(l.bedrag).toLowerCase();
    return amount.includes(digits) || amount.replace(/\./g, "").includes(digits.replace(/\./g, ""));
  });
}

/** Open sales invoices for the manual picker: the ones whose company name
 *  occurs in a line's description first, then by date, newest first. */
export function sortOpenPostsForPicker(posts: ReadonlyArray<OpenPostGridItem>, lines: ReadonlyArray<InboxClassification>): OpenPostGridItem[] {
  const words = new Set(lines.flatMap((l) => tokens(l.omschrijving)));
  const hit = (p: OpenPostGridItem) => tokens(p.bedrijf).some((t) => t.length >= 3 && words.has(t));
  return [...posts]
    .filter((p) => p.teOntvangen > 0)
    .sort((a, b) => {
      const ha = hit(a) ? 0 : 1;
      const hb = hit(b) ? 0 : 1;
      if (ha !== hb) return ha - hb;
      return (b.mutDatum || "").localeCompare(a.mutDatum || "");
    });
}

function tokens(s: string): string[] {
  return s
    .toLowerCase()
    .split(/[^\p{L}\p{N}]+/u)
    .filter(Boolean);
}

/** Label of an open post in the picker. */
export function openPostOptionLabel(p: OpenPostGridItem): string {
  return `${p.bedrijf || "Onbekende relatie"} · ${p.mutFactuur} · ${formatMoney(p.teOntvangen)} open`;
}

// Rendering the list

export type InboxRenderEntry =
  | { kind: "line"; item: InboxClassification }
  | { kind: "group"; group: PaymentGroup; lines: InboxClassification[] }
  | { kind: "verificationPair"; pair: VerificationPair; lines: InboxClassification[] };

/**
 * Turns the visible lines, the groups and the verification pairs into one
 * render list: a group (or pair) takes the place of its first member and
 * holds all its members that are still visible; members never appear as
 * loose lines. A group none of whose members is visible is left out. A line
 * that is in a group and in a pair renders with the group.
 */
export function buildRenderList(
  items: ReadonlyArray<InboxClassification>,
  groups: ReadonlyArray<PaymentGroup>,
  pairs: ReadonlyArray<VerificationPair> = [],
): InboxRenderEntry[] {
  const groupById = new Map(groups.map((g) => [g.id, g]));
  const memberOf = new Map<number, PaymentGroup>();
  for (const g of groups) for (const id of g.lineIds) memberOf.set(id, g);
  const pairOf = new Map<number, VerificationPair>();
  for (const p of pairs) for (const l of p.lines) pairOf.set(l.id, p);
  const placed = new Set<string>();
  const out: InboxRenderEntry[] = [];
  for (const item of items) {
    const g = (item.paymentGroupId && groupById.get(item.paymentGroupId)) || memberOf.get(item.id);
    if (g) {
      if (placed.has(g.id)) continue;
      placed.add(g.id);
      out.push({ kind: "group", group: g, lines: items.filter((i) => g.lineIds.includes(i.id)) });
      continue;
    }
    const p = pairOf.get(item.id);
    if (p) {
      if (placed.has(p.id)) continue;
      placed.add(p.id);
      out.push({ kind: "verificationPair", pair: p, lines: items.filter((i) => p.lines.some((l) => l.id === i.id)) });
      continue;
    }
    out.push({ kind: "line", item });
  }
  return out;
}

/** Lines that must not be picked up by "select all" or the batch bar: the
 *  members of an ambiguous group have no invoice yet. */
export function ambiguousLineIds(groups: ReadonlyArray<PaymentGroup>): Set<number> {
  return new Set(groups.filter((g) => g.state === "ambiguous").flatMap((g) => g.lineIds));
}

/** True when a batch selection can be linked to one sales invoice: only
 *  incoming lines. */
export function allIncoming(lines: ReadonlyArray<InboxClassification>): boolean {
  return lines.length > 0 && lines.every((l) => l.bedrag > 0);
}
