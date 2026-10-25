import type {
  MailDocument,
  MailDocumentAnalyzeResponse,
  MailDocumentStatus,
} from "../api/types";

/**
 * Pure state logic of the documents inbox: how documents are grouped into
 * sections, which ones "Alles analyseren" picks, how an analyze response is
 * folded into a row, and the texts the list derives from a document. No
 * React and no API calls, so useMailDocuments and the components stay thin
 * and all of this is unit-tested.
 */

/** Client-side state of one row while the hook works on it. */
export type DocumentRowState =
  | { kind: "queued" }
  | { kind: "analyzing" }
  | { kind: "error"; message: string };

export type DocumentSectionId = "ready" | "todo" | "processed" | "ignored";

export interface DocumentSections {
  /** "Klaar om te boeken": matched. */
  ready: MailDocument[];
  /** "Te doen": everything that still needs the user. */
  todo: MailDocument[];
  /** "Al verwerkt in e-Boekhouden": linked. */
  processed: MailDocument[];
  /** Hidden behind "Toon genegeerd". */
  ignored: MailDocument[];
}

/** Statuses "Alles analyseren" never touches. */
export const ANALYZE_SKIP_STATUSES: ReadonlySet<MailDocumentStatus> = new Set<MailDocumentStatus>([
  "junk",
  "ignored",
  "linked",
  "unsupported",
]);

/** Above this many documents "Alles analyseren" asks for confirmation. */
export const ANALYZE_CONFIRM_THRESHOLD = 20;

/** Rough Claude cost of one document analysis, for the caption and the
 *  confirmation text. */
export const ANALYZE_COST_PER_DOCUMENT = 0.03;

/** How often an analysis is retried when the backend says it is already
 *  running (409), and how long to wait in between. */
export const ANALYZE_BUSY_RETRIES = 3;
export const ANALYZE_BUSY_DELAY_MS = 1500;

export function sectionForStatus(status: MailDocumentStatus): DocumentSectionId {
  switch (status) {
    case "matched":
      return "ready";
    case "linked":
      return "processed";
    case "ignored":
      return "ignored";
    default:
      return "todo";
  }
}

export function emptySections(): DocumentSections {
  return { ready: [], todo: [], processed: [], ignored: [] };
}

/** Groups documents into sections, keeping the list order within each. */
export function groupDocuments(docs: readonly MailDocument[]): DocumentSections {
  const sections = emptySections();
  for (const doc of docs) {
    sections[sectionForStatus(doc.status)].push(doc);
  }
  return sections;
}

/**
 * Sections to render during an "Alles analyseren" run. Rows must keep their
 * place while their chips change, so the grouping follows the snapshot
 * taken when the run started, while every row shows its live data. A
 * document that appeared after the snapshot is grouped by its live status;
 * one that has since disappeared is dropped. Ignoring is the user's own
 * action, not the run's, so a row that was ignored or un-ignored meanwhile
 * does move to where its live status puts it.
 */
export function sectionsFromSnapshot(
  snapshot: readonly MailDocument[],
  live: readonly MailDocument[],
): DocumentSections {
  const liveById = new Map(live.map((d) => [d.id, d]));
  const sections = emptySections();
  const placed = new Set<number>();
  for (const old of snapshot) {
    const current = liveById.get(old.id);
    if (!current) continue;
    const ignoredChanged = (old.status === "ignored") !== (current.status === "ignored");
    sections[sectionForStatus(ignoredChanged ? current.status : old.status)].push(current);
    placed.add(old.id);
  }
  for (const doc of live) {
    if (!placed.has(doc.id)) {
      sections[sectionForStatus(doc.status)].push(doc);
    }
  }
  return sections;
}

export function isAnalyzed(doc: MailDocument): boolean {
  return !!doc.analyzedAt || !!doc.analysis;
}

/** Whether the analyser can read this file at all (and it is still open). */
export function canAnalyze(doc: MailDocument): boolean {
  return doc.mediaType !== "" && doc.status !== "linked";
}

