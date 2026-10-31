import { describe, expect, it } from "vitest";
import type { InboxClassification, InboxProcessResult, OpenPostGridItem, PaymentGroup } from "../../api/types";
import {
  addableLines,
  allIncoming,
  ambiguousLineIds,
  buildGroupPayload,
  buildRenderList,
  candidateFitLabel,
  failedLines,
  filterAddable,
  fitState,
  groupButtonLabel,
  groupInvoice,
  groupProgress,
  groupSummaryLine,
  groupTitle,
  includeLabel,
  invoiceFromOpenPost,
  isOverpayment,
  lineStatus,
  primaryBlocker,
  primaryLabel,
  remainder,
  resultAlert,
  snackbarMessage,
  sortOpenPostsForPicker,
  summaryStatus,
  sumLines,
} from "./paymentGroups";

// A fictitious invoice of € 12.000 excl + € 2.520 BTW, paid in the WKA shape.
const invoice = {
  relatieId: 9701,
  relatieNaam: "Voorbeeld Bouw B.V.",
  factuurnummer: "VF-2026-0017",
  factuurdatum: "2026-10-01T00:00:00",
  sBedrag: 14520,
  sBetaald: 0,
  teOntvangen: 14520,
};

const line = (id: number, bedrag: number, overrides: Partial<InboxClassification> = {}): InboxClassification => ({
  id,
  datum: "2026-10-20T00:00:00",
  bedrag,
  omschrijving: `OVERBOEKING Voorbeeld Bouw VF-2026-0017 deel ${id}`,
  rekening: "NL00VOOR0000000001",
  grootboekId: 1022,
  category: "auto",
  needsInvoice: false,
  confidence: 1,
  grootboekcode: "",
  btwCode: "GEEN",
  soort: "FactuurbetalingOntvangen",
  aiOmschrijving: "",
  indicator: "",
  relatieId: 9701,
  relatieNaam: "Voorbeeld Bouw B.V.",
  factuurnummer: "VF-2026-0017",
  paymentGroupId: "pg-1",
  ...overrides,
});

const lines = [line(1, 10800, { grootboekId: 1020 }), line(2, 1200), line(3, 2520)];

const group: PaymentGroup = {
  id: "pg-1",
  state: "complete",
  category: "auto",
  ...invoice,
  lineIds: [1, 2, 3],
  total: 14520,
  candidates: [{ ...invoice, lineIds: [1, 2, 3] }],
  evidence: "factuurnummer",
};

describe("sums and fit", () => {
  it("sums in cents without float noise", () => {
    expect(sumLines([{ bedrag: 0.1 }, { bedrag: 0.2 }])).toBe(0.3);
    expect(sumLines(lines)).toBe(14520);
    expect(sumLines([{ bedrag: -5 }])).toBe(5);
  });

  it("computes the remainder and the fit state", () => {
    expect(remainder(invoice, 10800)).toBe(3720);
    expect(fitState(invoice, 14520)).toBe("complete");
    expect(fitState(invoice, 14520.01)).toBe("complete");
    expect(fitState(invoice, 14519.99)).toBe("complete");
    expect(fitState(invoice, 14520.02)).toBe("over");
    expect(fitState(invoice, 10800)).toBe("partial");
    expect(isOverpayment(invoice, 14620)).toBe(true);
    expect(isOverpayment(invoice, 14520)).toBe(false);
  });
});

