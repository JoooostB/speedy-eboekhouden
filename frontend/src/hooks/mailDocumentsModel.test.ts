import { describe, expect, it } from "vitest";
import type { MailDocument, MailDocumentAnalyzeResponse, MailDocumentStatus } from "../api/types";
import {
  analyzeAllConfirmText,
  analyzeAllTargets,
  analyzeErrorMessage,
  applyAnalysis,
  documentAmount,
  documentAriaLabel,
  documentDate,
  documentSubtitle,
  formatDocumentAmount,
  formatMoney,
  formatShortDate,
  groupDocuments,
  inboxSubtitle,
  isAnalyzeBusyError,
  isFatalAnalyzeError,
  nextFocusId,
  parseDate,
  pendingDocumentCount,
  primaryActionFor,
  sectionsFromSnapshot,
  summarizeRun,
  CREDITNOTA_NOT_BOOKABLE,
} from "./mailDocumentsModel";

const doc = (id: number, status: MailDocumentStatus, overrides: Partial<MailDocument> = {}): MailDocument => ({
  id,
  naam: `file_${id}.pdf`,
  datum: "2026-10-24T15:43:35.67",
  size: 228310,
  mediaType: "application/pdf",
  koppelCount: status === "linked" ? 1 : 0,
  status,
  title: `file ${id}`,
  subtitle: "Ontvangen 24 okt 15:43 · PDF · 223 kB",
  ignored: status === "ignored",
  ...overrides,
});

const analyzed = (id: number, status: MailDocumentStatus, overrides: Partial<MailDocument> = {}): MailDocument =>
  doc(id, status, {
    analyzedAt: "2026-10-24T16:00:00Z",
    title: "Acme · 12 okt 2026 · € 21,78",
    subtitle: "Factuur INV-0042 · file.pdf",
    ...overrides,
  });

describe("groupDocuments", () => {
  it("puts each status in its section and keeps the list order", () => {
    const docs = [
      doc(1, "new"),
      analyzed(2, "matched"),
      doc(3, "linked"),
      doc(4, "ignored"),
      analyzed(5, "unmatched"),
      analyzed(6, "receipt_waiting"),
      analyzed(7, "duplicate"),
      analyzed(8, "not_invoice"),
      doc(9, "junk", { mediaType: "image/png", size: 800 }),
      doc(10, "unsupported", { mediaType: "", naam: "ubl.xml" }),
      analyzed(11, "matched"),
    ];
    const s = groupDocuments(docs);
    expect(s.ready.map((d) => d.id)).toEqual([2, 11]);
    expect(s.todo.map((d) => d.id)).toEqual([1, 5, 6, 7, 8, 9, 10]);
    expect(s.processed.map((d) => d.id)).toEqual([3]);
    expect(s.ignored.map((d) => d.id)).toEqual([4]);
  });
});

describe("sectionsFromSnapshot", () => {
  it("keeps a row in its snapshot section while showing its live data", () => {
    const snapshot = [doc(1, "new"), doc(2, "new")];
    const live = [analyzed(1, "matched"), doc(2, "new")];
    const s = sectionsFromSnapshot(snapshot, live);
    expect(s.ready).toEqual([]);
    expect(s.todo.map((d) => d.id)).toEqual([1, 2]);
    expect(s.todo[0].status).toBe("matched");
    expect(s.todo[0].title).toBe("Acme · 12 okt 2026 · € 21,78");
  });

  it("drops rows that disappeared and appends rows that arrived", () => {
    const snapshot = [doc(1, "new"), doc(2, "new")];
    const live = [doc(2, "new"), analyzed(3, "matched")];
    const s = sectionsFromSnapshot(snapshot, live);
    expect(s.todo.map((d) => d.id)).toEqual([2]);
    expect(s.ready.map((d) => d.id)).toEqual([3]);
  });

  it("lets a row the user ignored or un-ignored move at once", () => {
    const snapshot = [doc(1, "new"), doc(2, "ignored")];
    const live = [doc(1, "ignored"), doc(2, "new")];
    const s = sectionsFromSnapshot(snapshot, live);
    expect(s.ignored.map((d) => d.id)).toEqual([1]);
    expect(s.todo.map((d) => d.id)).toEqual([2]);
  });
});

