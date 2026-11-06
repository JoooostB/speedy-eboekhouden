import type {
  AnalyzedInvoice,
  ArchiveFolder,
  BankLineCandidate,
  MailDocument,
  MatchedBankLine,
  SubmitArchiveOutcome,
} from "../../api/types";
import { formatLongDate, formatMoney, parseDate } from "../../hooks/mailDocumentsModel";
import { buildArchiveFolderPath } from "../../hooks/useArchiveFolders";

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
 * without candidate fields, unless a bank line is forced. The preview uses
 * the object URL. Returns null for a document that has not
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
  "Nog geen betaling gevonden, dus Speedy boekt dit als openstaande factuur. Staat het bedrag later op je afschrift, dan stelt Speedy voor om de betaling te koppelen.";

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
  if (i.submitting) return `Boeken (${i.progress}/${i.total})…`;
  if (i.total === 1) return i.openBooking ? "Boeken als openstaand" : "Boeken";
  if (i.blockedCount > 0) return `Boek ${i.total - i.blockedCount} van ${i.total}`;
  return `Alles boeken (${i.total})`;
}

/** The month folder names the backend creates under <jaar>. */
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

/** Where the year folders of the automatic month folder live: an existing
 *  folder ("Facturen"), the archive root (year folders already sit there),
 *  or a "Facturen" the backend still has to create. */
export type ArchiveBase = { kind: "folder"; id: number } | { kind: "root" } | { kind: "create" };

const sameFolderName = (a: string, b: string): boolean => a.trim().toLowerCase() === b.trim().toLowerCase();

function liveChildNamed(folders: readonly ArchiveFolder[], parentId: number, name: string): ArchiveFolder | undefined {
  return folders.find((f) => !f.isDeleted && f.parentId === parentId && sameFolderName(f.naam, name));
}

/** The backend's resolveArchiveBase (handler/invoice.go), same order: a live
 *  "Facturen" at the root, then a "Facturen" under a folder named
 *  "Basismap" (legacy), then year folders ("2025") at the root, else a new
 *  "Facturen". Deleted folders never count. */
export function archiveBaseFolder(folders: readonly ArchiveFolder[]): ArchiveBase {
  const facturen = liveChildNamed(folders, 0, "Facturen");
  if (facturen) return { kind: "folder", id: facturen.id };
  for (const f of folders) {
    if (f.isDeleted || !sameFolderName(f.naam, "Basismap")) continue;
    const legacy = liveChildNamed(folders, f.id, "Facturen");
    if (legacy) return { kind: "folder", id: legacy.id };
  }
  if (folders.some((f) => !f.isDeleted && f.parentId === 0 && /^\d{4}$/.test(f.naam.trim()))) return { kind: "root" };
  return { kind: "create" };
}

/** "Facturen / 2026 / 10 Oktober" or "2026 / 10 Oktober": the folder the
 *  backend picks when none was chosen, for the booking date and the archive
 *  as it is (archiveBaseFolder), written like the folder picker and the
 *  backend's targetFolderPath. Existing folders keep their own spelling. An
 *  unreadable date means today, like the backend's bookingDate. */
export function autoArchiveFolderPath(datum: string, folders: readonly ArchiveFolder[], now: Date = new Date()): string {
  const d = parseDate(datum) ?? now;
  const jaar = String(d.getFullYear());
  const maand = MONTH_FOLDER_NAMES[d.getMonth()];
  const base = archiveBaseFolder(folders);
  const segments: string[] = [];
  let parentId: number | null = null;
  if (base.kind === "folder") {
    const byId = new Map(folders.map((f) => [f.id, f]));
    const folder = byId.get(base.id);
    if (folder) segments.push(buildArchiveFolderPath(folder, byId));
    parentId = base.id;
  } else if (base.kind === "root") {
    parentId = 0;
  } else {
    segments.push("Facturen");
  }
  const year = parentId === null ? undefined : liveChildNamed(folders, parentId, jaar);
  segments.push(year?.naam ?? jaar);
  const month = year ? liveChildNamed(folders, year.id, maand) : undefined;
  segments.push(month?.naam ?? maand);
  return segments.join(" / ");
}

export interface FolderHelperInput {
  folderPicked: boolean;
  /** The booking date, for the automatic folder. */
  datum: string;
  /** The archive's folders, for the automatic folder (archiveBaseFolder). */
  folders: readonly ArchiveFolder[];
  mailFolderName: string;
}

/** Helper text under the "Verplaatsen naar" picker: that the file gets an
 *  archive name and leaves the mail folder either way, and where it goes
 *  when no folder is picked. */
export function archiveFolderHelperText(i: FolderHelperInput, now?: Date): string {
  const leaves = i.mailFolderName
    ? `Het bestand krijgt een archiefnaam en verlaat je mailmap ${i.mailFolderName}.`
    : "Het bestand krijgt een archiefnaam en verlaat je mailmap.";
  if (i.folderPicked) return leaves;
  return `${leaves} Kies je geen map, dan komt het in ${autoArchiveFolderPath(i.datum, i.folders, now)}.`;
}

/** Helper text under the folder picker of an upload: the file always goes
 *  into the digital archive, into the automatic month folder when no folder
 *  is picked. The upload is not booked without it. */
export function uploadFolderHelperText(i: Omit<FolderHelperInput, "mailFolderName">, now?: Date): string {
  if (i.folderPicked) return "Het bestand komt in deze map in je digitale archief.";
  return `Het bestand komt in je digitale archief. Kies je geen map, dan komt het in ${autoArchiveFolderPath(i.datum, i.folders, now)}.`;
}

/** The result line of a booked mail-folder document: "Geboekt · verplaatst
 *  naar Facturen / 2026 / 10 Oktober", or "Geboekt als openstaande factuur"
 *  for a booking without a bank line. Without a move only the first part. */
export function archiveResultLine(resp: SubmitArchiveOutcome, openBooking: boolean): string {
  const base = openBooking ? "Geboekt als openstaande factuur" : "Geboekt";
  return resp.moved && resp.targetFolderPath ? `${base} · verplaatst naar ${resp.targetFolderPath}` : base;
}
