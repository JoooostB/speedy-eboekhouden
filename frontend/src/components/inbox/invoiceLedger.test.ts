import { describe, it, expect } from "vitest";
import type { LedgerAccount } from "../../api/types";
import { aiSuggestedLedger, initialLedger, ledgerAfterRelationChange, relationDefaultLedger } from "./invoiceLedger";

const software: LedgerAccount = { id: 11, code: "4000", omschrijving: "Software", rekeningCategorie: "KOSTEN" };
const telefoon: LedgerAccount = { id: 12, code: "4300", omschrijving: "Telefoonkosten", rekeningCategorie: "KOSTEN" };
const crediteuren: LedgerAccount = { id: 13, code: "1600", omschrijving: "Crediteuren", rekeningCategorie: "CRED" };
const bank: LedgerAccount = { id: 14, code: "1100", omschrijving: "Bank", rekeningCategorie: "BANK" };
const accounts = [software, telefoon, crediteuren, bank];

describe("relationDefaultLedger", () => {
  it("returns the active account with the relation's default id", () => {
    expect(relationDefaultLedger({ grootboekrekeningId: 12 }, accounts)).toBe(telefoon);
  });

  it("returns null without a usable default", () => {
    expect(relationDefaultLedger(null, accounts)).toBeNull();
    expect(relationDefaultLedger({}, accounts)).toBeNull();
    expect(relationDefaultLedger({ grootboekrekeningId: null }, accounts)).toBeNull();
    expect(relationDefaultLedger({ grootboekrekeningId: 0 }, accounts)).toBeNull();
    expect(relationDefaultLedger({ grootboekrekeningId: 99 }, accounts)).toBeNull();
  });

  it("never offers a crediteuren or bank account as tegenrekening", () => {
    expect(relationDefaultLedger({ grootboekrekeningId: 13 }, accounts)).toBeNull();
    expect(relationDefaultLedger({ grootboekrekeningId: 14 }, accounts)).toBeNull();
  });
});

describe("aiSuggestedLedger", () => {
  it("matches by code or description", () => {
    expect(aiSuggestedLedger("4000", accounts)).toBe(software);
    expect(aiSuggestedLedger("telefoonkosten", accounts)).toBe(telefoon);
    expect(aiSuggestedLedger("", accounts)).toBeNull();
    expect(aiSuggestedLedger("9999", accounts)).toBeNull();
  });
});

describe("initialLedger", () => {
  it("prefers the relation's default over Claude's suggestion", () => {
    expect(initialLedger({ grootboekrekeningId: 12 }, "4000", accounts)).toEqual({ account: telefoon, source: "relation" });
  });

  it("falls back to Claude's suggestion", () => {
    expect(initialLedger({ grootboekrekeningId: 0 }, "4000", accounts)).toEqual({ account: software, source: "ai" });
    expect(initialLedger(null, "4000", accounts)).toEqual({ account: software, source: "ai" });
  });

  it("is empty when neither gives an account", () => {
    expect(initialLedger(null, "", accounts)).toEqual({ account: null, source: null });
  });
});

describe("ledgerAfterRelationChange", () => {
  it("applies the new relation's default over Claude's suggestion", () => {
    expect(ledgerAfterRelationChange({ account: software, source: "ai" }, { grootboekrekeningId: 12 }, accounts))
      .toEqual({ account: telefoon, source: "relation" });
  });

  it("keeps an account the user picked by hand", () => {
    const manual = { account: software, source: "manual" as const };
    expect(ledgerAfterRelationChange(manual, { grootboekrekeningId: 12 }, accounts)).toBe(manual);
  });

  it("keeps the account when the new relation has no default", () => {
    const ai = { account: software, source: "ai" as const };
    expect(ledgerAfterRelationChange(ai, { grootboekrekeningId: 0 }, accounts)).toBe(ai);
    expect(ledgerAfterRelationChange({ account: telefoon, source: "relation" }, null, accounts))
      .toEqual({ account: telefoon, source: null });
  });
});
