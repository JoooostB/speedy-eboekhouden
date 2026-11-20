import type { AnalyzedInvoice, LedgerAccount, MatchedBankLine } from "../../api/types";
import { bankLineChoices, bankLineOptionLabel } from "./mailDocumentReview";

/**
 * Pure rules for the bank lines of the cards in the review dialog
 * (InvoiceReviewDialog): two documents in one review never take the same
 * bank line. Every analysis ranks its own candidates without knowing the
 * other cards, so two plane tickets with the same price both come back with
 * the same top line. No React and no API calls, so it is unit-tested.
 */

/** A choice of a card: a ranked candidate (with score) or a bare match. */
type Choice = MatchedBankLine & { score?: number };

export type AssignmentSource = Pick<AnalyzedInvoice, "matchedBankLine" | "bankLineCandidates" | "invoice">;

export interface Assignment {
  /** The line this card opens with, null for none. */
  line: MatchedBankLine | null;
  /** True when the card had a match but another card in the list got it. */
  displaced: boolean;
}

const DAY_MS = 24 * 60 * 60 * 1000;

/** Days since the epoch of the ISO date at the start of s, null when s does
 *  not start with one. Bank lines carry "2026-10-12T00:00:00", Claude
 *  "2026-10-12". */
function isoDay(s: string | undefined | null): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec((s ?? "").trim());
  if (!m) return null;
  const t = Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  return Number.isNaN(t) ? null : Math.round(t / DAY_MS);
}

function scoreOf(source: AssignmentSource, lineId: number): number {
  return source.bankLineCandidates?.find((c) => c.id === lineId)?.score ?? 0;
}

/**
 * The line each card opens with, by minimal intervention: a card whose match
 * no other card shares keeps it (also when it is not its own best candidate:
 * the backend's global assignment may have given that one to a document
 * outside this dialog), and a card without a match stays without. Cards that
 * share a match are contested: they divide the lines no keeping card holds
 * greedily, best score first, then the line closest to the document date
 * (unknown last), then card order, then line id. A contested card left
 * without a line is displaced.
 */
export function assignDistinctBankLines(sources: readonly AssignmentSource[]): Assignment[] {
  const matchCount = new Map<number, number>();
  for (const s of sources) {
    const id = s.matchedBankLine?.id;
    if (id !== undefined) matchCount.set(id, (matchCount.get(id) ?? 0) + 1);
  }

  const result: Assignment[] = sources.map(() => ({ line: null, displaced: false }));
  const held = new Set<number>();
  const contested: number[] = [];
  sources.forEach((s, i) => {
    const id = s.matchedBankLine?.id;
    if (id === undefined) return;
    if (matchCount.get(id) === 1) {
      result[i] = { line: bankLineChoices(s)[0], displaced: false };
      held.add(id);
    } else {
      contested.push(i);
    }
  });
  if (contested.length === 0) return result;

  interface Pair {
    card: number;
    line: MatchedBankLine;
    score: number;
    distance: number;
  }
  const pairs: Pair[] = [];
  for (const card of contested) {
    const source = sources[card];
    const docDay = isoDay(source.invoice.datum);
    for (const line of bankLineChoices(source)) {
      if (held.has(line.id)) continue;
      const lineDay = isoDay(line.datum);
      pairs.push({
        card,
        line,
        score: scoreOf(source, line.id),
        distance: docDay === null || lineDay === null ? Number.POSITIVE_INFINITY : Math.abs(lineDay - docDay),
      });
    }
  }
  pairs.sort(
    (a, b) => b.score - a.score || compareDistance(a.distance, b.distance) || a.card - b.card || a.line.id - b.line.id,
  );

  const done = new Set<number>();
  const taken = new Set<number>();
  for (const p of pairs) {
    if (done.has(p.card) || taken.has(p.line.id)) continue;
    done.add(p.card);
    taken.add(p.line.id);
    result[p.card] = { line: p.line, displaced: false };
  }
  for (const card of contested) {
    if (!done.has(card)) result[card] = { line: null, displaced: true };
  }
  return result;
}

function compareDistance(a: number, b: number): number {
  if (a === b) return 0;
  return a < b ? -1 : 1;
}

/** The bank-line fields of a review card. */
export interface BankLineCard {
  id: string;
  /** The linked line, 0 for none. */
  importId: number;
  /** The line shown as the payment (also when unlinked). */
  bankLine: MatchedBankLine | null;
}

export type PickEffect = { kind: "swap" | "release"; otherId: string } | null;

/**
 * The user picks `line` on card `cardId` (the select, or "Koppelen"), null to
 * unlink. When another card holds that line, it swaps: the other card gets the
 * line the picking card held, when the picking card held one and it is among
 * the other card's choices; otherwise the other card is released (no line).
 * Two cards never end up with the same line.
 */
