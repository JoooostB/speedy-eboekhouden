// Auth
export interface AuthResponse {
  status: "ok" | "mfa_required";
}

export interface User {
  id: string;
  email: string;
  name: string;
}

export interface Team {
  id: string;
  name: string;
}

export interface MeResponse {
  user: User;
  team: Team | null;
  eboekhoudenConnected: boolean;
  avatarUrl: string;
}

// e-Boekhouden connection
export interface EBLoginRequest {
  email: string;
  password: string;
}

export interface EBStatusResponse {
  connected: boolean;
  mfaPending: boolean;
}

// Hours (existing)
export interface Employee {
  id: number;
  naam: string;
}

export interface Project {
  id: number;
  naam: string;
  relatieBedrijf?: string;
}

export interface Activity {
  id: number;
  naam: string;
}

export interface BulkEntry {
  employeeId: number;
  projectId: number;
  activityId: number;
  hours: string;
  dates: string[];
  description: string;
}

export interface BulkRequest {
  entries: BulkEntry[];
}

export interface EntryResult {
  employeeId: number;
  date: string;
  status: "ok" | "error";
  error?: string;
}

export interface BulkResponse {
  results: EntryResult[];
}

/** One previously-booked hour entry, returned by /hours/overview.
 *  Used by the bulk-entry calendar to mark dates that already have
 *  hours so the user doesn't double-book. */
export interface HourOverviewEntry {
  id: number;
  /** ISO datetime — typically "2026-04-01T00:00:00" with zero time. */
  datum: string;
  medewerker: string;
  project: string;
  activiteit: string;
  opmerkingen: string;
  aantalUren: number;
  aantalKilometers: number;
}

export interface HourOverviewResponse {
  entries: HourOverviewEntry[];
}

// Bank statements
export interface BankStatementRow {
  id: number;
  datum: string;
  rekening: string;
  mutDatum: string;
  mutBedrag: number;
  mutOmschrijving: string;
  mutFactuur: string | null;
  grootboekId: number;
  opmerking: string | null;
  hasFiles: boolean | null;
  verwerkFailureReason: string | null;
}

export interface BankStatementsResponse {
  items: BankStatementRow[];
  totalCount: number;
}

// Reference data
export interface LedgerAccount {
  id: number;
  code: string;
  omschrijving: string;
  rekeningCategorie: string;
}

export interface Relation {
  id: number;
  code: string;
  bedrijf: string;
  grootboekrekeningId: number;
  iban: string;
}

export interface VATCode {
  id: number;
  code: string;
  omschrijving: string;
  soort: string;
  rekenpercentage: number;
  percentage: number;
}

// Claude
export interface ClassifyRequest {
  omschrijving: string;
  bedrag: number;
  tegenrekening?: string;
  datum: string;
}

export interface ClassifyResult {
  grootboekcode: string;
  btwCode: string;
  soort: string;
  omschrijving: string;
  confidence: number;
}

export interface InvoiceData {
  leverancier: string;
  factuurnummer: string;
  datum: string;
  bedragExclBtw: number;
  bedragInclBtw: number;
  btwBedrag: number;
  btwPercentage: number;
  omschrijving: string;
  grootboekcode: string;
  btwCode: string;
  isReverseCharge: boolean;
  /** True for bonnetjes (restaurant, supermarkt, tankstation) — book as
   *  "Geld uitgegeven" without a leverancier relation. */
  isReceipt: boolean;
  receiptReason: string;
  /** ISO 4217 code from the invoice (EUR by default; CHF/USD/GBP/… for
   *  foreign invoices). Drives fuzzy bank-line matching via FX conversion. */
  currency: string;
  confidence: number;
  redenering: string;
  belastingAdvies: Array<{ type: string; tekst: string }>;
}

/** A folder in the e-boekhouden digitaal archief. */
export interface ArchiveFolder {
  id: number;
  naam: string;
  /** 0 means "root" (the implicit Basismap that e-boekhouden never returns). */
  parentId: number;
  isDeleted: boolean;
}

export interface InvoiceAnalyzeResponse {
  invoice: InvoiceData;
  filename: string;
  uploadKey: string;
  /** Public CDN URL for the uploaded PDF (from R2). Empty when R2 is not
   *  configured — the frontend then falls back to localPdfUrl. */
  pdfUrl: string;
  /** SHA-256 of the uploaded PDF — echoed back to SubmitFull so the
   *  backend can write a duplicate-detection marker post-booking. */
  pdfHash: string;
  /** True when the analysis result came from the per-user cache (i.e. the
   *  same PDF + ledger-account-set was analyzed before within the TTL). */
  cachedAnalysis?: boolean;
  /** Set when this exact PDF was previously booked successfully. The UI
   *  shows a prominent warning so the user can avoid creating a duplicate
   *  mutation. */
  alreadySubmitted?: {
    mutNr: number;
    paymentMutNr?: number;
    leverancier?: string;
    factuur?: string;
    bedragIncl?: number;
    submittedAt: string;
  };
  matchedRelation: {
    id: number;
    code: string;
    bedrijf: string;
  } | null;
  /** Internal ID of the crediteuren (1600) account */
  crediteurenId: number;