describe("analyzeAllTargets", () => {
  it("skips junk, ignored, linked, unsupported and already analysed documents", () => {
    const docs = [
      doc(1, "new"),
      doc(2, "junk", { mediaType: "image/png" }),
      doc(3, "ignored"),
      doc(4, "linked"),
      doc(5, "unsupported", { mediaType: "" }),
      analyzed(6, "matched"),
      analyzed(7, "not_invoice"),
      analyzed(8, "duplicate"),
      doc(9, "new", { mediaType: "image/jpeg" }),
    ];
    expect(analyzeAllTargets(docs)).toEqual([1, 9]);
  });

  it("includes a document whose earlier analysis failed (still new)", () => {
    expect(analyzeAllTargets([doc(1, "new")])).toEqual([1]);
  });
});

describe("pendingDocumentCount", () => {
  it("counts everything except linked, ignored and unsupported", () => {
    const docs = [
      doc(1, "new"),
      analyzed(2, "matched"),
      doc(3, "linked"),
      doc(4, "ignored"),
      doc(5, "unsupported", { mediaType: "" }),
      doc(6, "junk", { mediaType: "image/png" }),
      analyzed(7, "not_invoice"),
    ];
    expect(pendingDocumentCount(docs)).toBe(4);
  });
});

describe("applyAnalysis", () => {
  const resp = {
    archiveFileId: 1,
    title: "Acme · 12 okt 2026 · € 21,78",
    subtitle: "Factuur INV-0042 · file_1.pdf",
    status: "matched",
    analyzedAt: "2026-10-24T16:00:00Z",
    invoice: { documentType: "factuur", bedragInclBtw: 21.78, currency: "EUR", datum: "2026-10-12" },
    matchedBankLine: { id: 500, datum: "2026-10-13T00:00:00", bedrag: -21.78, omschrijving: "ACME" },
    bankLineCandidates: [{ id: 500, datum: "2026-10-13T00:00:00", bedrag: -21.78, omschrijving: "ACME", score: 120 }],
    alreadySubmitted: undefined,
  } as unknown as MailDocumentAnalyzeResponse;

  it("copies status, titles, analysis, match and candidates onto the row", () => {
    const out = applyAnalysis(doc(1, "new"), resp);
    expect(out.id).toBe(1);
    expect(out.status).toBe("matched");
    expect(out.title).toBe(resp.title);
    expect(out.subtitle).toBe(resp.subtitle);
    expect(out.analyzedAt).toBe(resp.analyzedAt);
    expect(out.documentType).toBe("factuur");
    expect(out.analysis).toBe(resp);
    expect(out.match?.id).toBe(500);
    expect(out.candidates).toHaveLength(1);
    expect(out.naam).toBe("file_1.pdf");
  });

  it("clears a stale match when the new analysis has none", () => {
    const before = analyzed(1, "matched", { match: { id: 1, datum: "", bedrag: 1, omschrijving: "" } });
    const out = applyAnalysis(before, { ...resp, status: "unmatched", matchedBankLine: null, bankLineCandidates: undefined });
    expect(out.match).toBeUndefined();
    expect(out.candidates).toEqual([]);
  });
});

