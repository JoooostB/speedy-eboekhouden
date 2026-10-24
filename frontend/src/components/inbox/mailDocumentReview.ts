import type {
  AnalyzedInvoice,
  BankLineCandidate,
  MailDocument,
  MatchedBankLine,
  SubmitArchiveOutcome,
} from "../../api/types";
import { formatLongDate, formatMoney, parseDate } from "../../hooks/mailDocumentsModel";

/**
 * Pure helpers for booking a Mail & herken document through the invoice
 * review dialog: turning a listed document and its downloaded bytes into the
 * AnalyzedInvoice the dialog edits, and the texts and decisions the dialog
 * needs when such a document is on the table. No React and no API calls, so
 * all of it is unit-tested and the dialog stays thin.
 */

/** The file types the invoice reader accepts, as the accept attribute of a
 *  file input: extensions for the picker, media types for drag and drop. */
export const INVOICE_FILE_ACCEPT = ".pdf,.jpg,.jpeg,.png,application/pdf,image/jpeg,image/png";

const ACCEPTED_INVOICE_TYPES: ReadonlySet<string> = new Set(["application/pdf", "image/jpeg", "image/png"]);

/** The media type implied by a file name's extension, "" when unknown. */
export function mediaTypeFromName(name: string): string {
  const ext = /\.([a-z0-9]+)$/.exec(name.toLowerCase())?.[1];
  switch (ext) {
    case "pdf":
      return "application/pdf";
    case "jpg":
    case "jpeg":
      return "image/jpeg";
    case "png":
      return "image/png";
    default:
      return "";
  }
}

/** Whether a dropped or picked file is one the analyser can read. Browsers
 *  leave type empty for extensions they do not know, so the name decides
 *  then. */
export function isAcceptedInvoiceFile(file: { type: string; name: string }): boolean {
  return ACCEPTED_INVOICE_TYPES.has(file.type || mediaTypeFromName(file.name));
}

/** Wraps the downloaded bytes of a mail-folder document in a File named
 *  like the document. The type comes from the response (the backend sniffs
 *  it), else from the listing, else from the extension. */
export function documentFile(doc: Pick<MailDocument, "naam" | "mediaType">, blob: Blob): File {
  const type = blob.type || doc.mediaType || mediaTypeFromName(doc.naam);
  return new File([blob], doc.naam, { type });
}

export type PreviewKind = "image" | "pdf";

/** How the dialog previews a document: an <img> for images, the PDF viewer
 *  for everything else. */
export function previewKind(mediaType: string | undefined, filename: string): PreviewKind {
  const type = mediaType || mediaTypeFromName(filename);
  return type.startsWith("image/") ? "image" : "pdf";
}

export interface BuildFromDocumentOptions {
  /** Name of the mail folder, for the dialog's helper text. */
  mailFolderName: string;
  /** A bank line to pre-select as the payment, when the review is opened
   *  from that line. It becomes the match and heads the candidates. */
  bankLine?: MatchedBankLine;
}

/**
 * Builds the AnalyzedInvoice the review dialog edits from a listed
 * mail-folder document and its downloaded File. The analysis comes from the
 * document; the match and the candidates are the document's fresh ones (the
 * list carries the global assignment, so a document that lists candidates
 * but no match really has none), falling back to the analysis for a document
 * without candidate fields, unless a bank line is forced. There is no R2
 * upload for a mail-folder document, so uploadKey and pdfUrl are empty and
 * the preview uses the object URL. Returns null for a document that has not
 * been analysed: there is nothing to review then.
 */
export function buildAnalyzedInvoiceFromDocument(
  doc: MailDocument,
  file: File,
  objectUrl: string,
  opts: BuildFromDocumentOptions,
): AnalyzedInvoice | null {
  const analysis = doc.analysis;
  if (!analysis) return null;

  const fresh = doc.candidates !== undefined;
  let matched: MatchedBankLine | null = fresh ? doc.match ?? null : analysis.matchedBankLine ?? null;
  let candidates: BankLineCandidate[] = fresh ? doc.candidates! : analysis.bankLineCandidates ?? [];
  if (opts.bankLine) {
    const { bankLine } = opts;
    matched = bankLine;
    const known = candidates.find((c) => c.id === bankLine.id);
    candidates = [known ?? { ...bankLine, score: 0 }, ...candidates.filter((c) => c.id !== bankLine.id)];
  }

  return {
    ...analysis,
    filename: doc.naam,
    uploadKey: "",
    pdfUrl: "",
    matchedBankLine: matched,
    bankLineCandidates: candidates,
    alreadySubmitted: doc.alreadySubmitted ?? analysis.alreadySubmitted,
    localFile: file,
    localPdfUrl: objectUrl,
    archiveFileId: doc.id,
    mailFolderName: opts.mailFolderName,
    documentTitle: doc.title,
  };
}

