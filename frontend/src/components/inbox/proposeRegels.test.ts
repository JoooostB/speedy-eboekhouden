import { describe, expect, it } from "vitest";
import type { InvoiceRegel, LedgerAccount, SplitsAdvies } from "../../api/types";
import { proposeRegels, undoProposal } from "./proposeRegels";

const cadeaus: LedgerAccount = { id: 4500, code: "4500", omschrijving: "Personeelsgeschenken", rekeningCategorie: "KOSTEN" };
const verzend: LedgerAccount = { id: 4700, code: "4700", omschrijving: "Verzendkosten", rekeningCategorie: "KOSTEN" };
const algemeen: LedgerAccount = { id: 4000, code: "4000", omschrijving: "Algemene kosten", rekeningCategorie: "KOSTEN" };
const ledgers = [cadeaus, verzend, algemeen];

/** "Voorbeeld Cadeauwinkel": a birthday gift plus shipping, both 9 %. */
const giftRegels: InvoiceRegel[] = [
  { omschrijving: "Verjaardagscadeau", bedragIncl: 35.8, btwPercentage: 9, btwCode: "LAAG_INK_9", grootboekcode: "4500", soort: "product" },
  { omschrijving: "Verzendkosten", bedragIncl: 3.95, btwPercentage: 9, btwCode: "LAAG_INK_9", grootboekcode: "4700", soort: "verzending" },
];
const wkr: SplitsAdvies = { reden: "wkr", toelichting: "Het cadeau valt onder de WKR, de verzendkosten niet." };

describe("proposeRegels", () => {
  it("maps the proposal to editable lines with resolved ledgers", () => {
    const p = proposeRegels({ regels: giftRegels, splitsAdvies: wkr, bedragInclBtw: 39.75 }, ledgers, algemeen);
    expect(p?.advies).toEqual(wkr);
    expect(p?.regels.map(({ key: _k, ...r }) => r)).toEqual([
      { omschrijving: "Verjaardagscadeau", ledgerAccount: cadeaus, btwCode: "LAAG_INK_9", bedragIncl: "35,80" },
      { omschrijving: "Verzendkosten", ledgerAccount: verzend, btwCode: "LAAG_INK_9", bedragIncl: "3,95" },
    ]);
    expect(new Set(p?.regels.map((r) => r.key)).size).toBe(2);
  });

  it("falls back to the main ledger for a code the administration does not have", () => {
    const regels = [giftRegels[0], { ...giftRegels[1], grootboekcode: "9999" }];
    const p = proposeRegels({ regels, splitsAdvies: wkr, bedragInclBtw: 39.75 }, ledgers, algemeen);
    expect(p?.regels[1].ledgerAccount).toEqual(algemeen);
    expect(p?.regels[0].ledgerAccount).toEqual(cadeaus);
  });

  it("resolves a ledger by description as well, like the single-line suggestion", () => {
    const regels = [giftRegels[0], { ...giftRegels[1], grootboekcode: "Verzendkosten" }];
    expect(proposeRegels({ regels, splitsAdvies: wkr, bedragInclBtw: 39.75 }, ledgers, null)?.regels[1].ledgerAccount).toEqual(verzend);
  });

  it("is null without a proposal, with one line, without advice, or when the lines do not add up", () => {
    expect(proposeRegels({ bedragInclBtw: 39.75 }, ledgers, algemeen)).toBeNull();
    expect(proposeRegels({ regels: [], splitsAdvies: wkr, bedragInclBtw: 39.75 }, ledgers, algemeen)).toBeNull();
    expect(proposeRegels({ regels: [giftRegels[0]], splitsAdvies: wkr, bedragInclBtw: 35.8 }, ledgers, algemeen)).toBeNull();
    expect(proposeRegels({ regels: giftRegels, bedragInclBtw: 39.75 }, ledgers, algemeen)).toBeNull();
    expect(proposeRegels({ regels: giftRegels, splitsAdvies: { reden: "wkr", toelichting: "  " }, bedragInclBtw: 39.75 }, ledgers, algemeen)).toBeNull();
    expect(proposeRegels({ regels: giftRegels, splitsAdvies: wkr, bedragInclBtw: 39.77 }, ledgers, algemeen)).toBeNull();
    expect(proposeRegels({ regels: giftRegels, splitsAdvies: wkr, bedragInclBtw: 39.76 }, ledgers, algemeen)).not.toBeNull();
  });

  it("refuses a line without a positive amount", () => {
    const regels = [giftRegels[0], { ...giftRegels[1], bedragIncl: 0 }, { ...giftRegels[1], bedragIncl: 3.95 }];
    expect(proposeRegels({ regels, splitsAdvies: wkr, bedragInclBtw: 39.75 }, ledgers, algemeen)).toBeNull();
  });
});

describe("undoProposal", () => {
  it("restores the single-line values and clears the lines and the proposal", () => {
    const proposal = proposeRegels({ regels: giftRegels, splitsAdvies: wkr, bedragInclBtw: 39.75 }, ledgers, algemeen)!;
    const original = {
      ledgerAccount: algemeen,
      ledgerSource: "relation" as const,
      btwCode: "LAAG_INK_9",
      bedragExcl: "36,47",
      bedragIncl: "39,75",
      btwBedrag: "3,28",
    };
    const row = {
      ...original,
      leverancier: "Voorbeeld Cadeauwinkel",
      ledgerAccount: cadeaus, // line 1 mirrored while split
      ledgerSource: "manual" as const,
      btwBedrag: "3,29",
      regels: proposal.regels,
      proposal,
      proposalEdited: true,
    };
    expect(undoProposal(row, original)).toEqual({
      ...original,
      leverancier: "Voorbeeld Cadeauwinkel",
      regels: null,
      proposal: null,
      proposalEdited: false,
    });
  });
});