/** The ids "Alles analyseren" works through: readable, not yet analysed,
 *  not junk, ignored, linked or unsupported. In list order. */
export function analyzeAllTargets(docs: readonly MailDocument[]): number[] {
  return docs
    .filter((d) => !ANALYZE_SKIP_STATUSES.has(d.status) && d.mediaType !== "" && !isAnalyzed(d))
    .map((d) => d.id);
}

/** Documents the user still has to deal with: the tab badge and the
 *  subtitle count. Mirrors the backend's dashboard count (not linked, not
 *  ignored, readable). */
export function pendingDocumentCount(docs: readonly MailDocument[]): number {
  return docs.filter((d) => d.status !== "linked" && d.status !== "ignored" && d.status !== "unsupported").length;
}

/** Folds an analyze response into the list row it belongs to. */
export function applyAnalysis(doc: MailDocument, resp: MailDocumentAnalyzeResponse): MailDocument {
  return {
    ...doc,
    status: resp.status,
    title: resp.title,
    subtitle: resp.subtitle,
    analyzedAt: resp.analyzedAt,
    documentType: resp.invoice?.documentType ?? "",
    analysis: resp,
    match: resp.matchedBankLine ?? undefined,
    candidates: resp.bankLineCandidates ?? [],
    alreadySubmitted: resp.alreadySubmitted,
  };
}

/** Per-document outcome of an "Alles analyseren" run. */
export type AnalyzeOutcome =
  | { kind: "ok"; status: MailDocumentStatus }
  | { kind: "error"; message: string }
  | { kind: "aborted" };

/** The closing snackbar text: "12 geanalyseerd · 7 met betaling · 3 zonder
 *  · 2 geen factuur", with failures and skipped documents appended and
 *  zero parts left out. */
export function summarizeRun(outcomes: readonly AnalyzeOutcome[]): string {
  let analyzed = 0;
  let matched = 0;
  let without = 0;
  let notInvoice = 0;
  let duplicate = 0;
  let failed = 0;
  let aborted = 0;
  for (const o of outcomes) {
    if (o.kind === "error") {
      failed++;
      continue;
    }
    if (o.kind === "aborted") {
      aborted++;
      continue;
    }
    analyzed++;
    switch (o.status) {
      case "matched":
        matched++;
        break;
      case "unmatched":
      case "receipt_waiting":
        without++;
        break;
      case "not_invoice":
        notInvoice++;
        break;
      case "duplicate":
        duplicate++;
        break;
    }
  }
  const parts = [`${analyzed} geanalyseerd`];
  if (matched > 0) parts.push(`${matched} met betaling`);
  if (without > 0) parts.push(`${without} zonder`);
  if (notInvoice > 0) parts.push(`${notInvoice} geen factuur`);
  if (duplicate > 0) parts.push(`${duplicate} mogelijk dubbel`);
  if (failed > 0) parts.push(`${failed} mislukt`);
  if (aborted > 0) parts.push(`${aborted} overgeslagen`);
  return parts.join(" · ");
}

interface ErrorLike {
  status?: number;
  code?: string;
  message?: string;
}

function asErrorLike(err: unknown): ErrorLike {
  if (err && typeof err === "object") return err as ErrorLike;
  return {};
}

/** 409: the backend is already analysing this file; wait and retry. */
export function isAnalyzeBusyError(err: unknown): boolean {
  return asErrorLike(err).status === 409;
}

/**
 * Errors after which continuing a run is pointless: no API key, no Claude
 * credits (a claude_error whose Dutch message mentions the tegoed), an
 * expired e-boekhouden session (412) or an expired Speedy session (401).
 */
export function isFatalAnalyzeError(err: unknown): boolean {
  const { status, code, message } = asErrorLike(err);
  if (status === 401 || status === 412) return true;
  if (code === "no_api_key") return true;
  if (code === "claude_error" && /tegoed|credit|billing/i.test(message ?? "")) return true;
  return false;
}