/** The bank lines a card can be linked to: the match first, then the other
 *  candidates, without duplicates. */
export function bankLineChoices(
  source: Pick<AnalyzedInvoice, "matchedBankLine" | "bankLineCandidates">,
): MatchedBankLine[] {
  const out: MatchedBankLine[] = [];
  const seen = new Set<number>();
  const add = (line: MatchedBankLine | null | undefined) => {
    if (line && !seen.has(line.id)) {
      seen.add(line.id);
      out.push(line);
    }
  };
  add(source.matchedBankLine);
  for (const c of source.bankLineCandidates ?? []) add(c);
  return out;
}

/** Option text of the bank-line select: "13 okt 2026 · € 21,78 · ANTHROPIC PBC",
 *  with the description cut at maxDescription characters. */
export function bankLineOptionLabel(line: MatchedBankLine, maxDescription = 60): string {
  const parts = [formatLongDate(line.datum), formatMoney(line.bedrag)];
  const desc = (line.omschrijving ?? "").trim();
  if (desc) {
    parts.push(desc.length > maxDescription ? `${desc.slice(0, maxDescription - 1).trimEnd()}…` : desc);
  }
  return parts.filter(Boolean).join(" · ");
}

export interface BookingShape {
  archiveFileId?: number;
  isReceipt: boolean;
  /** The linked bank line, 0 for none. */
  importId: number;
}

/** A mail-folder invoice without a bank line is booked as an open invoice
 *  (FactuurOntvangen without a payment). Receipts cannot be booked without a
 *  bank line at all, and the upload flow keeps its own wording. */
export function isOpenBooking(inv: BookingShape): boolean {
  return (inv.archiveFileId ?? 0) > 0 && !inv.isReceipt && inv.importId === 0;
}

/** The info alert for an open booking (UX flow 4). */
export const NO_MATCH_ALERT_TEXT =
  "Nog geen betaling gevonden. We boeken dit als openstaande factuur. Zodra de betaling op je afschrift staat, stelt Speedy voor om hem te koppelen.";

export interface SubmitLabelInput {
  submitting: boolean;
  progress: number;
  total: number;
  /** Cards that will be skipped for missing data. */
  blockedCount: number;
  /** The single card is an open booking (see isOpenBooking). */
  openBooking: boolean;
}

/** Text of the dialog's primary button. */
export function submitButtonLabel(i: SubmitLabelInput): string {
  if (i.submitting) return `Verwerken (${i.progress}/${i.total})...`;
  if (i.total === 1) return i.openBooking ? "Boeken als openstaand" : "Boeken";
  if (i.blockedCount > 0) return `Boek ${i.total - i.blockedCount} van ${i.total}`;
  return `Alles boeken (${i.total})`;
}

/** The month folder names the backend creates under Facturen/<jaar>. */
export const MONTH_FOLDER_NAMES: readonly string[] = [
  "01 Januari",
  "02 Februari",
  "03 Maart",
  "04 April",
  "05 Mei",
  "06 Juni",
  "07 Juli",
  "08 Augustus",
  "09 September",
  "10 Oktober",
  "11 November",
  "12 December",
];

/** "Facturen/2026/10 Oktober": the folder the backend picks when none was
 *  chosen, for the booking date. An unreadable date means today, like the
 *  backend's bookingDate. */
export function autoArchiveFolderPath(datum: string, now: Date = new Date()): string {
  const d = parseDate(datum) ?? now;
  return `Facturen/${d.getFullYear()}/${MONTH_FOLDER_NAMES[d.getMonth()]}`;
}

export interface FolderHelperInput {
  folderPicked: boolean;
  /** The booking date, for the automatic folder. */
  datum: string;
  mailFolderName: string;
}

/** Helper text under the "Verplaatsen naar" picker: where the file goes
 *  when no folder is picked, and that it leaves the mail folder either way. */
export function archiveFolderHelperText(i: FolderHelperInput, now?: Date): string {
  const leaves = i.mailFolderName
    ? `Het bestand gaat uit je mailmap ${i.mailFolderName}.`
    : "Het bestand gaat uit je mailmap.";
  if (i.folderPicked) return leaves;
  return `Geen map gekozen: ${autoArchiveFolderPath(i.datum, now)}. ${leaves}`;
}

/** The result line of a booked mail-folder document: "Geboekt · verplaatst
 *  naar Facturen / 2026 / 10 Oktober", or "Geboekt als openstaande factuur"
 *  for a booking without a bank line. Without a move only the first part. */
export function archiveResultLine(resp: SubmitArchiveOutcome, openBooking: boolean): string {
  const base = openBooking ? "Geboekt als openstaande factuur" : "Geboekt";
  return resp.moved && resp.targetFolderPath ? `${base} · verplaatst naar ${resp.targetFolderPath}` : base;
}
