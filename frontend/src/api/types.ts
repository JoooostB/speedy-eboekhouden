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
  confidence: number;
  redenering: string;
  belastingAdvies: Array<{ type: string; tekst: string }>;
}

export interface InvoiceAnalyzeResponse {
  invoice: InvoiceData;
  filename: string;
  uploadKey: string;
  /** Public CDN URL for the uploaded PDF (from R2) */
  pdfUrl: string;
  matchedRelation: {
    id: number;
    code: string;
    bedrijf: string;
  } | null;
  /** Internal ID of the crediteuren (1600) account */
  crediteurenId: number;
  /** Matched unprocessed bank statement line (by amount) */
  matchedBankLine: {
    id: number;
    datum: string;
    bedrag: number;
    omschrijving: string;
  } | null;
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
  importId?: number;
}

// Settings
export interface SettingsResponse {
  hasApiKey: boolean;
  hasSoapCredentials: boolean;
  hasRestAccessToken: boolean;
  preferences: Record<string, unknown>;
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
