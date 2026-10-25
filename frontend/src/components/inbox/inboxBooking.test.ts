import { describe, it, expect } from "vitest";
import type { InboxClassification, LedgerAccount, OpenPostGridItem } from "../../api/types";
import {
  bookingBlocker,
  documentMatchIndicator,
  findRefundCandidate,
  hasOpenInvoiceReference,
  isFactuurbetalingSoort,
  koppelenLabel,
  toInboxProcessItem,
} from "./inboxBooking";

const row = (overrides: Partial<InboxClassification> = {}): InboxClassification => ({
  id: 42,
  datum: "2026-10-12T00:00:00",
  bedrag: -21.78,
  omschrijving: "SEPA Acme oktober",
  rekening: "NL00BANK0123456789",
  grootboekId: 5,
  category: "review",
  needsInvoice: false,
  confidence: 0.6,
  grootboekcode: "4000",
  btwCode: "HOOG_INK_21",
  soort: "GeldUitgegeven",
  aiOmschrijving: "Software Acme",
  indicator: "AI",
  ...overrides,
});

const openPostRow = (overrides: Partial<InboxClassification> = {}): InboxClassification =>
  row({
    category: "auto",
    confidence: 1,
    grootboekcode: "",
    btwCode: "GEEN",
    soort: "FactuurbetalingVerstuurd",
    aiOmschrijving: "SEPA Acme oktober",
    indicator: "Acme B.V. · INV-0042 · geboekt 1 okt",
    relatieId: 4711,
    relatieNaam: "Acme B.V.",
    factuurnummer: "INV-0042",
    openPostMatch: true,
    ...overrides,
  });

const ledger: LedgerAccount = { id: 1, code: "4000", omschrijving: "Software", rekeningCategorie: "KOSTEN" };

const post = (overrides: Partial<OpenPostGridItem> = {}): OpenPostGridItem => ({
  mutDatum: "2026-10-01T00:00:00",
  sBedrag: 21.78,
  mutFactuur: "INV-0042",
  mutNr: 1001,
  relatieCode: "R1",
  relatieId: 4711,
  bedrijf: "Acme B.V.",
  sBetaald: 0,
  teOntvangen: 21.78,
  vervaldatum: "2026-10-31T00:00:00",
  mutOmschrijving: "",
  mutSoort: 1,
  ...overrides,
});

describe("isFactuurbetalingSoort", () => {
  it("recognises both payment soorten and nothing else", () => {
    expect(isFactuurbetalingSoort("FactuurbetalingVerstuurd")).toBe(true);
    expect(isFactuurbetalingSoort("FactuurbetalingOntvangen")).toBe(true);
    expect(isFactuurbetalingSoort("GeldUitgegeven")).toBe(false);
    expect(isFactuurbetalingSoort("FactuurOntvangen")).toBe(false);
    expect(isFactuurbetalingSoort("Memoriaal")).toBe(false);
  });
});

describe("hasOpenInvoiceReference", () => {
  it("needs both a relation and a non-blank invoice number", () => {
    expect(hasOpenInvoiceReference({ relatieId: 1, factuurnummer: "X" })).toBe(true);
    expect(hasOpenInvoiceReference({ relatieId: 0, factuurnummer: "X" })).toBe(false);
    expect(hasOpenInvoiceReference({ relatieId: 1, factuurnummer: "" })).toBe(false);
    expect(hasOpenInvoiceReference({ relatieId: 1, factuurnummer: "   " })).toBe(false);
    expect(hasOpenInvoiceReference({})).toBe(false);
  });
});

