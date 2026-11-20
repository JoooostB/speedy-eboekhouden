import { describe, it, expect } from "vitest";
import type { BankLineCandidate, InvoiceData, MatchedBankLine } from "../../api/types";
import {
  AMBIGUOUS_BANK_LINE_HINT,
  AMBIGUOUS_SCORE_MARGIN,
  DISPLACED_BANK_LINE_NOTE,
  assignDistinctBankLines,
  bankLineChoiceLabel,
  heldByCaption,
  holdsSharedBankLine,
  isAmbiguousBankLine,
  pickAnnouncement,
  pickBankLine,
  pickNote,
  sharedBankLineIds,
  sharedBankLineSummary,
  type AssignmentSource,
  type BankLineCard,
} from "./bankLineAssignment";

const line = (id: number, datum: string, score: number, overrides: Partial<BankLineCandidate> = {}): BankLineCandidate => ({
  id,
  datum: `${datum}T00:00:00`,
  bedrag: -149,
  omschrijving: `VOORBEELD LUCHTVAART ${id}`,
  score,
  ...overrides,
});

const source = (
  datum: string,
  candidates: BankLineCandidate[],
  matched: MatchedBankLine | null = candidates[0] ?? null,
): AssignmentSource => ({
  invoice: { datum } as InvoiceData,
  matchedBankLine: matched,
  bankLineCandidates: candidates,
});

const ids = (assignments: ReturnType<typeof assignDistinctBankLines>) => assignments.map((a) => a.line?.id ?? null);

describe("assignDistinctBankLines", () => {
  it("gives two identical tickets two distinct lines", () => {
    const candidates = [line(101, "2026-10-02", 119.78), line(102, "2026-10-04", 119.33)];
    const result = assignDistinctBankLines([source("2026-10-01", candidates), source("2026-10-01", candidates)]);
    expect(ids(result)).toEqual([101, 102]);
    expect(result.every((a) => !a.displaced)).toBe(true);
  });

  it("lets the document dates decide which ticket gets which line", () => {
    // Line 101 on 2 Oct, line 102 on 11 Oct, both € 149. Ticket A dated
    // 1 Oct, ticket B 5 Oct: both rank 101 first, A is closer to it.
    const a = source("2026-10-01", [line(101, "2026-10-02", 119.78), line(102, "2026-10-11", 117.78)]);
    const b = source("2026-10-05", [line(101, "2026-10-02", 119.33), line(102, "2026-10-11", 118.67)]);
    expect(ids(assignDistinctBankLines([a, b]))).toEqual([101, 102]);
    expect(ids(assignDistinctBankLines([b, a]))).toEqual([102, 101]);
  });

  it("uses the date distance when the scores are equal", () => {
    const a = source("2026-10-10", [line(101, "2026-10-01", 100), line(102, "2026-10-09", 100)]);
    const b = source("2026-10-02", [line(101, "2026-10-01", 100), line(102, "2026-10-09", 100)]);
    expect(ids(assignDistinctBankLines([a, b]))).toEqual([102, 101]);
    expect(ids(assignDistinctBankLines([b, a]))).toEqual([101, 102]);
  });

  it("displaces the third of three documents for two lines", () => {
    const candidates = [line(101, "2026-10-02", 119.78), line(102, "2026-10-04", 119.33)];
    const result = assignDistinctBankLines([
      source("2026-10-01", candidates),
      source("2026-10-01", candidates),
      source("2026-10-01", candidates),
    ]);
    expect(ids(result)).toEqual([101, 102, null]);
    expect(result.map((a) => a.displaced)).toEqual([false, false, true]);
  });

  it("leaves cards that do not collide alone", () => {
    // Not its own best candidate: the backend gave 101 to another document.
    const kept = source("2026-10-01", [line(101, "2026-10-01", 120), line(102, "2026-10-02", 110)], line(102, "2026-10-02", 110));
    // Lost the global assignment: candidates, no match.
    const lost = source("2026-10-01", [line(104, "2026-10-03", 118)], null);
    const contested = [line(104, "2026-10-03", 118), line(102, "2026-10-02", 117), line(105, "2026-10-05", 116)];
    const result = assignDistinctBankLines([kept, lost, source("2026-10-01", contested), source("2026-10-01", contested)]);
    expect(ids(result)).toEqual([102, null, 104, 105]);
    expect(result.map((a) => a.displaced)).toEqual([false, false, false, false]);
  });

  it("does not depend on card order when the scores differ", () => {
    const a = source("2026-10-01", [line(101, "2026-10-02", 119), line(102, "2026-10-03", 110), line(103, "2026-10-04", 105)]);
    const b = source("2026-10-01", [line(101, "2026-10-02", 118), line(102, "2026-10-03", 116), line(103, "2026-10-04", 104)]);
    const c = source("2026-10-01", [line(101, "2026-10-02", 117), line(102, "2026-10-03", 109), line(103, "2026-10-04", 108)]);
    const byCard = (order: AssignmentSource[]) => {
      const result = assignDistinctBankLines(order);
      return new Map(order.map((s, i) => [s, result[i].line?.id ?? null]));
    };
    const reference = byCard([a, b, c]);
    for (const order of [[c, b, a], [b, a, c], [c, a, b]]) {
      expect(byCard(order)).toEqual(reference);
    }
    expect([reference.get(a), reference.get(b), reference.get(c)]).toEqual([101, 102, 103]);
  });

  it("assigns the same set of lines on a complete tie, whatever the order", () => {
    const candidates = [line(101, "2026-10-02", 100), line(102, "2026-10-02", 100)];
    const x = source("2026-10-01", candidates);
    const y = source("2026-10-01", candidates);
    expect(new Set(ids(assignDistinctBankLines([x, y])))).toEqual(new Set([101, 102]));
    expect(new Set(ids(assignDistinctBankLines([y, x])))).toEqual(new Set([101, 102]));
  });

  it("sorts an unknown document date last", () => {
    const a = source("", [line(101, "2026-10-02", 100), line(102, "2026-10-09", 100)]);
    const b = source("2026-10-09", [line(101, "2026-10-02", 100), line(102, "2026-10-09", 100)]);
    expect(ids(assignDistinctBankLines([a, b]))).toEqual([101, 102]);
  });
});

