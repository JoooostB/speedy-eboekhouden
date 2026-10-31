import { describe, expect, it } from "vitest";
import type { LedgerAccount } from "../../api/types";
import {
  MAX_REGELS,
  addRegel,
  focusAfterDelete,
  regelBtwCents,
  regelsTotals,
  remainderCents,
  remainderStatus,
  splitFromSingle,
  splitWithTip,
  toRegelPayload,
  validateRegels,
  type RegelEdit,
} from "./regels";

const cadeaus: LedgerAccount = { id: 4500, code: "4500", omschrijving: "Personeelsgeschenken", rekeningCategorie: "KOSTEN" };
const verzend: LedgerAccount = { id: 4700, code: "4700", omschrijving: "Verzendkosten", rekeningCategorie: "KOSTEN" };

const vatRate = (code: string) => ({ HOOG_INK_21: 21, LAAG_INK_9: 9 })[code] ?? 0;

/** The two-regel invoice of "Voorbeeld Leverancier" as the e-boekhouden UI
 *  books it: 35,80 and 3,95 at 9 %, BTW 2,96 and 0,33. */
const harRegels: RegelEdit[] = [
  { key: "a", omschrijving: "Cadeau", ledgerAccount: cadeaus, btwCode: "LAAG_INK_9", bedragIncl: "35,80" },
  { key: "b", omschrijving: "Verzendkosten", ledgerAccount: verzend, btwCode: "LAAG_INK_9", bedragIncl: "3,95" },
];

/** Without the generated keys, for comparisons. */
const bare = (regels: RegelEdit[]) => regels.map(({ key: _key, ...r }) => r);

describe("regelBtwCents", () => {
  // Shared with the backend (regelBTWTable in regels_test.go, same rows):
  // the HAR values, the cent amounts, and the amounts whose net value lies
  // closest to a half cent on either side at 9 % and 21 %. Both sides must
  // agree on every row.
  const table: Array<[inclCents: number, rate: number, exclCents: number, btwCents: number]> = [
    [3580, 9, 3284, 296],
    [395, 9, 362, 33],
    [1011, 9, 928, 83],
    [12100, 21, 10000, 2100],
    [1, 21, 1, 0],
    [2, 21, 2, 0],
    [1, 9, 1, 0],
    [6, 9, 6, 0],
    [115, 9, 106, 9],
    [224, 9, 206, 18],
    [99729, 9, 91494, 8235],
    [72, 21, 60, 12],
    [193, 21, 160, 33],
    [314, 21, 260, 54],
    [99753, 21, 82440, 17313],
    [1250, 0, 1250, 0],
  ];

  it.each(table)("rounds %i cents at %i %% to %i + %i like Go regelBTW", (incl, rate, excl, btw) => {
    expect(regelBtwCents(incl, rate)).toEqual({ exclCents: excl, btwCents: btw });
  });

  it("agrees with the float rule the single-line paths use, for every amount up to € 20.000", () => {
    for (const rate of [21, 9]) {
      for (let c = 1; c <= 2_000_000; c++) {
        const float = Math.round(c / 100 / (1 + rate / 100) * 100);
        if (regelBtwCents(c, rate).exclCents !== float) throw new Error(`${c} at ${rate}%: integer rule differs from float rule`);
      }
    }
  });
});

describe("remainderCents and remainderStatus", () => {
  it("counts what is left of the total", () => {
    expect(remainderCents(harRegels, 3975)).toBe(0);
    expect(remainderCents([harRegels[0]], 3975)).toBe(395);
    expect(remainderCents([...harRegels, { ...harRegels[1], bedragIncl: "1,00" }], 3975)).toBe(-100);
  });

  it("ignores text that is not an amount", () => {
    expect(remainderCents([{ bedragIncl: "" }, { bedragIncl: "abc" }, { bedragIncl: "-5" }], 1000)).toBe(1000);
  });

  it("reads as a status line", () => {
    expect(remainderStatus(395)).toBe("Nog te verdelen: € 3,95");
    expect(remainderStatus(0)).toBe("Verdeeld");
    expect(remainderStatus(-100)).toBe("€ 1,00 te veel");
  });
});

describe("regelsTotals", () => {
  it("sums the per-regel BTW, so 3,29 for the HAR invoice (its own BTW line says 3,28)", () => {
    expect(regelsTotals(harRegels, 3975, vatRate)).toEqual({ bedragIncl: 39.75, bedragExcl: 36.46, btwBedrag: 3.29 });
  });
});