describe("summarizeRun", () => {
  it("renders the full summary", () => {
    const outcomes = [
      ...Array.from({ length: 7 }, () => ({ kind: "ok", status: "matched" }) as const),
      ...Array.from({ length: 2 }, () => ({ kind: "ok", status: "unmatched" }) as const),
      { kind: "ok", status: "receipt_waiting" } as const,
      ...Array.from({ length: 2 }, () => ({ kind: "ok", status: "not_invoice" }) as const),
    ];
    expect(summarizeRun(outcomes)).toBe("12 geanalyseerd · 7 met betaling · 3 zonder betaling · 2 geen factuur");
  });

  it("leaves out zero parts and appends failures and skipped documents", () => {
    expect(
      summarizeRun([
        { kind: "ok", status: "matched" },
        { kind: "error", message: "boom" },
        { kind: "aborted" },
        { kind: "aborted" },
      ]),
    ).toBe("1 geanalyseerd · 1 met betaling · 1 mislukt · 2 overgeslagen");
  });

  it("counts duplicates separately", () => {
    expect(summarizeRun([{ kind: "ok", status: "duplicate" }])).toBe("1 geanalyseerd · 1 mogelijk dubbel");
  });
});

describe("analyze error classification", () => {
  it("treats 409 as busy", () => {
    expect(isAnalyzeBusyError({ status: 409, message: "Wordt al geanalyseerd" })).toBe(true);
    expect(isAnalyzeBusyError({ status: 502 })).toBe(false);
  });

  it("stops the run on no_api_key, credits, 412 and 401", () => {
    expect(isFatalAnalyzeError({ status: 400, code: "no_api_key", message: "Stel eerst een Anthropic API-sleutel in" })).toBe(true);
    expect(isFatalAnalyzeError({ status: 502, code: "claude_error", message: "Je Anthropic API-tegoed is op. Vul je tegoed aan." })).toBe(true);
    expect(isFatalAnalyzeError({ status: 502, code: "claude_error", message: "Your credit balance is too low" })).toBe(true);
    expect(isFatalAnalyzeError({ status: 412, code: "eboekhouden_session_expired" })).toBe(true);
    expect(isFatalAnalyzeError({ status: 401 })).toBe(true);
  });

  it("keeps going on ordinary failures", () => {
    expect(isFatalAnalyzeError({ status: 502, code: "claude_error", message: "Claude is momenteel overbelast." })).toBe(false);
    expect(isFatalAnalyzeError({ status: 415, code: "unsupported_media_type" })).toBe(false);
    expect(isFatalAnalyzeError({ status: 404, message: "Document niet gevonden in de mailmap" })).toBe(false);
    expect(isFatalAnalyzeError(new Error("network"))).toBe(false);
    expect(isFatalAnalyzeError(undefined)).toBe(false);
  });

  it("falls back to a generic message", () => {
    expect(analyzeErrorMessage(new Error("Downloaden uit e-Boekhouden mislukt"))).toBe("Downloaden uit e-Boekhouden mislukt");
    expect(analyzeErrorMessage({})).toBe("Analyse mislukt");
    expect(analyzeErrorMessage(null)).toBe("Analyse mislukt");
  });
});

