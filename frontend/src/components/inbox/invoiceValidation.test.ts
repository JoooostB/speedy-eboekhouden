import { describe, it, expect } from "vitest";
import { SHARED_BANK_LINE_BLOCKER, invoiceBlocker } from "./invoiceValidation";

describe("invoiceBlocker — factuur mode", () => {
  const factuur = (overrides: Partial<Parameters<typeof invoiceBlocker>[0]> = {}) =>
    invoiceBlocker({
      hasLedgerAccount: true,
      hasRelation: true,
      hasImportId: true,
      isReceipt: false,
      ...overrides,
    });

  it("returns null when all fields are present", () => {
    expect(factuur()).toBeNull();
  });

  it("blocks when no ledger account is selected", () => {
    expect(factuur({ hasLedgerAccount: false })).toBe("Geen tegenrekening");
  });

  it("blocks when no relation is selected", () => {
    expect(factuur({ hasRelation: false })).toBe("Geen relatie");
  });

  it("blocks a row whose amounts do not read as amounts", () => {
    expect(factuur({ amountsValid: false })).toBe("Bedrag ontbreekt of klopt niet");
    expect(factuur({ amountsValid: true })).toBeNull();
  });

  it("blocks a credit note in either mode, even when complete", () => {
    expect(factuur({ isCreditnota: true })).toBe("Een creditnota boek je voorlopig zelf in e-Boekhouden");
    expect(factuur({ isCreditnota: true, isReceipt: true })).toBe("Een creditnota boek je voorlopig zelf in e-Boekhouden");
  });

  it("does NOT require a bank line for factuur mode", () => {
    // Factuur without bank line is legitimate — the user might link the
    // payment later. This was important for the partial-submit feature.
    expect(factuur({ hasImportId: false })).toBeNull();
  });

  it("reports tegenrekening as the highest-priority blocker", () => {
    // When multiple things are missing, surface tegenrekening first
    // because it's the field most commonly forgotten and it determines
    // the rest of the booking.
    expect(
      factuur({ hasLedgerAccount: false, hasRelation: false }),
    ).toBe("Geen tegenrekening");
  });
});

describe("invoiceBlocker — bonnetje mode", () => {
  const bonnetje = (overrides: Partial<Parameters<typeof invoiceBlocker>[0]> = {}) =>
    invoiceBlocker({
      hasLedgerAccount: true,
      hasRelation: false, // bonnetjes never need a relation
      hasImportId: true,
      isReceipt: true,
      ...overrides,
    });

  it("returns null when ledger and bank line are present", () => {
    expect(bonnetje()).toBeNull();
  });

  it("does NOT require a relation for bonnetje mode", () => {
    // Bonnetjes are booked as Geld uitgegeven directly against the bank
    // account — no leverancier relation needed. Regression: previously
    // any missing relation blocked the whole batch.
    expect(bonnetje({ hasRelation: false })).toBeNull();
  });

  it("blocks when no bank line is linked", () => {
    expect(bonnetje({ hasImportId: false })).toBe(
      "Geen afschriftregel gekoppeld",
    );
  });

  it("blocks when no ledger account is selected", () => {
    expect(bonnetje({ hasLedgerAccount: false })).toBe("Geen tegenrekening");
  });
});

describe("invoiceBlocker — toggling between modes", () => {
  it("makes a factuur with no relation valid by switching to bonnetje", () => {
    const base = {
      hasLedgerAccount: true,
      hasRelation: false,
      hasImportId: true,
    };
    expect(invoiceBlocker({ ...base, isReceipt: false })).toBe("Geen relatie");
    expect(invoiceBlocker({ ...base, isReceipt: true })).toBeNull();
  });

  it("makes a bonnetje with no bank line valid by switching to factuur", () => {
    // Provided the user also has a relation in factuur mode.
    const base = {
      hasLedgerAccount: true,
      hasRelation: true,
      hasImportId: false,
    };
    expect(invoiceBlocker({ ...base, isReceipt: true })).toBe(
      "Geen afschriftregel gekoppeld",
    );
    expect(invoiceBlocker({ ...base, isReceipt: false })).toBeNull();
  });
});

describe("invoiceBlocker — datum", () => {
  const complete = { hasLedgerAccount: true, hasRelation: true, hasImportId: true };

  it("blocks a row without a valid date, in either mode", () => {
    expect(invoiceBlocker({ ...complete, isReceipt: false, hasDatum: false })).toBe("Geen datum");
    expect(invoiceBlocker({ ...complete, isReceipt: true, hasDatum: false })).toBe("Geen datum");
  });

  it("does not block a row with a date, or when the date is not checked", () => {
    expect(invoiceBlocker({ ...complete, isReceipt: false, hasDatum: true })).toBeNull();
    expect(invoiceBlocker({ ...complete, isReceipt: false })).toBeNull();
  });

  it("reports the other blockers first", () => {
    expect(invoiceBlocker({ ...complete, hasRelation: false, isReceipt: false, hasDatum: false })).toBe("Geen relatie");
    expect(invoiceBlocker({ ...complete, isReceipt: false, amountsValid: false, hasDatum: false })).toBe(
      "Bedrag ontbreekt of klopt niet",
    );
  });
});

describe("invoiceBlocker — a bank line shared with another row", () => {
  const complete = { hasLedgerAccount: true, hasRelation: true, hasImportId: true };

  it("blocks a factuur and a bonnetje alike", () => {
    expect(invoiceBlocker({ ...complete, isReceipt: false, sharesBankLine: true })).toBe(SHARED_BANK_LINE_BLOCKER);
    expect(invoiceBlocker({ ...complete, isReceipt: true, sharesBankLine: true })).toBe(
      "Afschriftregel is ook aan een ander document gekoppeld",
    );
    expect(invoiceBlocker({ ...complete, isReceipt: false, sharesBankLine: false })).toBeNull();
  });

  it("comes after the ledger check and before the relation check", () => {
    expect(invoiceBlocker({ ...complete, hasLedgerAccount: false, isReceipt: false, sharesBankLine: true })).toBe("Geen tegenrekening");
    expect(invoiceBlocker({ ...complete, hasRelation: false, isReceipt: false, sharesBankLine: true })).toBe(SHARED_BANK_LINE_BLOCKER);
  });
});
