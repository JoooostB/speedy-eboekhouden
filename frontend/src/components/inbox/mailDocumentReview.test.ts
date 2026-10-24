import { describe, it, expect } from "vitest";
import type { BankLineCandidate, InvoiceAnalyzeResponse, InvoiceData, MailDocument } from "../../api/types";
import {
  INVOICE_FILE_ACCEPT,
  archiveFolderHelperText,
  archiveResultLine,
  autoArchiveFolderPath,
  bankLineChoices,
  bankLineOptionLabel,
  buildAnalyzedInvoiceFromDocument,
  documentFile,
  isAcceptedInvoiceFile,
  isOpenBooking,
  mediaTypeFromName,
  previewKind,
  submitButtonLabel,
  NO_MATCH_ALERT_TEXT,
} from "./mailDocumentReview";

const invoice = (overrides: Partial<InvoiceData> = {}): InvoiceData => ({
  leverancier: "Anthropic, PBC",
  factuurnummer: "INV-0042",
  datum: "2026-10-12",
  bedragExclBtw: 18,
  bedragInclBtw: 21.78,
  btwBedrag: 3.78,
  btwPercentage: 21,
  omschrijving: "Claude API",
  grootboekcode: "4000",
  btwCode: "HOOG_INK_21",
  isReverseCharge: false,
  documentType: "factuur",
  isReceipt: false,
  receiptReason: "",
  currency: "EUR",
  confidence: 0.9,
  redenering: "",
  belastingAdvies: [],
  ...overrides,
});

const candidate = (id: number, overrides: Partial<BankLineCandidate> = {}): BankLineCandidate => ({
  id,
  datum: "2026-10-13T00:00:00",
  bedrag: -21.78,
  omschrijving: `ANTHROPIC PBC betaling ${id}`,
  score: 100 - id,
  ...overrides,
});

const analysis = (overrides: Partial<InvoiceAnalyzeResponse> = {}): InvoiceAnalyzeResponse => ({
  invoice: invoice(),
  filename: "Kachelbrandje.pdf",
  uploadKey: "",
  pdfUrl: "",
  pdfHash: "abc",
  matchedRelation: { id: 55, code: "R1", bedrijf: "Anthropic" },
  crediteurenId: 1600,
  matchedBankLine: candidate(1),
  bankLineCandidates: [candidate(1), candidate(2)],
  ...overrides,
});

const document = (overrides: Partial<MailDocument> = {}): MailDocument => ({
  id: 501,
  naam: "Kachelbrandje.pdf",
  datum: "2026-10-24T15:43:35.67",
  size: 228310,
  mediaType: "application/pdf",
  koppelCount: 0,
  status: "matched",
  title: "Anthropic · 12 okt 2026 · € 21,78",
  subtitle: "Factuur INV-0042 · Kachelbrandje.pdf",
  ignored: false,
  documentType: "factuur",
  analyzedAt: "2026-10-24T16:00:00Z",
  analysis: analysis(),
  match: candidate(1),
  candidates: [candidate(1), candidate(2)],
  ...overrides,
});

const pdfFile = () => new File([new Uint8Array([0x25, 0x50, 0x44, 0x46])], "Kachelbrandje.pdf", { type: "application/pdf" });

describe("accepted files", () => {
  it("lists PDF, JPG and PNG by extension and media type", () => {
    expect(INVOICE_FILE_ACCEPT.split(",")).toEqual([
      ".pdf",
      ".jpg",
      ".jpeg",
      ".png",
      "application/pdf",
      "image/jpeg",
      "image/png",
    ]);
  });

  it("maps extensions to media types, case-insensitively", () => {
    expect(mediaTypeFromName("bon.JPG")).toBe("image/jpeg");
    expect(mediaTypeFromName("scan.jpeg")).toBe("image/jpeg");
    expect(mediaTypeFromName("logo.png")).toBe("image/png");
    expect(mediaTypeFromName("factuur.pdf")).toBe("application/pdf");
    expect(mediaTypeFromName("ubl.xml")).toBe("");
    expect(mediaTypeFromName("noext")).toBe("");
  });

  it("accepts by type, falls back to the name, refuses the rest", () => {
    expect(isAcceptedInvoiceFile({ type: "image/png", name: "x" })).toBe(true);
    expect(isAcceptedInvoiceFile({ type: "", name: "bon.jpg" })).toBe(true);
    expect(isAcceptedInvoiceFile({ type: "application/xml", name: "ubl.xml" })).toBe(false);
    expect(isAcceptedInvoiceFile({ type: "", name: "notes.txt" })).toBe(false);
  });
});

describe("documentFile", () => {
  it("names the file like the document and types it from the blob", () => {
    const file = documentFile(document(), new Blob(["x"], { type: "application/pdf" }));
    expect(file.name).toBe("Kachelbrandje.pdf");
    expect(file.type).toBe("application/pdf");
  });

  it("falls back to the listing's media type, then to the extension", () => {
    expect(documentFile({ naam: "bon.jpg", mediaType: "image/jpeg" }, new Blob(["x"])).type).toBe("image/jpeg");
    expect(documentFile({ naam: "bon.png", mediaType: "" }, new Blob(["x"])).type).toBe("image/png");
  });
});