export function pickBankLine<T extends BankLineCard>(
  cards: readonly T[],
  cardId: string,
  line: MatchedBankLine | null,
  choicesOf: (card: T) => readonly MatchedBankLine[],
): { cards: T[]; effect: PickEffect } {
  const picker = cards.find((c) => c.id === cardId);
  if (!picker) return { cards: [...cards], effect: null };
  const oldId = picker.importId;
  const other = line ? cards.find((c) => c.id !== cardId && c.importId !== 0 && c.importId === line.id) : undefined;

  let effect: PickEffect = null;
  let otherNext: T | undefined;
  if (other) {
    const back = oldId !== 0 ? choicesOf(other).find((l) => l.id === oldId) : undefined;
    if (back) {
      otherNext = { ...other, bankLine: back, importId: back.id };
      effect = { kind: "swap", otherId: other.id };
    } else {
      otherNext = { ...other, bankLine: null, importId: 0 };
      effect = { kind: "release", otherId: other.id };
    }
  }

  const next = cards.map((c) => {
    if (c.id === cardId) return { ...c, bankLine: line, importId: line?.id ?? 0 };
    if (otherNext && c.id === otherNext.id) return otherNext;
    return c;
  });
  return { cards: next, effect };
}

/** The non-zero bank line ids linked by more than one card. */
export function sharedBankLineIds(cards: readonly Pick<BankLineCard, "importId">[]): Set<number> {
  const seen = new Set<number>();
  const shared = new Set<number>();
  for (const c of cards) {
    if (c.importId === 0) continue;
    if (seen.has(c.importId)) shared.add(c.importId);
    seen.add(c.importId);
  }
  return shared;
}

/** Whether another card links the same non-zero bank line as this one. */
export function holdsSharedBankLine(card: BankLineCard, cards: readonly BankLineCard[]): boolean {
  return card.importId !== 0 && cards.some((c) => c.id !== card.id && c.importId === card.importId);
}

/** How close two scores must be for two lines with the same amount to count
 *  as interchangeable: 5 points ≈ three weeks of date distance, so a monthly
 *  repeat of the same amount is not flagged, two tickets bought days apart are. */
export const AMBIGUOUS_SCORE_MARGIN = 5;

/** Whether another choice has the same amount (in whole cents) and a score
 *  within AMBIGUOUS_SCORE_MARGIN of this line's. A missing score on either
 *  side counts as comparable. */
export function isAmbiguousBankLine(line: MatchedBankLine, choices: readonly Choice[]): boolean {
  const cents = Math.round(line.bedrag * 100);
  const score = (line as Choice).score ?? choices.find((c) => c.id === line.id)?.score;
  return choices.some((c) => {
    if (c.id === line.id || Math.round(c.bedrag * 100) !== cents) return false;
    if (score === undefined || c.score === undefined) return true;
    return Math.abs(c.score - score) <= AMBIGUOUS_SCORE_MARGIN;
  });
}

/** Option text of the bank-line select: date · amount · description, plus the
 *  own bank account ("1010 Bank") when the card's choices sit on more than
 *  one account. */
export function bankLineChoiceLabel(
  line: MatchedBankLine,
  choices: readonly MatchedBankLine[],
  accounts: ReadonlyArray<Pick<LedgerAccount, "id" | "code" | "omschrijving">>,
): string {
  const base = bankLineOptionLabel(line);
  const accountIds = new Set(choices.map((c) => c.grootboekId ?? 0));
  if (accountIds.size < 2 || !line.grootboekId) return base;
  const acc = accounts.find((a) => a.id === line.grootboekId);
  return acc ? `${base} · ${`${acc.code} ${acc.omschrijving}`.trim()}` : base;
}

// Copy

export const BANK_LINE_CHOICE_HINT = "Meerdere afschriftregels kunnen de betaling zijn. Kies de juiste.";
export const AMBIGUOUS_BANK_LINE_HINT =
  "Er zijn meerdere afschriftregels met dit bedrag. Controleer of de juiste is gekozen.";
export const DISPLACED_BANK_LINE_NOTE =
  "De afschriftregels met dit bedrag zijn al gekoppeld aan andere documenten in deze lijst.";

/** Caption under an option another card holds. */
export function heldByCaption(title: string): string {
  return `Nu gekoppeld aan ${title}`;
}

/** The note on the other card after a pick on card `pickerTitle`. */
export function pickNote(kind: "swap" | "release", pickerTitle: string): string {
  return kind === "swap"
    ? `Afschriftregel gewisseld met ${pickerTitle}.`
    : `${pickerTitle} gebruikt nu de afschriftregel die hier gekoppeld was.`;
}

/** What the polite live region says after a pick took another card's line. */
export function pickAnnouncement(kind: "swap" | "release", pickerTitle: string, otherTitle: string): string {
  return kind === "swap"
    ? `Afschriftregels gewisseld tussen ${pickerTitle} en ${otherTitle}`
    : `${otherTitle} heeft geen afschriftregel meer`;
}

/** Footer part for cards blocked by a shared bank line. */
export function sharedBankLineSummary(n: number): string {
  return `${n} met dezelfde afschriftregel`;
}