describe("texts", () => {
  it("formats money in nl-NL with the currency", () => {
    expect(formatMoney(21.78)).toBe("€ 21,78");
    expect(formatMoney(1234.5)).toBe("€ 1.234,50");
    expect(formatMoney(-21.78)).toBe("€ -21,78");
    expect(formatMoney(32.8, "CHF")).toBe("CHF 32,80");
  });

  it("builds the confirmation text with the estimated cost", () => {
    expect(analyzeAllConfirmText(34)).toBe("34 documenten analyseren? Dat kost ongeveer € 1,02 van je tegoed bij Anthropic.");
  });

  it("builds the subtitle with Dutch singular and plural", () => {
    expect(inboxSubtitle(12, 5)).toBe("12 afschriftregels en 5 documenten om te verwerken");
    expect(inboxSubtitle(1, 1)).toBe("1 afschriftregel en 1 document om te verwerken");
    expect(inboxSubtitle(3, 0)).toBe("3 afschriftregels om te verwerken");
    expect(inboxSubtitle(0, 2)).toBe("2 documenten om te verwerken");
    expect(inboxSubtitle(0, 0)).toBe("Je inbox is leeg. Alles is verwerkt.");
  });

  it("parses bare dates as local calendar dates", () => {
    const d = parseDate("2026-10-12")!;
    expect(d.getFullYear()).toBe(2026);
    expect(d.getMonth()).toBe(9);
    expect(d.getDate()).toBe(12);
    expect(parseDate("2026-10-24T15:43:35.67")?.getHours()).toBe(15);
    expect(parseDate("nope")).toBeNull();
    expect(parseDate("")).toBeNull();
  });

  it("formats short dates in Dutch", () => {
    expect(formatShortDate("2026-10-12")).toBe("12 okt");
    expect(formatShortDate("")).toBe("");
  });

  it("shows the invoice date once analysed, else the received date", () => {
    expect(documentDate(doc(1, "new"))).toBe("2026-10-24T15:43:35.67");
    const d = analyzed(1, "matched", { analysis: { invoice: { datum: "2026-10-12" } } as never });
    expect(documentDate(d)).toBe("2026-10-12");
  });

  it("writes the aria label out in full", () => {
    expect(documentAriaLabel("Boek", analyzed(1, "matched"))).toBe("Boek Acme, 12 okt 2026, € 21,78");
    expect(documentAriaLabel("Analyseer", doc(1, "new"))).toBe("Analyseer file 1");
  });
});

describe("documentAmount", () => {
  const withInvoice = (inv: object, overrides: Partial<MailDocument> = {}) =>
    analyzed(1, "unmatched", { analysis: { invoice: inv } as never, ...overrides });

  it("is null before analysis or for a zero amount", () => {
    expect(documentAmount(doc(1, "new"))).toBeNull();
    expect(documentAmount(withInvoice({ bedragInclBtw: 0, currency: "EUR" }))).toBeNull();
    expect(formatDocumentAmount(doc(1, "new"))).toBeNull();
  });

  it("returns the amount with its currency", () => {
    expect(documentAmount(withInvoice({ bedragInclBtw: 21.78, currency: "EUR" }))).toEqual({ value: 21.78, currency: "EUR" });
    expect(formatDocumentAmount(withInvoice({ bedragInclBtw: 32.8, currency: "chf" }))).toBe("CHF 32,80");
    expect(formatDocumentAmount(withInvoice({ bedragInclBtw: 5, currency: "" }))).toBe("€ 5,00");
  });

  it("shows a creditnota as a negative amount", () => {
    const credit = withInvoice({ bedragInclBtw: 21.78, currency: "EUR", documentType: "creditnota" }, { documentType: "creditnota" });
    expect(documentAmount(credit)).toEqual({ value: -21.78, currency: "EUR" });
    expect(formatDocumentAmount(credit)).toBe("€ -21,78");
  });
});

describe("documentSubtitle", () => {
  it("shows the error message for a failed row", () => {
    expect(documentSubtitle(doc(1, "new"), { kind: "error", message: "Downloaden mislukt" })).toBe("Downloaden mislukt");
  });

  it("describes the payment of a matched document", () => {
    const d = analyzed(1, "matched", {
      match: { id: 5, datum: "2026-10-13T00:00:00", bedrag: -21.78, omschrijving: "ANTHROPIC PBC" },
    });
    expect(documentSubtitle(d)).toBe("Betaald 13 okt · € -21,78 · ANTHROPIC PBC");
  });

  it("uses the fixed notes per status", () => {
    expect(documentSubtitle(analyzed(1, "unmatched"))).toBe("Wordt geboekt als openstaande factuur");
    expect(documentSubtitle(analyzed(1, "receipt_waiting"))).toBe("Bonnetjes boek je samen met de betaling");
    expect(documentSubtitle(doc(1, "linked"))).toBe("Verwerkt in e-Boekhouden");
    expect(documentSubtitle(doc(1, "junk"))).toBe("Kleine afbeelding");
  });

  it("names the earlier booking of a duplicate", () => {
    const d = analyzed(1, "duplicate", { alreadySubmitted: { mutNr: 1234, submittedAt: "2026-10-03T10:00:00Z" } });
    expect(documentSubtitle(d)).toBe("Dit bestand is al geboekt op 3 okt (mutatie 1234)");
    expect(documentSubtitle(analyzed(1, "duplicate"))).toBe("Dit bestand is al eens geboekt");
  });

  it("falls back to the backend subtitle", () => {
    expect(documentSubtitle(doc(1, "new"))).toBe("Ontvangen 24 okt 15:43 · PDF · 223 kB");
    expect(documentSubtitle(analyzed(1, "not_invoice", { subtitle: "Algemene voorwaarden · av.pdf" }))).toBe("Algemene voorwaarden · av.pdf");
  });
});