export function analyzeErrorMessage(err: unknown): string {
  const { message } = asErrorLike(err);
  return message && message.trim() !== "" ? message : "Analyse mislukt";
}

/** nl-NL money: "€ 21,78", "CHF 32,80", "€ -21,78". Regular spaces so the
 *  text is predictable in tests and copies cleanly. */
export function formatMoney(value: number, currency = "EUR"): string {
  const code = (currency || "EUR").toUpperCase();
  const formatted = new Intl.NumberFormat("nl-NL", {
    style: "currency",
    currency: code,
    currencyDisplay: code === "EUR" ? "symbol" : "code",
  }).format(value);
  return formatted.replace(/ /g, " ");
}

export function analyzeAllConfirmText(count: number): string {
  const cost = formatMoney(count * ANALYZE_COST_PER_DOCUMENT);
  return `Analyseer ${count} documenten? Dit kost ongeveer ${cost} aan AI-tegoed.`;
}

/** "12 afschriftregels en 5 documenten om te verwerken", singular aware,
 *  zero parts left out. */
export function inboxSubtitle(bankLines: number, documents: number): string {
  const parts: string[] = [];
  if (bankLines > 0) parts.push(`${bankLines} ${bankLines === 1 ? "afschriftregel" : "afschriftregels"}`);
  if (documents > 0) parts.push(`${documents} ${documents === 1 ? "document" : "documenten"}`);
  if (parts.length === 0) return "Je inbox is leeg. Alles is verwerkt!";
  return `${parts.join(" en ")} om te verwerken`;
}

/**
 * Parses the dates e-boekhouden and the analysis use. A bare "YYYY-MM-DD"
 * is read as a local calendar date (not UTC midnight, which would show the
 * previous day west of Greenwich); anything else goes through Date.
 */
export function parseDate(s: string | undefined | null): Date | null {
  if (!s) return null;
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s.trim());
  const d = m ? new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])) : new Date(s);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** "12 okt" */
export function formatShortDate(s: string | undefined | null): string {
  const d = parseDate(s);
  if (!d) return "";
  return new Intl.DateTimeFormat("nl-NL", { day: "numeric", month: "short" }).format(d).replace(/\./g, "");
}

/** "12 okt 2026" */
export function formatLongDate(s: string | undefined | null): string {
  const d = parseDate(s);
  if (!d) return "";
  return new Intl.DateTimeFormat("nl-NL", { day: "numeric", month: "short", year: "numeric" })
    .format(d)
    .replace(/\./g, "");
}

/** The date a row shows: the invoice date once analysed, else received. */
export function documentDate(doc: MailDocument): string {
  const invoiceDate = doc.analysis?.invoice?.datum;
  if (invoiceDate && parseDate(invoiceDate)) return invoiceDate;
  return doc.datum;
}

export function isCreditnota(doc: MailDocument): boolean {
  return (doc.documentType ?? doc.analysis?.invoice?.documentType) === "creditnota";
}

/** The amount of an analysed document (negative for a creditnota) with its
 *  currency, or null when there is nothing to show. */
export function documentAmount(doc: MailDocument): { value: number; currency: string } | null {
  const inv = doc.analysis?.invoice;
  if (!inv || !Number.isFinite(inv.bedragInclBtw) || inv.bedragInclBtw === 0) return null;
  const magnitude = Math.abs(inv.bedragInclBtw);
  return {
    value: isCreditnota(doc) ? -magnitude : magnitude,
    currency: (inv.currency || "EUR").toUpperCase(),
  };
}

export function formatDocumentAmount(doc: MailDocument): string | null {
  const amount = documentAmount(doc);
  return amount ? formatMoney(amount.value, amount.currency) : null;
}

/** Accessible name of a row action: "Boek Anthropic, 12 okt 2026, € 21,78". */
export function documentAriaLabel(action: string, doc: MailDocument): string {
  return `${action} ${doc.title.split(" · ").join(", ")}`;
}