interface Card extends BankLineCard {
  title: string;
  choices: MatchedBankLine[];
}

const L1 = line(101, "2026-10-02", 119);
const L2 = line(102, "2026-10-04", 118);
const L3 = line(103, "2026-10-06", 117);

const card = (id: string, linked: MatchedBankLine | null, choices: MatchedBankLine[]): Card => ({
  id,
  title: id,
  importId: linked?.id ?? 0,
  bankLine: linked,
  choices,
});

const pick = (cards: Card[], id: string, l: MatchedBankLine | null) => pickBankLine(cards, id, l, (c) => c.choices);

describe("pickBankLine", () => {
  it("links a free line", () => {
    const { cards, effect } = pick([card("a", L1, [L1, L2]), card("b", null, [L1])], "a", L2);
    expect(cards.map((c) => c.importId)).toEqual([102, 0]);
    expect(cards[0].bankLine).toBe(L2);
    expect(effect).toBeNull();
  });

  it("swaps when another card holds the line and can take the old one", () => {
    const { cards, effect } = pick([card("a", L1, [L1, L2]), card("b", L2, [L2, L1])], "a", L2);
    expect(cards.map((c) => c.importId)).toEqual([102, 101]);
    expect(cards[1].bankLine).toBe(L1);
    expect(effect).toEqual({ kind: "swap", otherId: "b" });
  });

  it("gives the other card its own copy of the old line", () => {
    const own = { ...L1, currencyConverted: true, invoiceCurrency: "CHF" };
    const { cards } = pick([card("a", L1, [L1, L2]), card("b", L2, [L2, own])], "a", L2);
    expect(cards[1].bankLine).toBe(own);
  });

  it("releases when the old line is not among the other card's choices", () => {
    const { cards, effect } = pick([card("a", L1, [L1, L2]), card("b", L2, [L2, L3])], "a", L2);
    expect(cards.map((c) => c.importId)).toEqual([102, 0]);
    expect(cards[1].bankLine).toBeNull();
    expect(effect).toEqual({ kind: "release", otherId: "b" });
  });

  it("releases when the picking card held no line", () => {
    const { cards, effect } = pick([card("a", null, [L1, L2]), card("b", L2, [L2, L1])], "a", L2);
    expect(cards.map((c) => c.importId)).toEqual([102, 0]);
    expect(effect).toEqual({ kind: "release", otherId: "b" });
  });

  it("'Koppelen' on a shown line another card took meanwhile releases that card", () => {
    // Card a was unlinked (line kept on screen), b picked a's line since.
    const a = { ...card("a", L1, [L1, L2]), importId: 0 };
    const { cards, effect } = pick([a, card("b", L1, [L1, L2])], "a", L1);
    expect(cards.map((c) => c.importId)).toEqual([101, 0]);
    expect(effect).toEqual({ kind: "release", otherId: "b" });
  });

  it("does not take a line from a card that only shows it unlinked", () => {
    const b = { ...card("b", L2, [L2]), importId: 0 };
    const { cards, effect } = pick([card("a", L1, [L1, L2]), b], "a", L2);
    expect(cards.map((c) => c.importId)).toEqual([102, 0]);
    expect(cards[1].bankLine).toBe(L2);
    expect(effect).toBeNull();
  });

  it("unlinks with null", () => {
    const { cards, effect } = pick([card("a", L1, [L1]), card("b", L2, [L2])], "a", null);
    expect(cards.map((c) => c.importId)).toEqual([0, 102]);
    expect(cards[0].bankLine).toBeNull();
    expect(effect).toBeNull();
  });

  it("never leaves two cards on one line", () => {
    let cards = [card("a", L1, [L1, L2, L3]), card("b", L2, [L1, L2, L3]), card("c", L3, [L1, L2])];
    for (const [id, l] of [["a", L2], ["c", L2], ["b", L3], ["a", L3], ["c", L1]] as const) {
      cards = pick(cards, id, l).cards;
      expect(sharedBankLineIds(cards).size).toBe(0);
    }
  });
});