describe("primaryActionFor", () => {
  it("follows the row state first", () => {
    expect(primaryActionFor(doc(1, "new"), { kind: "queued" }, true)).toMatchObject({ kind: "wait", disabled: true });
    expect(primaryActionFor(doc(1, "new"), { kind: "analyzing" }, true)).toMatchObject({ kind: "wait", label: "Analyseren…", disabled: true });
    expect(primaryActionFor(doc(1, "new"), { kind: "error", message: "x" }, true)).toEqual({ kind: "retry", label: "Opnieuw" });
  });

  it("maps statuses to actions", () => {
    expect(primaryActionFor(doc(1, "new"), undefined, true)).toEqual({ kind: "analyze", label: "Analyseren" });
    expect(primaryActionFor(analyzed(1, "matched"), undefined, true)).toEqual({ kind: "book", label: "Boeken" });
    expect(primaryActionFor(analyzed(1, "unmatched"), undefined, true)).toEqual({ kind: "book", label: "Boeken" });
    expect(primaryActionFor(analyzed(1, "receipt_waiting"), undefined, true)).toMatchObject({ kind: "wait", disabled: true });
    expect(primaryActionFor(doc(1, "linked"), undefined, true)).toEqual({ kind: "archive", label: "Opruimen" });
    expect(primaryActionFor(doc(1, "ignored"), undefined, true)).toEqual({ kind: "unignore", label: "Niet meer negeren" });
    for (const status of ["duplicate", "not_invoice", "junk", "unsupported"] as const) {
      expect(primaryActionFor(doc(1, status), undefined, true)).toEqual({ kind: "ignore", label: "Negeren" });
    }
  });

  it("has no booking action when the parent cannot book", () => {
    expect(primaryActionFor(analyzed(1, "matched"), undefined, false)).toBeNull();
    expect(primaryActionFor(analyzed(1, "unmatched"), undefined, false)).toBeNull();
  });

  it("never offers to book a credit note", () => {
    for (const status of ["matched", "unmatched"] as const) {
      const cn = analyzed(1, status, { documentType: "creditnota" });
      expect(primaryActionFor(cn, undefined, true)).toEqual({
        kind: "wait",
        label: "Boeken",
        disabled: true,
        reason: CREDITNOTA_NOT_BOOKABLE,
      });
      expect(documentSubtitle(cn)).toBe(CREDITNOTA_NOT_BOOKABLE);
    }
    // Other statuses keep their own action, e.g. cleaning up once it is
    // booked and linked in e-Boekhouden.
    expect(primaryActionFor(doc(1, "linked", { documentType: "creditnota" }), undefined, true)).toEqual({ kind: "archive", label: "Opruimen" });
  });
});

describe("nextFocusId", () => {
  it("prefers the next row, then the previous one", () => {
    expect(nextFocusId([1, 2, 3], 2)).toBe(3);
    expect(nextFocusId([1, 2, 3], 3)).toBe(2);
    expect(nextFocusId([1], 1)).toBeNull();
    expect(nextFocusId([1, 2], 9)).toBeNull();
  });
});
