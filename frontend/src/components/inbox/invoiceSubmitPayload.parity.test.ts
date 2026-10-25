import { describe, it, expect } from "vitest";
import type { ReceiptExtraLine } from "../../api/types";
import {
  buildFullSubmitPayload,
  buildReceiptSubmitPayload,
  receiptExtraLines,
  type ExtraLineEdit,
  type SubmitInput,
} from "./invoiceSubmitPayload";

/**
 * Characterisation against main (a524a1a): InvoiceReviewDialog built the
 * /invoices/submit-full and /invoices/submit-receipt bodies inline. The
 * reference builders below are main's code, copied verbatim apart from the
 * function wrapper and `inv`'s minimal shape. For an upload, the new
 * builders (fed the way the dialog on this branch feeds them) must produce
 * the same JSON, byte for byte, with one intended difference: the dialog
 * now always inlines the file (pdfBase64) for an upload, also when no
 * archive folder was picked, because the analyser no longer keeps a copy in
 * R2 (commit e3d851b).
 */

/** The part of main's InvoiceEdit the submit read. */
interface MainInvoiceEdit {
  source: { uploadKey: string; filename: string; pdfHash: string; crediteurenId: number };
  datum: string;
  leverancier: string;
  factuurnummer: string;
  omschrijving: string;
  bedragExcl: string;
  bedragIncl: string;
  btwBedrag: string;
  btwCode: string;
  relation: { id: number } | null;
  ledgerAccount: { id: number } | null;
  importId: number;
  extraLines: ExtraLineEdit[];
  archiveFolder: { id: number } | null;
}

// A fixed rate table standing in for the dialog's vatRate(code).
const vatRate = (code: string): number => (code === "HOOG_INK_21" ? 21 : code === "LAAG_INK_9" ? 9 : 0);

// main: InvoiceReviewDialog handleSubmitAll, the factuur branch.
// pdfBase64 was only read when a folder was picked.
function mainFullBody(inv: MainInvoiceEdit, pdfBase64: string | undefined) {
  const folderId = inv.archiveFolder?.id;
  return {
    datum: inv.datum,
    leverancier: inv.leverancier,
    factuurnummer: inv.factuurnummer,
    omschrijving: inv.omschrijving,
    bedragExcl: parseFloat(inv.bedragExcl) || 0,
    bedragIncl: parseFloat(inv.bedragIncl) || 0,
    btwBedrag: parseFloat(inv.btwBedrag) || 0,
    btwCode: inv.btwCode,
    inEx: "EX",
    relatieId: inv.relation!.id,
    tegenRekeningId: inv.ledgerAccount!.id,
    rekeningId: inv.source.crediteurenId || 0,
    uploadKey: inv.source.uploadKey,
    filename: inv.source.filename,
    pdfHash: inv.source.pdfHash,
    ...(inv.importId ? { importId: inv.importId } : {}),
    ...(pdfBase64 ? { pdfBase64 } : {}),
    ...(folderId ? { folderId } : {}),
  };
}

// main: InvoiceReviewDialog handleSubmitAll, the bonnetje branch.
function mainReceiptBody(inv: MainInvoiceEdit, pdfBase64: string | undefined) {
  const folderId = inv.archiveFolder?.id;
  const extraLines = inv.extraLines
    .filter((el) => (parseFloat(el.bedragIncl) || 0) > 0)
    .map((el) => {
      const incl = parseFloat(el.bedragIncl) || 0;
      const elRate = vatRate(el.btwCode);
      const btw = elRate === 0 ? 0 : +((incl * elRate) / (100 + elRate)).toFixed(2);
      const excl = +(incl - btw).toFixed(2);
      return {
        bedragIncl: incl,
        bedragExcl: excl,
        btwBedrag: btw,
        btwCode: el.btwCode,
        omschrijving: el.omschrijving || undefined,
      };
    });
  return {
    datum: inv.datum,
    leverancier: inv.leverancier,
    omschrijving: inv.omschrijving,
    bedragExcl: parseFloat(inv.bedragExcl) || 0,
    bedragIncl: parseFloat(inv.bedragIncl) || 0,
    btwBedrag: parseFloat(inv.btwBedrag) || 0,
    btwCode: inv.btwCode,
    tegenRekeningId: inv.ledgerAccount!.id,
    uploadKey: inv.source.uploadKey,
    filename: inv.source.filename,
    importId: inv.importId,
    ...(extraLines.length > 0 ? { extraLines } : {}),
    ...(pdfBase64 ? { pdfBase64 } : {}),
    ...(folderId ? { folderId } : {}),
  };
}

// This branch: what handleSubmitAll hands the builders for an upload
// (payloadInput in InvoiceReviewDialog.tsx). pdfBase64 is read for every
// upload with a local file.
function branchInput(inv: MainInvoiceEdit, pdfBase64: string | undefined, extraLines: ReceiptExtraLine[] = []): SubmitInput {
  return {
    source: { ...inv.source, invoice: { documentType: "factuur" } },
    datum: inv.datum,
    leverancier: inv.leverancier,
    factuurnummer: inv.factuurnummer,
    omschrijving: inv.omschrijving,
    bedragExcl: parseFloat(inv.bedragExcl) || 0,
    bedragIncl: parseFloat(inv.bedragIncl) || 0,
    btwBedrag: parseFloat(inv.btwBedrag) || 0,
    btwCode: inv.btwCode,
    relatieId: inv.relation?.id ?? 0,
    tegenRekeningId: inv.ledgerAccount!.id,
    importId: inv.importId,
    folderId: inv.archiveFolder?.id ?? 0,
    pdfBase64,
    extraLines,
  };
}

