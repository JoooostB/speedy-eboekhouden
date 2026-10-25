import { describe, expect, it } from "vitest";
import { formatFormAmount, fxInvoiceNote } from "./reviewAmounts";

describe("formatFormAmount", () => {
  // The bug: the result line and the receipt totals said "€12.50".
  it("formats a form amount in nl-NL notation", () => {
    expect(formatFormAmount("12.5")).toBe("€ 12,50");
    expect(formatFormAmount("1234.56")).toBe("€ 1.234,56");
    expect(formatFormAmount("0")).toBe("€ 0,00");
  });

  it("returns text that is not a number unchanged", () => {
    expect(formatFormAmount("")).toBe("");
    expect(formatFormAmount("onbekend")).toBe("onbekend");
  });
});

describe("fxInvoiceNote", () => {
  // The bug: "Factuur: 32.80 CHF".
  it("shows the invoice amount in its own currency, nl-NL", () => {
    expect(fxInvoiceNote(32.8, "CHF")).toBe(
      "Factuur: CHF 32,80, omgerekend naar euro om de betaling te vinden. Speedy vult het werkelijke eurobedrag van de bank in.",
    );
    expect(fxInvoiceNote(1250, "usd")).toMatch(/^Factuur: USD 1\.250,00, /);
  });

  it("names only the currency without an amount", () => {
    expect(fxInvoiceNote(undefined, "GBP")).toMatch(/^Factuur: GBP, omgerekend/);
  });
});
