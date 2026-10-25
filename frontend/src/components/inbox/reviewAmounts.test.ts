import { describe, expect, it } from "vitest";
import { formatAmountInput, formatFormAmount, fxInvoiceNote, invalidAmountField, parseAmount } from "./reviewAmounts";

describe("parseAmount", () => {
  // The bug: the amount fields were read with parseFloat, so "12,50" was
  // booked as 12 and "1.234,56" as 1.234.
  it("reads Dutch amounts", () => {
    expect(parseAmount("12,50")).toBe(12.5);
    expect(parseAmount("12,5")).toBe(12.5);
    expect(parseAmount("0,99")).toBe(0.99);
    expect(parseAmount(",5")).toBe(0.5);
    expect(parseAmount("1.234,56")).toBe(1234.56);
    expect(parseAmount("1.234")).toBe(1234);
    expect(parseAmount("12.345.678,90")).toBe(12345678.9);
    expect(parseAmount("€ 12,50")).toBe(12.5);
    expect(parseAmount("1 234,56")).toBe(1234.56);
    expect(parseAmount("1\u00a0234,56")).toBe(1234.56);
    expect(parseAmount("1234")).toBe(1234);
  });

  it("still reads amounts with a decimal point", () => {
    expect(parseAmount("12.50")).toBe(12.5);
    expect(parseAmount("12.5")).toBe(12.5);
    expect(parseAmount("1234.56")).toBe(1234.56);
    expect(parseAmount("1,234.56")).toBe(1234.56);
  });

  it("accepts a trailing separator while the user is typing", () => {
    expect(parseAmount("12,")).toBe(12);
    expect(parseAmount("12.")).toBe(12);
  });

  it("keeps the sign", () => {
    expect(parseAmount("-12,50")).toBe(-12.5);
    expect(parseAmount("+12,50")).toBe(12.5);
  });

  it("refuses what is not an amount instead of guessing", () => {
    for (const input of [
      "",
      "   ",
      "abc",
      "12,50 euro",
      "12,505",
      "1,234",
      "0.125",
      "1.2345",
      "12,34,56",
      "1.23.45",
      "1,234,56.7",
      "1.234.56,7",
      "12,5,0",
      ".",
      ",",
      "-",
    ]) {
      expect(parseAmount(input), input).toBeNull();
    }
  });
});

describe("formatAmountInput", () => {
  it("writes a decimal comma with two decimals", () => {
    expect(formatAmountInput(10.3)).toBe("10,30");
    expect(formatAmountInput(0)).toBe("0,00");
    expect(formatAmountInput(1234.567)).toBe("1234,57");
    expect(parseAmount(formatAmountInput(1234.56))).toBe(1234.56);
  });
});

describe("invalidAmountField", () => {
  it("flags text that is not an amount", () => {
    expect(invalidAmountField("12,50")).toBe(false);
    expect(invalidAmountField("12,505")).toBe(true);
    expect(invalidAmountField("twaalf")).toBe(true);
  });

  it("lets an empty field pass unless it is required", () => {
    expect(invalidAmountField("")).toBe(false);
    expect(invalidAmountField(" ", { required: true })).toBe(true);
  });
});

describe("formatFormAmount", () => {
  // The bug: the result line and the receipt totals said "€12.50".
  it("formats a form amount in nl-NL notation", () => {
    expect(formatFormAmount("12.5")).toBe("€ 12,50");
    expect(formatFormAmount("1234.56")).toBe("€ 1.234,56");
    expect(formatFormAmount("0")).toBe("€ 0,00");
    expect(formatFormAmount("12,5")).toBe("€ 12,50");
    expect(formatFormAmount("1.234,56")).toBe("€ 1.234,56");
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