describe("toInboxProcessItem", () => {
  it("maps soort names to codes and sends the absolute amount", () => {
    const item = toInboxProcessItem(row());
    expect(item).toEqual({
      id: 42,
      grootboekId: 5,
      soort: 6,
      grootboekcode: "4000",
      btwCode: "HOOG_INK_21",
      omschrijving: "Software Acme",
      bedrag: 21.78,
    });
    expect(item).not.toHaveProperty("relatieId");
    expect(item).not.toHaveProperty("factuurnummer");
  });

  it("builds a factuurbetaling verstuurd (soort 4) with relation and invoice number, BTW GEEN", () => {
    expect(toInboxProcessItem(openPostRow())).toEqual({
      id: 42,
      grootboekId: 5,
      soort: 4,
      grootboekcode: "",
      btwCode: "GEEN",
      omschrijving: "SEPA Acme oktober",
      bedrag: 21.78,
      relatieId: 4711,
      factuurnummer: "INV-0042",
    });
  });

  it("builds a factuurbetaling ontvangen (soort 3) for an incoming line", () => {
    const item = toInboxProcessItem(openPostRow({ bedrag: 500, soort: "FactuurbetalingOntvangen" }));
    expect(item.soort).toBe(3);
    expect(item.bedrag).toBe(500);
    expect(item.relatieId).toBe(4711);
    expect(item.factuurnummer).toBe("INV-0042");
  });

  it("forces BTW to GEEN on a factuurbetaling even when the row says otherwise", () => {
    expect(toInboxProcessItem(openPostRow({ btwCode: "HOOG_INK_21" })).btwCode).toBe("GEEN");
  });

  it("lets the caller override the relation (InboxRow's RelationPicker)", () => {
    expect(toInboxProcessItem(row(), 99).relatieId).toBe(99);
    expect(toInboxProcessItem(openPostRow(), 99).relatieId).toBe(99);
  });

  it("does not attach the invoice number to a non-factuurbetaling soort", () => {
    const item = toInboxProcessItem(openPostRow({ soort: "GeldUitgegeven" }));
    expect(item.soort).toBe(6);
    expect(item).not.toHaveProperty("factuurnummer");
    expect(item.relatieId).toBe(4711);
  });

  it("falls back to the bank description and to Geld uitgegeven", () => {
    const item = toInboxProcessItem(row({ aiOmschrijving: "", soort: "Onbekend" as never, grootboekId: 0 }));
    expect(item.omschrijving).toBe("SEPA Acme oktober");
    expect(item.soort).toBe(6);
    expect(item.grootboekId).toBe(0);
  });
});

describe("bookingBlocker", () => {
  it("requires a tegenrekening for a normal booking", () => {
    expect(bookingBlocker(row(), { ledger: null })).toBe("Geen tegenrekening");
    expect(bookingBlocker(row(), undefined)).toBe("Geen tegenrekening");
    expect(bookingBlocker(row(), { ledger })).toBeNull();
  });

  it("does not require a tegenrekening for a factuurbetaling", () => {
    expect(bookingBlocker(openPostRow(), { ledger: null })).toBeNull();
    expect(bookingBlocker(openPostRow(), undefined)).toBeNull();
  });

  it("requires the open invoice reference for a factuurbetaling", () => {
    expect(bookingBlocker(openPostRow({ relatieId: undefined }), { ledger })).toBe("Geen openstaande factuur");
    expect(bookingBlocker(openPostRow({ factuurnummer: "" }), { ledger })).toBe("Geen openstaande factuur");
    expect(bookingBlocker(row({ soort: "FactuurbetalingOntvangen" }), { ledger })).toBe("Geen openstaande factuur");
  });
});

describe("findRefundCandidate", () => {
  const posts = [post({ mutFactuur: "A", teOntvangen: 10 }), post({ mutFactuur: "B", teOntvangen: 21.78 })];

  it("finds the post whose outstanding amount equals the incoming amount", () => {
    expect(findRefundCandidate(21.78, posts)?.mutFactuur).toBe("B");
    expect(findRefundCandidate(10, posts)?.mutFactuur).toBe("A");
  });

  it("ignores outgoing amounts, non-matching amounts and settled posts", () => {
    expect(findRefundCandidate(-21.78, posts)).toBeUndefined();
    expect(findRefundCandidate(21.79, posts)).toBeUndefined();
    expect(findRefundCandidate(0, [post({ teOntvangen: 0 })])).toBeUndefined();
    expect(findRefundCandidate(5, [post({ teOntvangen: 0, sBedrag: 5 })])).toBeUndefined();
  });

  it("is not fooled by float noise", () => {
    expect(findRefundCandidate(0.1 + 0.2, [post({ teOntvangen: 0.3 })])).toBeDefined();
  });
});

describe("labels", () => {
  it("names the invoice and company in the Koppelen label", () => {
    expect(koppelenLabel(openPostRow())).toBe("Koppel betaling aan factuur INV-0042 van Acme B.V.");
    expect(koppelenLabel(openPostRow({ relatieNaam: undefined }))).toBe("Koppel betaling aan factuur INV-0042");
    expect(koppelenLabel(openPostRow({ factuurnummer: undefined }))).toBe("Koppel betaling aan openstaande factuur van Acme B.V.");
  });

  it("describes a document match", () => {
    expect(documentMatchIndicator("Acme · 1 okt 2026 · € 21,78")).toBe("Factuur Acme · 1 okt 2026 · € 21,78 staat in je mailmap");
  });
});
