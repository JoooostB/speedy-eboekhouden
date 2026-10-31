import type {
  AnalyzedInvoice,
  InvoiceSubmitFullRequest,
  InvoiceSubmitReceiptRequest,
  ReceiptExtraLine,
  RegelPayload,
} from "../../api/types";
import { parseAmount } from "./reviewAmounts";

/**
 * Builds the bodies of POST /invoices/submit-full and /invoices/submit-receipt
 * from what the review dialog has at submit time. Two document sources exist:
 *
 *  - an upload: the file was analysed through /invoices/analyze and is sent
 *    again inline as pdfBase64 (folderId says where it is archived); uploadKey
 *    is a legacy field, empty since the analyser stores nothing;
 *  - a Mail & herken document (archiveFileId): nothing is sent but the id,
 *    the backend links, renames and moves the file; targetFolderId says
 *    where, omitted means Facturen/<jaar>/<maand>.
 *
 * The upload payloads are the ones the dialog has always sent; only the
 * construction moved here so both sources are unit-tested.
 */

/** One additional booking line within a receipt as the dialog edits it:
 *  typically a restaurant tip that was added on the card but did not make it
 *  onto the receipt. Amounts are strings while editing ("12,50"), read with
 *  parseAmount. */
export interface ExtraLineEdit {
  bedragIncl: string;
  btwCode: string;
  omschrijving: string;
}

/** The part of the analyse response the payloads read. */
export type SubmitSource = Pick<AnalyzedInvoice, "uploadKey" | "filename" | "pdfHash" | "crediteurenId" | "archiveFileId"> & {
  invoice: Pick<AnalyzedInvoice["invoice"], "documentType">;
};

export interface SubmitInput {
  source: SubmitSource;
  datum: string;
  leverancier: string;
  factuurnummer: string;
  omschrijving: string;
  bedragExcl: number;
  bedragIncl: number;
  btwBedrag: number;
  btwCode: string;
  relatieId: number;
  tegenRekeningId: number;
  /** The linked bank line, 0 for none. */
  importId: number;
  /** The archive folder the user picked, 0 for none. For an upload none
   *  means "do not archive"; for a mail-folder document it means the
   *  automatic Facturen/<jaar>/<maand>. */
  folderId: number;
  /** The upload inlined as base64. Upload source only; the dialog reads the
   *  file only when a folder was picked. */
  pdfBase64?: string;
  /** Legacy receipt split (lines after the main line); the dialog no
   *  longer fills it and sends `regels` instead. */
  extraLines: ReceiptExtraLine[];
  /** The lines of a split booking (regels.ts). Sent only with two or more
   *  lines; the amounts above are then the totals they add up to. A split
   *  receipt never sends extraLines. */
  regels?: RegelPayload[];
}

/** The `regels` field of a payload: only a real split is sent, so a
 *  single-line booking keeps the body it has always had. */
function regelsField(i: Pick<SubmitInput, "regels">): { regels?: RegelPayload[] } {
  return i.regels && i.regels.length >= 2 ? { regels: i.regels } : {};
}

export function isArchiveSource(source: Pick<AnalyzedInvoice, "archiveFileId">): boolean {
  return (source.archiveFileId ?? 0) > 0;
}

/** Converts the dialog's extra lines into the backend's shape, dropping
 *  empty ones and splitting each amount by its BTW code. The tegenrekening is
 *  left out so the backend defaults it to the main line's. */
export function receiptExtraLines(lines: readonly ExtraLineEdit[], vatRate: (code: string) => number): ReceiptExtraLine[] {
  return lines
    .filter((el) => (parseAmount(el.bedragIncl) ?? 0) > 0)
    .map((el) => {
      const incl = parseAmount(el.bedragIncl) ?? 0;
      const rate = vatRate(el.btwCode);
      const btw = rate === 0 ? 0 : +((incl * rate) / (100 + rate)).toFixed(2);
      const excl = +(incl - btw).toFixed(2);
      return {
        bedragIncl: incl,
        bedragExcl: excl,
        btwBedrag: btw,
        btwCode: el.btwCode,
        omschrijving: el.omschrijving || undefined,
      };
    });
}

/** Body of POST /invoices/submit-full. */
export function buildFullSubmitPayload(i: SubmitInput): InvoiceSubmitFullRequest {
  const base = {
    datum: i.datum,
    leverancier: i.leverancier,
    factuurnummer: i.factuurnummer,
    omschrijving: i.omschrijving,
    bedragExcl: i.bedragExcl,
    bedragIncl: i.bedragIncl,
    btwBedrag: i.btwBedrag,
    btwCode: i.btwCode,
    inEx: "EX",
    relatieId: i.relatieId,
    tegenRekeningId: i.tegenRekeningId,
    rekeningId: i.source.crediteurenId || 0,
  };
  if (isArchiveSource(i.source)) {
    return {
      ...base,
      pdfHash: i.source.pdfHash,
      ...(i.importId ? { importId: i.importId } : {}),
      archiveFileId: i.source.archiveFileId,
      ...(i.folderId ? { targetFolderId: i.folderId } : {}),
      ...(i.source.invoice.documentType === "creditnota" ? { documentType: "creditnota" as const } : {}),
      ...regelsField(i),
    };
  }
  return {
    ...base,
    uploadKey: i.source.uploadKey,
    filename: i.source.filename,
    pdfHash: i.source.pdfHash,
    ...(i.importId ? { importId: i.importId } : {}),
    ...(i.pdfBase64 ? { pdfBase64: i.pdfBase64 } : {}),
    ...(i.folderId ? { folderId: i.folderId } : {}),
    ...regelsField(i),
  };
}

/** Body of POST /invoices/submit-receipt. The bank line (importId) is always
 *  sent: a receipt is booked against it. */
export function buildReceiptSubmitPayload(i: SubmitInput): InvoiceSubmitReceiptRequest {
  const base = {
    datum: i.datum,
    leverancier: i.leverancier,
    omschrijving: i.omschrijving,
    bedragExcl: i.bedragExcl,
    bedragIncl: i.bedragIncl,
    btwBedrag: i.btwBedrag,
    btwCode: i.btwCode,
    tegenRekeningId: i.tegenRekeningId,
  };
  const regels = regelsField(i);
  const extra = i.extraLines.length > 0 && !regels.regels ? { extraLines: i.extraLines } : {};
  if (isArchiveSource(i.source)) {
    return {
      ...base,
      importId: i.importId,
      ...extra,
      archiveFileId: i.source.archiveFileId,
      ...(i.folderId ? { targetFolderId: i.folderId } : {}),
      ...regels,
    };
  }
  return {
    ...base,
    uploadKey: i.source.uploadKey,
    filename: i.source.filename,
    importId: i.importId,
    ...extra,
    ...(i.pdfBase64 ? { pdfBase64: i.pdfBase64 } : {}),
    ...(i.folderId ? { folderId: i.folderId } : {}),
    ...regels,
  };
}