describe("previewKind", () => {
  it("previews images with <img> and everything else as PDF", () => {
    expect(previewKind("image/jpeg", "bon.jpg")).toBe("image");
    expect(previewKind("image/png", "x")).toBe("image");
    expect(previewKind("application/pdf", "f.pdf")).toBe("pdf");
    expect(previewKind(undefined, "scan.jpeg")).toBe("image");
    expect(previewKind("", "factuur.pdf")).toBe("pdf");
  });
});

describe("buildAnalyzedInvoiceFromDocument", () => {
  const opts = { mailFolderName: "Inbox" };

  it("returns null for a document without analysis", () => {
    expect(buildAnalyzedInvoiceFromDocument(document({ analysis: undefined }), pdfFile(), "blob:1", opts)).toBeNull();
  });

  it("carries the analysis, the file, the object URL and the document fields", () => {
    const file = pdfFile();
    const out = buildAnalyzedInvoiceFromDocument(document(), file, "blob:1", opts)!;
    expect(out.invoice).toEqual(invoice());
    expect(out.matchedRelation).toEqual({ id: 55, code: "R1", bedrijf: "Anthropic" });
    expect(out.crediteurenId).toBe(1600);
    expect(out.pdfHash).toBe("abc");
    expect(out.localFile).toBe(file);
    expect(out.localPdfUrl).toBe("blob:1");
    expect(out.archiveFileId).toBe(501);
    expect(out.mailFolderName).toBe("Inbox");
    expect(out.documentTitle).toBe("Anthropic · 12 okt 2026 · € 21,78");
    expect(out.filename).toBe("Kachelbrandje.pdf");
  });

  it("never uses R2 for a mail-folder document", () => {
    const out = buildAnalyzedInvoiceFromDocument(
      document({ analysis: analysis({ uploadKey: "uploads/x/document.pdf", pdfUrl: "https://r2/x" }) }),
      pdfFile(),
      "blob:1",
      opts,
    )!;
    expect(out.uploadKey).toBe("");
    expect(out.pdfUrl).toBe("");
  });

  it("takes the match and candidates from the document's fresh data", () => {
    // The list reassigned the line to another document: the cached analysis
    // still names it, the document no longer does.
    const doc = document({
      status: "unmatched",
      match: undefined,
      candidates: [candidate(2)],
      analysis: analysis({ matchedBankLine: candidate(1), bankLineCandidates: [candidate(1), candidate(2)] }),
    });
    const out = buildAnalyzedInvoiceFromDocument(doc, pdfFile(), "blob:1", opts)!;
    expect(out.matchedBankLine).toBeNull();
    expect(out.bankLineCandidates).toEqual([candidate(2)]);
  });

  it("falls back to the analysis when the document carries no match fields", () => {
    const doc = document({ match: undefined, candidates: undefined });
    const out = buildAnalyzedInvoiceFromDocument(doc, pdfFile(), "blob:1", opts)!;
    expect(out.matchedBankLine).toEqual(candidate(1));
    expect(out.bankLineCandidates).toEqual([candidate(1), candidate(2)]);
  });

  it("forces a bank line as the match and heads the candidates with it", () => {
    const out = buildAnalyzedInvoiceFromDocument(document(), pdfFile(), "blob:1", {
      ...opts,
      bankLine: { id: 2, datum: "2026-10-13T00:00:00", bedrag: -21.78, omschrijving: "line 2" },
    })!;
    expect(out.matchedBankLine?.id).toBe(2);
    expect(out.bankLineCandidates?.map((c) => c.id)).toEqual([2, 1]);
    // The known candidate keeps its score.
    expect(out.bankLineCandidates?.[0].score).toBe(98);
  });

  it("adds a forced bank line that is not among the candidates", () => {
    const line = { id: 9, datum: "2026-10-14T00:00:00", bedrag: -21.78, omschrijving: "other" };
    const out = buildAnalyzedInvoiceFromDocument(document(), pdfFile(), "blob:1", { ...opts, bankLine: line })!;
    expect(out.matchedBankLine).toEqual(line);
    expect(out.bankLineCandidates?.map((c) => c.id)).toEqual([9, 1, 2]);
  });

  it("prefers the document's duplicate marker", () => {
    const marker = { mutNr: 1234, submittedAt: "2026-10-03T10:00:00Z" };
    const out = buildAnalyzedInvoiceFromDocument(document({ alreadySubmitted: marker }), pdfFile(), "blob:1", opts)!;
    expect(out.alreadySubmitted).toEqual(marker);
  });
});

describe("bankLineChoices", () => {
  it("puts the match first and drops duplicates", () => {
    const choices = bankLineChoices({ matchedBankLine: candidate(2), bankLineCandidates: [candidate(1), candidate(2)] });
    expect(choices.map((c) => c.id)).toEqual([2, 1]);
  });

  it("works without a match or without candidates", () => {
    expect(bankLineChoices({ matchedBankLine: null, bankLineCandidates: [candidate(3)] }).map((c) => c.id)).toEqual([3]);
    expect(bankLineChoices({ matchedBankLine: candidate(4) }).map((c) => c.id)).toEqual([4]);
    expect(bankLineChoices({ matchedBankLine: null })).toEqual([]);
  });
});