/** Why a credit note has no booking action. Speedy books every document as
 *  an invoice received (a cost); for a credit note that would be wrong, so
 *  the backend refuses it and the user books it in e-Boekhouden. Once linked
 *  there, the file shows up as "Al gekoppeld" and can be cleaned up here. */
export const CREDITNOTA_NOT_BOOKABLE = "Creditnota's boek je voorlopig in e-Boekhouden zelf";

/** The second line of a row, per status (and per row state). */
export function documentSubtitle(doc: MailDocument, rowState?: DocumentRowState): string {
  if (rowState?.kind === "error") return rowState.message;
  if ((doc.status === "matched" || doc.status === "unmatched") && isCreditnota(doc)) return CREDITNOTA_NOT_BOOKABLE;
  switch (doc.status) {
    case "matched": {
      const m = doc.match;
      if (!m) return doc.subtitle;
      const parts = [`Betaald ${formatShortDate(m.datum)}`.trim(), formatMoney(m.bedrag)];
      if (m.omschrijving) parts.push(m.omschrijving);
      return parts.join(" · ");
    }
    case "unmatched":
      return "Wordt geboekt als openstaande factuur";
    case "receipt_waiting":
      return "Bonnetjes boek je samen met de betaling";
    case "linked":
      return "Verwerkt in e-Boekhouden";
    case "duplicate": {
      const s = doc.alreadySubmitted;
      if (!s) return "Zelfde bestand is al geboekt";
      const when = formatShortDate(s.submittedAt);
      return `Zelfde bestand geboekt${when ? ` op ${when}` : ""} (mutatie ${s.mutNr})`;
    }
    case "junk":
      return "Kleine afbeelding";
    default:
      return doc.subtitle;
  }
}

export type PrimaryActionKind = "analyze" | "retry" | "book" | "archive" | "ignore" | "unignore" | "wait";

export interface PrimaryAction {
  kind: PrimaryActionKind;
  label: string;
  /** Disabled actions still render so the row keeps its shape; reason is
   *  the tooltip. */
  disabled?: boolean;
  reason?: string;
}

/**
 * The primary button of a row. canBook says whether the parent can open the
 * booking flow; without it a matched or unmatched row has no primary action
 * (its menu still works).
 */
export function primaryActionFor(
  doc: MailDocument,
  rowState: DocumentRowState | undefined,
  canBook: boolean,
): PrimaryAction | null {
  if (rowState?.kind === "queued") return { kind: "wait", label: "Analyseren", disabled: true, reason: "In wachtrij" };
  if (rowState?.kind === "analyzing") return { kind: "wait", label: "Analyseren…", disabled: true };
  if (rowState?.kind === "error") return { kind: "retry", label: "Opnieuw" };
  switch (doc.status) {
    case "new":
      return { kind: "analyze", label: "Analyseren" };
    case "matched":
    case "unmatched":
      if (isCreditnota(doc)) return { kind: "wait", label: "Boeken", disabled: true, reason: CREDITNOTA_NOT_BOOKABLE };
      return canBook ? { kind: "book", label: "Boeken" } : null;
    case "receipt_waiting":
      return { kind: "wait", label: "Boeken", disabled: true, reason: "Bonnetjes boek je samen met de betaling" };
    case "linked":
      return { kind: "archive", label: "Opruimen" };
    case "ignored":
      return { kind: "unignore", label: "Niet meer negeren" };
    case "duplicate":
    case "not_invoice":
    case "junk":
    case "unsupported":
      return { kind: "ignore", label: "Negeren" };
  }
}

/** The id to focus after `removedId` leaves the visible order: the row
 *  after it, else the one before it, else null. */
export function nextFocusId(visibleIds: readonly number[], removedId: number): number | null {
  const i = visibleIds.indexOf(removedId);
  if (i < 0) return null;
  if (i + 1 < visibleIds.length) return visibleIds[i + 1];
  if (i > 0) return visibleIds[i - 1];
  return null;
}
