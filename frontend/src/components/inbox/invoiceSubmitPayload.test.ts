import { describe, it, expect } from "vitest";
import {
  buildFullSubmitPayload,
  buildReceiptSubmitPayload,
  isArchiveSource,
  receiptExtraLines,
  type SubmitInput,
  type SubmitSource,
} from "./invoiceSubmitPayload";

const uploadSource: SubmitSource = {
  uploadKey: "uploads/abc/document.pdf",
  filename: "factuur.pdf",
  pdfHash: "hash-1",
  crediteurenId: 1600,
  invoice: { documentType: "factuur" },
};

const archiveSource: SubmitSource = {
  uploadKey: "",
  filename: "Voorbeeld-Bijlage.pdf",
  pdfHash: "hash-2",
  crediteurenId: 1600,
  archiveFileId: 501,
  invoice: { documentType: "factuur" },
};

const input = (overrides: Partial<SubmitInput> = {}): SubmitInput => ({
  source: uploadSource,
  datum: "2026-10-12",
  leverancier: "Anthropic, PBC",
  factuurnummer: "INV-0042",
  omschrijving: "Claude API",
  bedragExcl: 18,
  bedragIncl: 21.78,
  btwBedrag: 3.78,
  btwCode: "HOOG_INK_21",
  relatieId: 55,
  tegenRekeningId: 4000,
  importId: 0,
  folderId: 0,
  extraLines: [],
  ...overrides,
});

const common = {
  datum: "2026-10-12",
  leverancier: "Anthropic, PBC",
  omschrijving: "Claude API",
  bedragExcl: 18,
  bedragIncl: 21.78,
  btwBedrag: 3.78,
  btwCode: "HOOG_INK_21",
  tegenRekeningId: 4000,
};

describe("isArchiveSource", () => {
  it("is true only for a positive archive file id", () => {
    expect(isArchiveSource(archiveSource)).toBe(true);
    expect(isArchiveSource(uploadSource)).toBe(false);
    expect(isArchiveSource({ archiveFileId: 0 })).toBe(false);
  });
});

describe("buildFullSubmitPayload for an upload", () => {
  it("sends the upload key, file name and hash, nothing optional", () => {
    expect(buildFullSubmitPayload(input())).toEqual({
      ...common,
      factuurnummer: "INV-0042",
      inEx: "EX",
      relatieId: 55,
      rekeningId: 1600,
      uploadKey: "uploads/abc/document.pdf",
      filename: "factuur.pdf",
      pdfHash: "hash-1",
    });
  });

  it("adds importId, pdfBase64 and folderId only when set", () => {
    const body = buildFullSubmitPayload(input({ importId: 42, folderId: 202, pdfBase64: "QUJD" }));
    expect(body.importId).toBe(42);
    expect(body.folderId).toBe(202);
    expect(body.pdfBase64).toBe("QUJD");
    expect(body).not.toHaveProperty("archiveFileId");
    expect(body).not.toHaveProperty("targetFolderId");
    expect(body).not.toHaveProperty("documentType");
  });

  it("falls back to rekeningId 0 without a crediteuren account", () => {
    expect(buildFullSubmitPayload(input({ source: { ...uploadSource, crediteurenId: 0 } })).rekeningId).toBe(0);
  });
});

describe("buildFullSubmitPayload for a mail-folder document", () => {
  it("sends archiveFileId and never the file, the upload key or the file name", () => {
    const body = buildFullSubmitPayload(input({ source: archiveSource, importId: 42, folderId: 202, pdfBase64: "QUJD" }));
    expect(body).toEqual({
      ...common,
      factuurnummer: "INV-0042",
      inEx: "EX",
      relatieId: 55,
      rekeningId: 1600,
      pdfHash: "hash-2",
      importId: 42,
      archiveFileId: 501,
      targetFolderId: 202,
    });
    expect(body).not.toHaveProperty("pdfBase64");
    expect(body).not.toHaveProperty("uploadKey");
    expect(body).not.toHaveProperty("filename");
    expect(body).not.toHaveProperty("folderId");
  });

  it("omits targetFolderId for the automatic folder and importId without a bank line", () => {
    const body = buildFullSubmitPayload(input({ source: archiveSource }));
    expect(body.archiveFileId).toBe(501);
    expect(body).not.toHaveProperty("targetFolderId");
    expect(body).not.toHaveProperty("importId");
  });

  it("marks a creditnota and nothing else", () => {
    const creditnota = { ...archiveSource, invoice: { documentType: "creditnota" as const } };
    expect(buildFullSubmitPayload(input({ source: creditnota })).documentType).toBe("creditnota");
    expect(buildFullSubmitPayload(input({ source: { ...archiveSource, invoice: { documentType: "bonnetje" } } }))).not.toHaveProperty(
      "documentType",
    );
    expect(buildFullSubmitPayload(input({ source: { ...uploadSource, invoice: { documentType: "creditnota" } } }))).not.toHaveProperty(
      "documentType",
    );
  });
});

describe("buildReceiptSubmitPayload", () => {
  const extraLines = [{ bedragIncl: 5, bedragExcl: 5, btwBedrag: 0, btwCode: "GEEN", omschrijving: "Fooi" }];

  it("sends the upload fields and always the bank line", () => {
    expect(buildReceiptSubmitPayload(input({ importId: 42 }))).toEqual({
      ...common,
      uploadKey: "uploads/abc/document.pdf",
      filename: "factuur.pdf",
      importId: 42,
    });
    expect(buildReceiptSubmitPayload(input()).importId).toBe(0);
  });

  it("adds extra lines, pdfBase64 and folderId only when set", () => {
    const body = buildReceiptSubmitPayload(input({ importId: 42, folderId: 202, pdfBase64: "QUJD", extraLines }));
    expect(body.extraLines).toEqual(extraLines);
    expect(body.pdfBase64).toBe("QUJD");
    expect(body.folderId).toBe(202);
    expect(buildReceiptSubmitPayload(input())).not.toHaveProperty("extraLines");
  });

  it("sends archiveFileId and targetFolderId for a mail-folder receipt", () => {
    const body = buildReceiptSubmitPayload(input({ source: archiveSource, importId: 42, folderId: 202, pdfBase64: "QUJD", extraLines }));
    expect(body).toEqual({ ...common, importId: 42, extraLines, archiveFileId: 501, targetFolderId: 202 });
    expect(buildReceiptSubmitPayload(input({ source: archiveSource, importId: 42 }))).toEqual({
      ...common,
      importId: 42,
      archiveFileId: 501,
    });
  });
});

describe("receiptExtraLines", () => {
  const vatRate = (code: string) => (code === "LAAG_INK_9" ? 9 : code === "HOOG_INK_21" ? 21 : 0);

  it("drops empty lines and splits BTW per line", () => {
    const out = receiptExtraLines(
      [
        { bedragIncl: "5.00", btwCode: "GEEN", omschrijving: "Fooi" },
        { bedragIncl: "", btwCode: "GEEN", omschrijving: "" },
        { bedragIncl: "10.90", btwCode: "LAAG_INK_9", omschrijving: "" },
      ],
      vatRate,
    );
    expect(out).toEqual([
      { bedragIncl: 5, bedragExcl: 5, btwBedrag: 0, btwCode: "GEEN", omschrijving: "Fooi" },
      { bedragIncl: 10.9, bedragExcl: 10, btwBedrag: 0.9, btwCode: "LAAG_INK_9", omschrijving: undefined },
    ]);
  });
});