describe("splitFromSingle, splitWithTip and addRegel", () => {
  it("keeps the single line as regel 1 and starts regel 2 with the same BTW code", () => {
    const regels = splitFromSingle({ ledgerAccount: cadeaus, btwCode: "LAAG_INK_9", bedragIncl: "39,75" });
    expect(bare(regels)).toEqual([
      { omschrijving: "", ledgerAccount: cadeaus, btwCode: "LAAG_INK_9", bedragIncl: "39,75" },
      { omschrijving: "", ledgerAccount: null, btwCode: "LAAG_INK_9", bedragIncl: "" },
    ]);
    expect(regels[0].key).not.toBe(regels[1].key);
  });

  it("puts the tip on its own regel without BTW, on the same ledger", () => {
    expect(bare(splitWithTip({ ledgerAccount: cadeaus, btwCode: "LAAG_INK_9", bedragIncl: "40,00" }, 500))).toEqual([
      { omschrijving: "", ledgerAccount: cadeaus, btwCode: "LAAG_INK_9", bedragIncl: "40,00" },
      { omschrijving: "Fooi", ledgerAccount: cadeaus, btwCode: "GEEN", bedragIncl: "5,00" },
    ]);
  });

  it("pre-fills a new regel with the remainder and the BTW code above it, under a new key", () => {
    const regels = addRegel([harRegels[0]], 3975);
    expect(regels).toHaveLength(2);
    expect(bare(regels)[1]).toEqual({ omschrijving: "", ledgerAccount: null, btwCode: "LAAG_INK_9", bedragIncl: "3,95" });
    expect(regels[1].key).not.toBe(regels[0].key);
    expect(addRegel(harRegels, 3975)[2].bedragIncl).toBe("");
  });

  it("gives every added line its own key", () => {
    let regels: RegelEdit[] = [];
    for (let i = 0; i < 5; i++) regels = addRegel(regels, 1000);
    expect(new Set(regels.map((r) => r.key)).size).toBe(5);
  });

  it("stops at the maximum", () => {
    let regels: RegelEdit[] = [];
    for (let i = 0; i < MAX_REGELS + 2; i++) regels = addRegel(regels, 1000);
    expect(regels).toHaveLength(MAX_REGELS);
  });
});

describe("focusAfterDelete", () => {
  it("moves to the line that took the deleted line's place", () => {
    expect(focusAfterDelete(0, 2)).toBe(0);
    expect(focusAfterDelete(1, 3)).toBe(1);
  });

  it("moves to the previous line when the last line was deleted", () => {
    expect(focusAfterDelete(2, 2)).toBe(1);
    expect(focusAfterDelete(0, 0)).toBeNull();
  });
});

describe("validateRegels", () => {
  it("accepts lines that add up", () => {
    expect(validateRegels(harRegels, 3975)).toBeNull();
  });

  it("names the line that is not ready", () => {
    expect(validateRegels([harRegels[0], { ...harRegels[1], bedragIncl: "" }], 3975)).toBe("Regel 2: vul een bedrag in");
    expect(validateRegels([harRegels[0], { ...harRegels[1], bedragIncl: "0" }], 3975)).toBe("Regel 2: vul een bedrag in");
    expect(validateRegels([{ ...harRegels[0], ledgerAccount: null }, harRegels[1]], 3975)).toBe("Regel 1: kies een tegenrekening");
  });

  it("blocks until the remainder is zero", () => {
    expect(validateRegels(harRegels, 4075)).toBe("Nog € 1,00 te verdelen");
    expect(validateRegels(harRegels, 3875)).toBe("€ 1,00 te veel");
    expect(validateRegels([harRegels[0]], 3580)).toBe("Een splitsing heeft minstens twee regels nodig");
  });
});

describe("toRegelPayload", () => {
  it("sends internal ids for the review dialog, never the key", () => {
    expect(toRegelPayload(harRegels)).toEqual([
      { omschrijving: "Cadeau", tegenRekeningId: 4500, btwCode: "LAAG_INK_9", bedragIncl: 35.8 },
      { omschrijving: "Verzendkosten", tegenRekeningId: 4700, btwCode: "LAAG_INK_9", bedragIncl: 3.95 },
    ]);
  });

  it("sends grootboekcodes for the bank-line split and drops an empty omschrijving", () => {
    expect(toRegelPayload([{ ...harRegels[0], omschrijving: "  " }], "code")).toEqual([
      { grootboekcode: "4500", btwCode: "LAAG_INK_9", bedragIncl: 35.8 },
    ]);
  });
});