describe("bankLineOptionLabel", () => {
  it("joins date, amount and description with middle dots", () => {
    expect(bankLineOptionLabel(candidate(1))).toBe("13 okt 2026 · € -21,78 · ANTHROPIC PBC betaling 1");
  });

  it("cuts a long description with an ellipsis and skips an empty one", () => {
    const long = "A".repeat(80);
    expect(bankLineOptionLabel(candidate(1, { omschrijving: long }))).toBe(`13 okt 2026 · € -21,78 · ${"A".repeat(59)}…`);
    expect(bankLineOptionLabel(candidate(1, { omschrijving: "  " }))).toBe("13 okt 2026 · € -21,78");
  });
});

describe("isOpenBooking and the dialog texts", () => {
  it("is an open booking only for a mail-folder invoice without a bank line", () => {
    expect(isOpenBooking({ archiveFileId: 501, isReceipt: false, importId: 0 })).toBe(true);
    expect(isOpenBooking({ archiveFileId: 501, isReceipt: false, importId: 42 })).toBe(false);
    expect(isOpenBooking({ archiveFileId: 501, isReceipt: true, importId: 0 })).toBe(false);
    expect(isOpenBooking({ isReceipt: false, importId: 0 })).toBe(false);
    expect(isOpenBooking({ archiveFileId: 0, isReceipt: false, importId: 0 })).toBe(false);
  });

  it("uses the flow-4 alert text verbatim", () => {
    expect(NO_MATCH_ALERT_TEXT).toBe(
      "Nog geen betaling gevonden. We boeken dit als openstaande factuur. Zodra de betaling op je afschrift staat, stelt Speedy voor om hem te koppelen.",
    );
  });

  it("labels the submit button", () => {
    const base = { submitting: false, progress: 0, total: 1, blockedCount: 0, openBooking: false };
    expect(submitButtonLabel(base)).toBe("Boeken");
    expect(submitButtonLabel({ ...base, openBooking: true })).toBe("Boeken als openstaand");
    expect(submitButtonLabel({ ...base, total: 3 })).toBe("Alles boeken (3)");
    expect(submitButtonLabel({ ...base, total: 3, blockedCount: 1 })).toBe("Boek 2 van 3");
    expect(submitButtonLabel({ ...base, total: 3, submitting: true, progress: 1 })).toBe("Verwerken (1/3)...");
    // Several cards keep the batch wording even when they are open bookings.
    expect(submitButtonLabel({ ...base, total: 2, openBooking: true })).toBe("Alles boeken (2)");
  });
});

describe("automatic folder", () => {
  const now = new Date(2026, 9, 24);

  it("names Facturen/<jaar>/<NN Maand> for the booking date", () => {
    expect(autoArchiveFolderPath("2026-10-12", now)).toBe("Facturen/2026/10 Oktober");
    expect(autoArchiveFolderPath("2025-01-03", now)).toBe("Facturen/2025/01 Januari");
    expect(autoArchiveFolderPath("2026-12-31", now)).toBe("Facturen/2026/12 December");
  });

  it("falls back to today for an unreadable date", () => {
    expect(autoArchiveFolderPath("", now)).toBe("Facturen/2026/10 Oktober");
    expect(autoArchiveFolderPath("geen datum", now)).toBe("Facturen/2026/10 Oktober");
  });

  it("explains the automatic folder only while none is picked", () => {
    expect(archiveFolderHelperText({ folderPicked: false, datum: "2026-10-12", mailFolderName: "Inbox" }, now)).toBe(
      "Geen map gekozen: Facturen/2026/10 Oktober. Het bestand gaat uit je mailmap Inbox.",
    );
    expect(archiveFolderHelperText({ folderPicked: true, datum: "2026-10-12", mailFolderName: "Inbox" }, now)).toBe(
      "Het bestand gaat uit je mailmap Inbox.",
    );
    expect(archiveFolderHelperText({ folderPicked: true, datum: "2026-10-12", mailFolderName: "" }, now)).toBe(
      "Het bestand gaat uit je mailmap.",
    );
  });
});

describe("archiveResultLine", () => {
  it("reports the move for a booked document", () => {
    expect(archiveResultLine({ moved: true, targetFolderPath: "Facturen / 2026 / 10 Oktober" }, false)).toBe(
      "Geboekt · verplaatst naar Facturen / 2026 / 10 Oktober",
    );
  });

  it("names an open booking", () => {
    expect(archiveResultLine({ moved: false }, true)).toBe("Geboekt als openstaande factuur");
    expect(archiveResultLine({ moved: true, targetFolderPath: "Facturen" }, true)).toBe(
      "Geboekt als openstaande factuur · verplaatst naar Facturen",
    );
  });

  it("leaves the move out when the file did not move", () => {
    expect(archiveResultLine({ moved: false, targetFolderPath: "Facturen" }, false)).toBe("Geboekt");
    expect(archiveResultLine({}, false)).toBe("Geboekt");
  });
});