  // NOTE: client-side enrichments (a kept reference to the original File
  // object, a blob: URL for in-browser preview, etc.) live on the
  // AnalyzedInvoice extension type below — NOT on this interface. This
  // type describes the JSON shape the backend actually sends.
  /** Matched unprocessed bank statement line. For non-EUR invoices the
   *  match is fuzzy via approximate FX conversion — currencyConverted is
   *  true in that case, with invoiceCurrency / invoiceAmount carrying the
   *  original (non-EUR) values so the UI can show "32.80 CHF ≈ €34.50". */
  matchedBankLine: {
    id: number;
    datum: string;
    bedrag: number;
    omschrijving: string;
    currencyConverted?: boolean;
    invoiceCurrency?: string;
    invoiceAmount?: number;
  } | null;
}

/** InvoiceAnalyzeResponse plus client-side enrichments added by the inbox
 *  after a successful /invoices/analyze call. These fields keep the
 *  original PDF File reachable in-memory so:
 *   - the review dialog can preview it via a blob: URL without R2,
 *   - the submit handlers can re-encode it as base64 and ship inline.
 *  None of these fields ever round-trip through JSON; they're purely
 *  client-side state. Keeping them off InvoiceAnalyzeResponse avoids
 *  pretending they could come from the backend. */
export interface AnalyzedInvoice extends InvoiceAnalyzeResponse {
  /** Object URL created via URL.createObjectURL on the original File.
   *  Must be revoked by the owning component to release the file data. */
  localPdfUrl?: string;
  /** Original File for re-encoding as base64 on submit. */
  localFile?: File;
}

/** Payload for POST /api/v1/invoices/submit-full */
export interface InvoiceSubmitFullRequest {
  datum: string;
  leverancier: string;
  factuurnummer: string;
  omschrijving: string;
  bedragExcl: number;
  bedragIncl: number;
  btwBedrag: number;
  btwCode: string;
  inEx: string;
  relatieId: number;
  tegenRekeningId: number;
  rekeningId: number;
  uploadKey: string;
  filename: string;
  /** Base64-encoded PDF; alternative to uploadKey for clients that don't use
   *  R2. The backend prefers pdfBase64 when both are present. */
  pdfBase64?: string;
  /** Archive folder ID picked by the user. When omitted the backend either
   *  auto-creates Inkoopfacturen/year/month (if R2 is configured) or skips
   *  archiving entirely. */
  folderId?: number;
  importId?: number;
  /** Echo of the analyze response's pdfHash so the backend can write a
   *  duplicate-detection marker. Optional for legacy clients. */
  pdfHash?: string;
}

/** One additional booking line within a single receipt mutation. Used to
 *  split a restaurant tip (no BTW) from the food portion (9% BTW), or
 *  any case where one bonnetje needs to span multiple BTW codes. */
export interface ReceiptExtraLine {
  bedragExcl: number;
  bedragIncl: number;
  btwBedrag: number;
  btwCode: string;
  /** Optional — defaults to the main line's tegenrekening when omitted,
   *  which is the right behaviour for the common tip case (same expense
   *  account, just a different BTW treatment). */
  tegenRekeningId?: number;
  /** Optional per-line label (e.g. "Fooi"). */
  omschrijving?: string;
}

/** Payload for POST /api/v1/invoices/submit-receipt — bonnetje without relation */
export interface InvoiceSubmitReceiptRequest {
  datum: string;
  leverancier: string;
  omschrijving: string;
  bedragExcl: number;
  bedragIncl: number;
  btwBedrag: number;
  btwCode: string;
  tegenRekeningId: number;
  uploadKey: string;
  filename: string;
  /** Base64-encoded PDF; see InvoiceSubmitFullRequest.pdfBase64. */
  pdfBase64?: string;
  /** Archive folder ID; see InvoiceSubmitFullRequest.folderId. */
  folderId?: number;
  importId?: number;
  /** Bank account internal ID — required when importId is not provided. */
  bankAccountId?: number;
  /** Additional regels for split bookings (e.g. food + tip). */
  extraLines?: ReceiptExtraLine[];
}