describe("shared bank lines", () => {
  const cards: BankLineCard[] = [
    { id: "a", importId: 101, bankLine: L1 },
    { id: "b", importId: 101, bankLine: L1 },
    { id: "c", importId: 0, bankLine: null },
    { id: "d", importId: 0, bankLine: L2 },
    { id: "e", importId: 102, bankLine: L2 },
  ];

  it("finds the non-zero ids linked twice", () => {
    expect(sharedBankLineIds(cards)).toEqual(new Set([101]));
  });

  it("flags only the cards that share a linked line", () => {
    expect(cards.map((c) => holdsSharedBankLine(c, cards))).toEqual([true, true, false, false, false]);
  });
});

describe("isAmbiguousBankLine", () => {
  it("flags two lines with the same amount days apart", () => {
    expect(isAmbiguousBankLine(L1, [L1, line(102, "2026-10-05", 118)])).toBe(true);
  });

  it("does not flag a monthly repeat of the same amount", () => {
    // A month apart: the date term differs by far more than the margin.
    expect(isAmbiguousBankLine(line(101, "2026-10-02", 119.78), [line(101, "2026-10-02", 119.78), line(102, "2026-11-02", 112.89)])).toBe(false);
  });

  it("does not flag another amount", () => {
    expect(isAmbiguousBankLine(L1, [L1, line(102, "2026-10-02", 119, { bedrag: -149.01 })])).toBe(false);
  });

  it("counts the margin inclusive and a missing score as comparable", () => {
    expect(isAmbiguousBankLine(L1, [L1, line(102, "2026-10-02", 119 - AMBIGUOUS_SCORE_MARGIN)])).toBe(true);
    const bare: MatchedBankLine = { id: 101, datum: "2026-10-02T00:00:00", bedrag: -149, omschrijving: "x" };
    expect(isAmbiguousBankLine(bare, [bare, line(102, "2026-12-02", 60)])).toBe(true);
  });

  it("is false without another choice", () => {
    expect(isAmbiguousBankLine(L1, [L1])).toBe(false);
  });
});

describe("bankLineChoiceLabel", () => {
  const accounts = [
    { id: 1010, code: "1010", omschrijving: "Bank" },
    { id: 1020, code: "1020", omschrijving: "Spaarrekening" },
  ];

  it("leaves the account out when every choice is on the same account", () => {
    const a = line(101, "2026-10-13", 100, { grootboekId: 1010, omschrijving: "VOORBEELD LUCHTVAART" });
    const b = line(102, "2026-10-14", 100, { grootboekId: 1010 });
    expect(bankLineChoiceLabel(a, [a, b], accounts)).toBe("13 okt 2026 · € -149,00 · VOORBEELD LUCHTVAART");
  });

  it("names the own account when the choices sit on several", () => {
    const a = line(101, "2026-10-13", 100, { grootboekId: 1010, omschrijving: "VOORBEELD LUCHTVAART" });
    const b = line(102, "2026-10-13", 100, { grootboekId: 1020 });
    expect(bankLineChoiceLabel(a, [a, b], accounts)).toBe("13 okt 2026 · € -149,00 · VOORBEELD LUCHTVAART · 1010 Bank");
    const unknown = line(103, "2026-10-13", 100, { grootboekId: 9999, omschrijving: "X" });
    expect(bankLineChoiceLabel(unknown, [unknown, b], accounts)).toBe("13 okt 2026 · € -149,00 · X");
  });
});

describe("copy", () => {
  it("names the documents", () => {
    expect(heldByCaption("Ticket B")).toBe("Nu gekoppeld aan Ticket B");
    expect(pickNote("swap", "Ticket A")).toBe("Afschriftregel gewisseld met Ticket A.");
    expect(pickNote("release", "Ticket A")).toBe("Ticket A gebruikt nu de afschriftregel die hier gekoppeld was.");
    expect(pickAnnouncement("swap", "Ticket A", "Ticket B")).toBe("Afschriftregels gewisseld tussen Ticket A en Ticket B");
    expect(pickAnnouncement("release", "Ticket A", "Ticket B")).toBe("Ticket B heeft geen afschriftregel meer");
    expect(sharedBankLineSummary(2)).toBe("2 met dezelfde afschriftregel");
    expect(AMBIGUOUS_BANK_LINE_HINT).toBe("Er zijn meerdere afschriftregels met dit bedrag. Controleer of de juiste is gekozen.");
    expect(DISPLACED_BANK_LINE_NOTE).toBe("De afschriftregels met dit bedrag zijn al gekoppeld aan andere documenten in deze lijst.");
  });
});
