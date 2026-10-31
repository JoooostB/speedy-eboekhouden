import { describe, expect, it } from "vitest";
import type { InboxClassification, InboxProcessResult, PaymentGroup } from "../../api/types";
import { buildRenderList } from "./paymentGroups";
import {
  boekenPairLabel,
  buildVerificationPairs,
  failedPairLines,
  pairButtonLabel,
  pairId,
  pairResultAlert,
  pairSummaryLine,
  pairTotal,
  withPairPartners,
} from "./verificationPairs";

const desc = "eCom, Betaalpas Voorbeeld Winkel P0000000001 NR:TERMBNET, 23.10.26/10:56 VOORBEELDWINKEL.COM, Land: IRL KAARTNUMMER: **1234";

const line = (id: number, bedrag: number, overrides: Partial<InboxClassification> = {}): InboxClassification => ({
  id,
  datum: "2026-10-23T00:00:00",
  bedrag,
  omschrijving: desc,
  rekening: "",
  grootboekId: 1020,
  category: "auto",
  needsInvoice: false,
  confidence: 1,
  grootboekcode: "9300",
  btwCode: "GEEN",
  soort: bedrag < 0 ? "GeldUitgegeven" : "GeldOntvangen",
  aiOmschrijving: desc,
  indicator: "Controlebetaling · boeken op 9300 Bijzondere baten",
  ...overrides,
});

const charge = line(1, -1, { verification: { kind: "card_check", pairLineId: 2 } });
const refund = line(2, 1, { verification: { kind: "card_check", pairLineId: 1 } });

describe("buildVerificationPairs", () => {
  it("pairs two lines that point at each other, charge first", () => {
    const pairs = buildVerificationPairs([refund, line(5, -12), charge]);
    expect(pairs).toHaveLength(1);
    expect(pairs[0].id).toBe("vp-1-2");
    expect(pairs[0].lines.map((l) => l.id)).toEqual([1, 2]);
  });

  it("leaves a line whose partner is gone or does not point back as a loose row", () => {
    expect(buildVerificationPairs([charge])).toEqual([]);
    const stranger = line(2, 1, { verification: { kind: "card_check", pairLineId: 7 } });
    expect(buildVerificationPairs([charge, stranger])).toEqual([]);
    const single = line(3, -1, { verification: { kind: "card_check" } });
    expect(buildVerificationPairs([single])).toEqual([]);
  });

  it("puts a line in at most one pair", () => {
    const pairs = buildVerificationPairs([charge, refund, line(3, 1, { verification: { kind: "card_check", pairLineId: 1 } })]);
    expect(pairs.map((p) => p.id)).toEqual(["vp-1-2"]);
  });

  it("never pairs a line that renders with a payment group", () => {
    const group = { id: "pg-1", lineIds: [2, 7] } as PaymentGroup;
    expect(buildVerificationPairs([charge, refund], [group])).toEqual([]);
    expect(buildVerificationPairs([charge, { ...refund, paymentGroupId: "pg-1" }])).toEqual([]);
    expect(pairId(9, 3)).toBe("vp-3-9");
  });

  it("shows a pair whole when either leg passes the filter", () => {
    const pairs = buildVerificationPairs([charge, refund]);
    const { items, pairs: shown } = withPairPartners([line(9, -5), charge], pairs);
    expect(items.map((i) => i.id)).toEqual([9, 1, 2]);
    expect(shown.map((p) => p.id)).toEqual(["vp-1-2"]);
    expect(withPairPartners([line(9, -5)], pairs)).toEqual({ items: [line(9, -5)], pairs: [] });
    expect(withPairPartners([charge, refund], pairs).items.map((i) => i.id)).toEqual([1, 2]);
  });
});

describe("pair copy", () => {
  it("says the legs cancel out and nets to zero", () => {
    expect(pairSummaryLine([charge, refund])).toBe("−€ 1,00 en +€ 1,00 heffen elkaar op");
    expect(pairTotal([charge, refund])).toBe(0);
    expect(pairTotal([line(1, -0.1), line(2, 0.3)])).toBe(0.2);
  });

  it("labels the button and its accessible name", () => {
    expect(pairButtonLabel(2, false)).toBe("Boeken (2)");
    expect(pairButtonLabel(1, true)).toBe("Opnieuw proberen (1)");
    expect(boekenPairLabel(2, false, "9300 Bijzondere baten")).toBe("Boek 2 controlebetalingen op 9300 Bijzondere baten");
    expect(boekenPairLabel(1, true, "")).toBe("Probeer de controlebetaling opnieuw te boeken");
  });

  it("keeps only the failed lines for a retry", () => {
    const results = new Map<number, InboxProcessResult>([
      [1, { id: 1, status: "ok" }],
      [2, { id: 2, status: "error", error: "Boeking mislukt" }],
    ]);
    expect(failedPairLines([charge, refund], results).map((l) => l.id)).toEqual([2]);
    expect(pairResultAlert([charge, refund], results)).toBe("1 van de 2 controlebetalingen geboekt, 1 mislukt: Boeking mislukt.");
    expect(pairResultAlert([refund], results)).toBe("Boeken mislukt: Boeking mislukt");
    expect(pairResultAlert([charge], results)).toBeNull();
  });
});

describe("buildRenderList with pairs", () => {
  it("renders a pair at its first member's place and never as loose lines", () => {
    const pairs = buildVerificationPairs([charge, refund]);
    const list = buildRenderList([line(9, -5), refund, line(8, 3), charge], [], pairs);
    expect(list.map((e) => (e.kind === "line" ? `line ${e.item.id}` : `${e.kind} ${e.kind === "verificationPair" ? e.pair.id : e.group.id}`))).toEqual([
      "line 9",
      "verificationPair vp-1-2",
      "line 8",
    ]);
    const entry = list[1];
    expect(entry.kind === "verificationPair" && entry.lines.map((l) => l.id)).toEqual([2, 1]);
  });

  it("is unchanged without pairs", () => {
    expect(buildRenderList([charge, refund], []).map((e) => e.kind)).toEqual(["line", "line"]);
  });
});