// Settings
export type EntityType = "BV" | "ZZP" | "EM" | "ANDERS" | "";

export interface SettingsResponse {
  hasApiKey: boolean;
  hasSoapCredentials: boolean;
  hasRestAccessToken: boolean;
  preferences: { entityType?: EntityType } & Record<string, unknown>;
}

// SOAP API types (raw JSON from e-boekhouden, Dutch field names)
export interface OpenPost {
  factuurnummer: string;
  relatie: string;
  relatieId: number;
  datum: string;
  bedrag: number;
  openstaand: number;
  vervalDatum?: string;
}

export interface Saldo {
  code: string;
  omschrijving: string;
  saldo: number;
}

export interface Mutatie {
  mutatieNr: number;
  datum: string;
  rekening: string;
  soort: string;
  bedrag: number;
  omschrijving: string;
}

export interface Artikel {
  id: number;
  code: string;
  omschrijving: string;
  prijs: number;
  btwCode: string;
  grootboekrekening: string;
}

export interface Kostenplaats {
  id: number;
  omschrijving: string;
}

// REST API types
export interface RestInvoice {
  id: number;
  factuurnummer: string;
  relatie: string;
  datum: string;
  bedrag: number;
  status: string;
}

export interface RestInvoicesResponse {
  items: RestInvoice[];
  totalCount: number;
}

export interface EmailTemplate {
  id: number;
  naam: string;
}

export interface InvoiceLineItem {
  quantity: number;
  description: string;
  pricePerUnit: number;
  vatCode: string;
  ledgerId: number;
}

export interface CreateInvoiceRequest {
  relatieId: number;
  betalingstermijn: number;
  sjabloonId?: number;
  factuurnummer?: string;
  datum: string;
  regels: InvoiceLineItem[];
}

export interface RestCostCenter {
  id: number;
  omschrijving: string;
}

// Inbox (AI-classified bank lines)
export type InboxCategory = "auto" | "review" | "invoice" | "manual";

export interface InboxClassification {
  id: number;
  datum: string;
  bedrag: number;
  omschrijving: string;
  rekening: string;
  grootboekId: number;
  category: InboxCategory;
  needsInvoice: boolean;
  confidence: number;
  grootboekcode: string;
  btwCode: string;
  soort: MutatieSoort;
  aiOmschrijving: string;
  indicator: string;
  /** True when this row was filled in from the user's learned classification
   *  memory rather than a fresh Claude call. The UI shows a small badge. */
  learned?: boolean;
}

export interface LearnedClassification {
  signal: string;
  grootboekcode: string;
  btwCode: string;
  soort: string;
  count: number;
  sampleOmschrijving: string;
  createdAt: string;
  updatedAt: string;
  confirmedAt?: string | null;
}

export interface InboxClassifyResponse {
  classifications: InboxClassification[];
  totalCount: number;
  summary: Record<InboxCategory, number>;
}

export interface InboxSummary {
  unprocessedCount: number;
  classificationSummary: Record<InboxCategory, number>;
  overdueCount: number;
  overdueTotal: number;
  hasApiKey: boolean;
  hasSoap: boolean;
  hasRest: boolean;
  eboekhoudenConnected: boolean;
}

export interface InboxProcessItem {
  id: number;
  grootboekId: number;
  soort: number;
  grootboekcode: string;
  btwCode: string;
  omschrijving: string;
  bedrag: number;
  relatieId?: number;
  factuurnummer?: string;
}

export interface InboxProcessResult {
  status: "ok" | "error";
  mutNr?: string;
  error?: string;
}

export interface InboxBatchResponse {
  results: InboxProcessResult[];
}

export interface InvoiceMatchResponse {
  invoice: InvoiceData;
  uploadKey: string;
  bankLineId: number;
  amountMatch: boolean;
  amountDiff: number;
}

// WebAuthn
export interface WebAuthnBeginResponse {
  options: PublicKeyCredentialCreationOptions | PublicKeyCredentialRequestOptions;
  challengeId: string;
}

// Mutation types
export type MutatieSoort =
  | "FactuurOntvangen"
  | "FactuurVerstuurd"
  | "FactuurbetalingOntvangen"
  | "FactuurbetalingVerstuurd"
  | "GeldOntvangen"
  | "GeldUitgegeven"
  | "Memoriaal";

export const MUTATIE_SOORT_CODES: Record<MutatieSoort, number> = {
  FactuurOntvangen: 1,
  FactuurVerstuurd: 2,
  FactuurbetalingOntvangen: 3,
  FactuurbetalingVerstuurd: 4,
  GeldOntvangen: 5,
  GeldUitgegeven: 6,
  Memoriaal: 7,
};
