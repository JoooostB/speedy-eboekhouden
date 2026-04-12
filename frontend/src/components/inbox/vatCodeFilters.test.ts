import { describe, it, expect } from "vitest";
import { dedupePurchaseVatCodes, isSalesCode } from "./vatCodeFilters";
import type { VATCode } from "../../api/types";

const code = (
  id: number,
  c: string,
  omschrijving: string,
  percentage: number,
): VATCode => ({
  id,
  code: c,
  omschrijving,
  soort: "INKOOP",
  rekenpercentage: percentage,
  percentage,
});

describe("isSalesCode", () => {
  it("flags _VERK_ codes", () => {
    expect(isSalesCode("HOOG_VERK_21")).toBe(true);
    expect(isSalesCode("LAAG_VERK_9")).toBe(true);
  });

  it("does not flag _INK_ codes", () => {
    expect(isSalesCode("HOOG_INK_21")).toBe(false);
    expect(isSalesCode("LAAG_INK_9")).toBe(false);
  });

  it("does not flag GEEN or reverse-charge codes", () => {
    expect(isSalesCode("GEEN")).toBe(false);
    expect(isSalesCode("VERL_INK")).toBe(false);
    expect(isSalesCode("BU_EU_INK")).toBe(false);
    expect(isSalesCode("BI_EU_INK")).toBe(false);
  });
});

describe("dedupePurchaseVatCodes", () => {
  it("strips sales codes", () => {
    const out = dedupePurchaseVatCodes([
      code(1, "HOOG_INK_21", "Btw hoog 21%", 21),
      code(2, "HOOG_VERK_21", "Btw hoog 21%", 21),
      code(3, "LAAG_VERK_9", "Btw laag 9%", 9),
    ]);
    expect(out.map((c) => c.code)).toEqual(["HOOG_INK_21"]);
  });

  it("dedupes purchase codes that render identically", () => {
    // The regression we hit in production: e-boekhouden returned multiple
    // INK variants with identical omschrijving + percentage. The picker
    // showed them as visual duplicates which the user couldn't tell apart.
    const out = dedupePurchaseVatCodes([
      code(1, "LAAG_INK_6", "Btw laag 6%", 6),
      code(2, "LAAG_INK_6_OLD", "Btw laag 6%", 6),
      code(3, "LAAG_INK_9", "Btw laag 9%", 9),
      code(4, "LAAG_INK_9_FF", "Btw laag 9%", 9),
      code(5, "HOOG_INK_21", "Btw hoog 21%", 21),
      code(6, "HOOG_INK_21_LEG", "Btw hoog 21%", 21),
    ]);
    expect(out.map((c) => c.code)).toEqual([
      "LAAG_INK_6",
      "LAAG_INK_9",
      "HOOG_INK_21",
    ]);
  });

  it("treats different percentages as different even with same omschrijving", () => {
    // Defensive: if e-boekhouden ever produces a code where the percentage
    // genuinely differs, keep both. The dedup key is omschrijving + %.
    const out = dedupePurchaseVatCodes([
      code(1, "AFWIJK_A", "Afwijkend", 5),
      code(2, "AFWIJK_B", "Afwijkend", 7),
    ]);
    expect(out.length).toBe(2);
  });

  it("preserves reverse-charge codes (which legitimately share 0%)", () => {
    const out = dedupePurchaseVatCodes([
      code(1, "VERL_INK", "Btw verlegd 21%", 0),
      code(2, "VERL_INK_L9", "Btw verlegd 9%", 0),
      code(3, "BU_EU_INK", "Leveringen/diensten van buiten EU 0%", 0),
      code(4, "BI_EU_INK", "Leveringen/diensten van binnen EU 0%", 0),
      code(5, "GEEN", "Geen btw", 0),
    ]);
    // All distinct labels — none should be deduped despite all being 0%.
    expect(out.map((c) => c.code)).toEqual([
      "VERL_INK",
      "VERL_INK_L9",
      "BU_EU_INK",
      "BI_EU_INK",
      "GEEN",
    ]);
  });

  it("handles an empty list", () => {
    expect(dedupePurchaseVatCodes([])).toEqual([]);
  });

  it("keeps the first occurrence on collision", () => {
    const out = dedupePurchaseVatCodes([
      code(1, "FIRST", "Same label", 21),
      code(2, "SECOND", "Same label", 21),
    ]);
    expect(out.length).toBe(1);
    expect(out[0].code).toBe("FIRST");
  });
});