describe("copy", () => {
  it("summarises a complete group", () => {
    expect(groupSummaryLine(group)).toBe("3 betalingen · € 14.520,00 van € 14.520,00 · factuur volledig betaald");
    expect(groupButtonLabel(group)).toBe("Koppelen (3)");
    expect(groupTitle(group)).toBe("Voorbeeld Bouw B.V. · factuur VF-2026-0017");
    expect(groupProgress(group)).toBe(100);
  });

  it("summarises a partial group", () => {
    const partial: PaymentGroup = { ...group, state: "partial", category: "review", lineIds: [1], total: 10800 };
    expect(groupSummaryLine(partial)).toBe("1 betaling · € 10.800,00 van € 14.520,00 · nog open € 3.720,00");
    expect(groupProgress(partial)).toBe(74);
  });

  it("summarises an ambiguous group without picking", () => {
    const ambiguous: PaymentGroup = {
      ...group,
      state: "ambiguous",
      category: "review",
      relatieId: 0,
      relatieNaam: "",
      factuurnummer: "",
      candidates: [
        { ...invoice, lineIds: [1, 2, 3] },
        { ...invoice, factuurnummer: "VF-2026-0018", lineIds: [1, 2, 3] },
      ],
    };
    expect(groupSummaryLine(ambiguous)).toBe("Past bij 2 openstaande facturen van Voorbeeld Bouw B.V.");
    expect(groupButtonLabel(ambiguous)).toBe("Kiezen");
    expect(groupTitle(ambiguous)).toBe("Voorbeeld Bouw B.V.");
    expect(groupInvoice(ambiguous)).toBeNull();
    expect(ambiguousLineIds([group, ambiguous])).toEqual(new Set([1, 2, 3]));
  });

  it("writes the sticky summary", () => {
    expect(summaryStatus(invoice, 14520)).toEqual({
      text: "Samen € 14.520,00 van € 14.520,00 open. Na koppelen is de factuur volledig betaald.",
      blocking: false,
    });
    expect(summaryStatus(invoice, 10800)).toEqual({
      text: "Samen € 10.800,00 van € 14.520,00 open. Na koppelen blijft € 3.720,00 open.",
      blocking: false,
    });
    expect(summaryStatus(invoice, 14620)).toEqual({
      text: "Samen € 14.620,00, dat is € 100,00 meer dan er openstaat. Haal een betaling weg of kies een andere factuur.",
      blocking: true,
    });
    expect(summaryStatus(null, 100)).toEqual({ text: "Samen € 100,00", blocking: false });
  });

  it("labels the primary button and its blockers", () => {
    expect(primaryLabel(3, false)).toBe("3 betalingen koppelen");
    expect(primaryLabel(1, false)).toBe("Betaling koppelen");
    expect(primaryLabel(2, true)).toBe("Opnieuw proberen (2)");
    expect(primaryBlocker(null, 3, 100)).toBe("Kies eerst een factuur.");
    expect(primaryBlocker(invoice, 0, 0)).toBe("Selecteer minstens één betaling.");
    expect(primaryBlocker(invoice, 3, 14620)).toContain("meer dan er openstaat");
    expect(primaryBlocker(invoice, 3, 14520)).toBeNull();
  });

  it("names a line's checkbox", () => {
    expect(includeLabel(lines[0], "1020 Bank")).toBe("Betaling van 20 okt 2026, € 10.800,00 op 1020 Bank meenemen");
    expect(includeLabel(lines[0], "")).toBe("Betaling van 20 okt 2026, € 10.800,00 meenemen");
  });

  it("writes the snackbar", () => {
    expect(snackbarMessage(3, invoice, 14520)).toBe("3 betalingen gekoppeld aan factuur VF-2026-0017. De factuur is volledig betaald.");
    expect(snackbarMessage(1, invoice, 10800)).toBe("1 betaling gekoppeld aan factuur VF-2026-0017. Nog open: € 3.720,00.");
  });

  it("labels how a candidate fits", () => {
    expect(candidateFitLabel({ ...invoice, lineIds: [1, 2, 3] }, lines)).toBe("Past precies");
    expect(candidateFitLabel({ ...invoice, lineIds: [1] }, lines)).toBe("Past deels");
  });
});

describe("booking", () => {
  it("builds one factuurbetaling per line on the same invoice", () => {
    const other = { ...invoice, relatieId: 9702, factuurnummer: "VF-2026-0018" };
    const payload = buildGroupPayload(lines, other);
    expect(payload).toEqual([
      { id: 1, grootboekId: 1020, soort: 3, grootboekcode: "", btwCode: "GEEN", omschrijving: lines[0].omschrijving, bedrag: 10800, relatieId: 9702, factuurnummer: "VF-2026-0018" },
      { id: 2, grootboekId: 1022, soort: 3, grootboekcode: "", btwCode: "GEEN", omschrijving: lines[1].omschrijving, bedrag: 1200, relatieId: 9702, factuurnummer: "VF-2026-0018" },
      { id: 3, grootboekId: 1022, soort: 3, grootboekcode: "", btwCode: "GEEN", omschrijving: lines[2].omschrijving, bedrag: 2520, relatieId: 9702, factuurnummer: "VF-2026-0018" },
    ]);
  });

  it("retries only the failed lines", () => {
    const results = new Map<number, InboxProcessResult>([
      [1, { status: "ok", mutNr: "767" }],
      [2, { status: "error", error: "Boeking geweigerd" }],
      [3, { status: "error", error: "Boeking geweigerd" }],
    ]);
    expect(failedLines(lines, results).map((l) => l.id)).toEqual([2, 3]);
    expect(resultAlert(results)).toBe("1 van de 3 betalingen zijn geboekt. 2 niet: Boeking geweigerd. De geboekte betalingen blijven staan.");
    expect(lineStatus(results.get(1))).toBe("Geboekt");
    expect(lineStatus(results.get(2))).toBe("Niet geboekt: Boeking geweigerd");
    expect(lineStatus(undefined)).toBeNull();
  });

  it("reports a total failure", () => {
    const results = new Map<number, InboxProcessResult>([
      [1, { status: "error", error: "Sessie verlopen" }],
      [2, { status: "error", error: "Sessie verlopen" }],
    ]);
    expect(resultAlert(results)).toBe("Koppelen mislukt: Sessie verlopen. Er is niets geboekt.");
    expect(resultAlert(new Map([[1, { status: "ok" } as InboxProcessResult]]))).toBeNull();
  });
});