const FILE_B64 = "JVBERi0xLjQKJUVPRgo="; // "%PDF-1.4\n%EOF\n", a fictitious stub

const edit = (overrides: Partial<MainInvoiceEdit> = {}): MainInvoiceEdit => ({
  // The analyser answers uploadKey "" since e3d851b; main's R2 deployment
  // sent a key here, which the payload passes through unchanged either way.
  source: { uploadKey: "", filename: "voorbeeld-factuur.pdf", pdfHash: "0f".repeat(32), crediteurenId: 1600 },
  datum: "2026-10-01",
  leverancier: "Voorbeeld Leverancier",
  factuurnummer: "VL-2026-001",
  omschrijving: "Kantoorbenodigdheden",
  bedragExcl: "100.00",
  bedragIncl: "121.00",
  btwBedrag: "21.00",
  btwCode: "HOOG_INK_21",
  relation: { id: 321 },
  ledgerAccount: { id: 4300 },
  importId: 9001,
  extraLines: [],
  archiveFolder: { id: 70001 },
  ...overrides,
});

describe("submit-full body for an upload", () => {
  const cases: Array<{ name: string; inv: MainInvoiceEdit }> = [
    { name: "folder picked, bank line linked", inv: edit() },
    { name: "folder picked, no bank line", inv: edit({ importId: 0 }) },
    { name: "folder picked, no crediteuren account", inv: edit({ source: { ...edit().source, crediteurenId: 0 } }) },
    { name: "legacy uploadKey from an R2 analysis", inv: edit({ source: { ...edit().source, uploadKey: "uploads/00000000-0000-0000-0000-000000000000/document.pdf" } }) },
    { name: "unparseable amounts fall back to 0", inv: edit({ bedragExcl: "", btwBedrag: "abc" }) },
  ];
  for (const c of cases) {
    it(`${c.name}: byte-for-byte main's JSON`, () => {
      const pdf = c.inv.archiveFolder ? FILE_B64 : undefined;
      expect(JSON.stringify(buildFullSubmitPayload(branchInput(c.inv, FILE_B64)))).toBe(
        JSON.stringify(mainFullBody(c.inv, pdf)),
      );
    });
  }

  it("no folder picked: main's JSON plus the inlined file, still without folderId (intended, e3d851b)", () => {
    const inv = edit({ archiveFolder: null });
    const now = JSON.parse(JSON.stringify(buildFullSubmitPayload(branchInput(inv, FILE_B64))));
    const before = JSON.parse(JSON.stringify(mainFullBody(inv, undefined)));
    expect(now).toEqual({ ...before, pdfBase64: FILE_B64 });
    expect(now).not.toHaveProperty("folderId");
  });

  it("no folder and no local file: exactly main's JSON", () => {
    const inv = edit({ archiveFolder: null });
    expect(JSON.stringify(buildFullSubmitPayload(branchInput(inv, undefined)))).toBe(JSON.stringify(mainFullBody(inv, undefined)));
  });
});

describe("submit-receipt body for an upload", () => {
  const receipt = (overrides: Partial<MainInvoiceEdit> = {}) =>
    edit({ factuurnummer: "", relation: null, leverancier: "Voorbeeld Restaurant", ...overrides });

  const tip: ExtraLineEdit[] = [
    { bedragIncl: "5.00", btwCode: "GEEN", omschrijving: "Fooi" },
    { bedragIncl: "", btwCode: "HOOG_INK_21", omschrijving: "leeg, valt weg" },
    { bedragIncl: "10.90", btwCode: "LAAG_INK_9", omschrijving: "" },
    { bedragIncl: "12.10", btwCode: "HOOG_INK_21", omschrijving: "Wijn" },
    { bedragIncl: "-3", btwCode: "GEEN", omschrijving: "negatief, valt weg" },
  ];

  const cases: Array<{ name: string; inv: MainInvoiceEdit }> = [
    { name: "folder picked, no extra lines", inv: receipt() },
    { name: "folder picked, tip and split lines", inv: receipt({ extraLines: tip }) },
    { name: "folder picked, only empty extra lines", inv: receipt({ extraLines: [{ bedragIncl: "0", btwCode: "GEEN", omschrijving: "" }] }) },
  ];
  for (const c of cases) {
    it(`${c.name}: byte-for-byte main's JSON`, () => {
      const body = buildReceiptSubmitPayload(branchInput(c.inv, FILE_B64, receiptExtraLines(c.inv.extraLines, vatRate)));
      expect(JSON.stringify(body)).toBe(JSON.stringify(mainReceiptBody(c.inv, FILE_B64)));
    });
  }

  it("no folder picked: main's JSON plus the inlined file, still without folderId (intended, e3d851b)", () => {
    const inv = receipt({ archiveFolder: null, extraLines: tip });
    const now = JSON.parse(
      JSON.stringify(buildReceiptSubmitPayload(branchInput(inv, FILE_B64, receiptExtraLines(inv.extraLines, vatRate)))),
    );
    const before = JSON.parse(JSON.stringify(mainReceiptBody(inv, undefined)));
    expect(now).toEqual({ ...before, pdfBase64: FILE_B64 });
    expect(now).not.toHaveProperty("folderId");
  });

  it("receiptExtraLines equals main's inline split for every line", () => {
    const now = receiptExtraLines(tip, vatRate);
    const before = mainReceiptBody(receipt({ extraLines: tip }), undefined).extraLines;
    expect(JSON.stringify(now)).toBe(JSON.stringify(before));
  });
});