describe("picking", () => {
  const others = [
    line(4, 500, { datum: "2026-10-25T00:00:00", omschrijving: "Andere klant", paymentGroupId: undefined }),
    line(5, 250, { datum: "2026-09-30T00:00:00", omschrijving: "Te vroeg", paymentGroupId: undefined }),
    line(6, -80, { datum: "2026-10-25T00:00:00", omschrijving: "Uitgaand", paymentGroupId: undefined }),
  ];

  it("offers other incoming lines dated from the invoice date", () => {
    const all = [...lines, ...others];
    expect(addableLines(all, new Set([1, 2, 3]), invoice).map((l) => l.id)).toEqual([4]);
    expect(addableLines(all, new Set([1, 2, 3]), null).map((l) => l.id)).toEqual([4, 5]);
  });

  it("filters on amount or description", () => {
    expect(filterAddable(others, "andere").map((l) => l.id)).toEqual([4]);
    expect(filterAddable(others, "500").map((l) => l.id)).toEqual([4]);
    expect(filterAddable(others, "250,00").map((l) => l.id)).toEqual([5]);
    expect(filterAddable(others, "").map((l) => l.id)).toEqual([4, 5, 6]);
    expect(filterAddable(others, "xyz")).toEqual([]);
  });

  it("puts open posts with a name hit first", () => {
    const post = (bedrijf: string, mutFactuur: string, mutDatum: string, teOntvangen: number): OpenPostGridItem => ({
      mutDatum, sBedrag: teOntvangen, mutFactuur, mutNr: 1, relatieCode: "", relatieId: 1, bedrijf, sBetaald: 0, teOntvangen, vervaldatum: "", mutOmschrijving: "", mutSoort: 2,
    });
    const posts = [
      post("Andere Klant", "AK-1", "2026-10-09", 100),
      post("Voorbeeld Bouw B.V.", "VF-2026-0017", "2026-10-01", 14520),
      post("Voorbeeld Bouw B.V.", "VF-2026-0018", "2026-10-05", 14520),
      post("Betaald", "B-1", "2026-10-10", 0),
    ];
    expect(sortOpenPostsForPicker(posts, lines).map((p) => p.mutFactuur)).toEqual(["VF-2026-0018", "VF-2026-0017", "AK-1"]);
    expect(invoiceFromOpenPost(posts[1])).toEqual({ ...invoice, relatieId: 1, factuurdatum: "2026-10-01" });
  });

  it("knows when a selection can be linked to one invoice", () => {
    expect(allIncoming(lines)).toBe(true);
    expect(allIncoming([...lines, others[2]])).toBe(false);
    expect(allIncoming([])).toBe(false);
  });
});

describe("render list", () => {
  it("replaces the members by one group entry at the first member's place", () => {
    const loose = line(9, 10, { paymentGroupId: undefined, omschrijving: "Los" });
    const list = buildRenderList([loose, lines[0], lines[1], line(8, 1, { paymentGroupId: undefined }), lines[2]], [group]);
    expect(list.map((e) => (e.kind === "line" ? `line ${e.item.id}` : `group ${e.group.id}`))).toEqual(["line 9", "group pg-1", "line 8"]);
    const entry = list[1];
    expect(entry.kind === "group" && entry.lines.map((l) => l.id)).toEqual([1, 2, 3]);
  });

  it("leaves out a group with no visible member and keeps a partly processed one", () => {
    expect(buildRenderList([], [group])).toEqual([]);
    const list = buildRenderList([lines[1], lines[2]], [group]);
    expect(list.length).toBe(1);
    expect(list[0].kind === "group" && list[0].lines.map((l) => l.id)).toEqual([2, 3]);
  });
});
