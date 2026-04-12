import type {
  AuthResponse,
  BankStatementsResponse,
  BulkRequest,
  BulkResponse,
  ClassifyRequest,
  ClassifyResult,
  CreateInvoiceRequest,
  EBStatusResponse,
  EmailTemplate,
  Employee,
  Project,
  Activity,
  InboxBatchResponse,
  InboxClassifyResponse,
  InboxProcessItem,
  InboxSummary,
  InvoiceAnalyzeResponse,
  InvoiceMatchResponse,
  InvoiceSubmitFullRequest,
  InvoiceSubmitReceiptRequest,
  LedgerAccount,
  MeResponse,
  Mutatie,
  OpenPost,
  Relation,
  RestCostCenter,
  RestInvoicesResponse,
  Saldo,
  SettingsResponse,
  VATCode,
} from "./types";

const BASE = "/api/v1";

class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

async function request<T>(
  path: string,
  options?: RequestInit,
): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    ...options,
  });

  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    const message = body.message || body.error || res.statusText;

    if (res.status === 401 && !path.startsWith("/auth/") && !path.startsWith("/eboekhouden/")) {
      window.dispatchEvent(new CustomEvent("auth:expired"));
    }

    // 412 Precondition Failed with eboekhouden_session_expired means the
    // upstream e-boekhouden cookie is no longer valid. The backend has
    // already cleared the stored token; the frontend needs to update its
    // own connection state and prompt the user to reconnect.
    if (res.status === 412 && body.error === "eboekhouden_session_expired") {
      window.dispatchEvent(new CustomEvent("eb:session-expired"));
    }

    throw new ApiError(res.status, message);
  }

  return res.json();
}

export const api = {
  // Passkey auth
  registerBegin(email: string, name: string) {
    return request<{ options: unknown; challengeId: string }>("/auth/register/begin", {
      method: "POST",
      body: JSON.stringify({ email, name }),
    });
  },

  registerFinish(challengeId: string, credential: unknown) {
    return fetch(`${BASE}/auth/register/finish`, {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        "X-Challenge-ID": challengeId,
      },
      body: JSON.stringify(credential),
    }).then(async (res) => {
      if (!res.ok) throw new ApiError(res.status, (await res.json()).error);
      return res.json() as Promise<{ status: string; user: unknown }>;
    });
  },

  loginBegin() {
    return request<{ options: unknown; challengeId: string }>("/auth/login/begin", {
      method: "POST",
    });
  },

  loginFinish(challengeId: string, credential: unknown) {
    return fetch(`${BASE}/auth/login/finish`, {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        "X-Challenge-ID": challengeId,
      },
      body: JSON.stringify(credential),
    }).then(async (res) => {
      if (!res.ok) throw new ApiError(res.status, (await res.json()).error);
      return res.json() as Promise<{ status: string; user: unknown }>;
    });
  },

  logout() {
    return request<{ status: string }>("/auth/logout", { method: "POST" });
  },

  recoverRequest(email: string) {
    return request<{ status: string }>("/auth/recover", {
      method: "POST",
      body: JSON.stringify({ email }),
    });
  },

  recoverBegin(token: string) {
    return request<{ options: unknown; challengeId: string; userId: string }>("/auth/recover/begin", {
      method: "POST",
      body: JSON.stringify({ token }),
    });
  },

  recoverFinish(challengeId: string, userId: string, credential: unknown) {
    return fetch(`${BASE}/auth/recover/finish`, {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        "X-Challenge-ID": challengeId,
        "X-User-ID": userId,
      },
      body: JSON.stringify(credential),
    }).then(async (res) => {
      if (!res.ok) throw new ApiError(res.status, (await res.json()).error);
      return res.json() as Promise<{ status: string; user: unknown }>;
    });
  },

  me() {
    return request<MeResponse>("/auth/me");
  },

  // e-Boekhouden connection
  ebLogin(email: string, password: string) {
    return request<AuthResponse>("/eboekhouden/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
  },

  ebMfa(code: string) {
    return request<AuthResponse>("/eboekhouden/mfa", {
      method: "POST",
      body: JSON.stringify({ code }),
    });
  },

  ebStatus() {
    return request<EBStatusResponse>("/eboekhouden/status");
  },

  ebDisconnect() {
    return request<{ status: string }>("/eboekhouden/disconnect", { method: "POST" });
  },

  ebKeepalive() {
    return request<{ alive: boolean; reason?: string }>("/eboekhouden/keepalive");
  },

  // Hours (existing features)
  getEmployees() {
    return request<Employee[]>("/employees");
  },

  getProjects() {
    return request<Project[]>("/projects");
  },

  getActivities() {
    return request<Activity[]>("/activities");
  },

  submitHours(data: BulkRequest) {
    return request<BulkResponse>("/hours", {
      method: "POST",
      body: JSON.stringify(data),
    });
  },

  // Bank statements
  getBankStatements(offset = 0, limit = 2000) {
    return request<BankStatementsResponse>(`/bankstatements?offset=${offset}&limit=${limit}`);
  },

  getBankStatementCount() {
    return request<{ count: number }>("/bankstatements/count");
  },

  getBankStatementSuggestion(id: number, params: Record<string, string>) {
    const qs = new URLSearchParams(params).toString();
    return request<unknown>(`/bankstatements/${id}/suggestion?${qs}`);
  },

  processBankStatement(id: number, data: unknown) {
    return request<{ mutNr: number; mutId: number }>(`/bankstatements/${id}/process`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  },

  getLastMutatieData() {
    return request<unknown[]>("/bankstatements/lastdata");
  },

  // Mutations
  createMutation(data: unknown) {
    return request<{ mutNr: number; mutId: number }>("/mutations", {
      method: "POST",
      body: JSON.stringify(data),
    });
  },

  // Reference data
  getLedgerAccounts() {
    return request<LedgerAccount[]>("/ledger-accounts");
  },

  searchRelations(query: string) {
    return request<Relation[]>(`/relations?q=${encodeURIComponent(query)}`);
  },

  getVATCodes() {
    return request<VATCode[]>("/vat-codes");
  },

  // Archive
  getArchiveFolders() {
    return request<unknown[]>("/archive/folders");
  },

  createArchiveFolder(data: { parentFolderId: number; name: string }) {
    return request<unknown>("/archive/folders", {
      method: "POST",
      body: JSON.stringify(data),
    });
  },

  uploadArchiveFile(data: { fileName: string; data: string; overwrite: boolean; folderId: number }) {
    return request<{ folderId: number; saved: boolean }>("/archive/upload", {
      method: "POST",
      body: JSON.stringify(data),
    });
  },

  linkFileToMutation(data: { koppelId: number; folders: { id: number; soort: string }[]; koppelType: string }) {
    return request<unknown>("/archive/link", {
      method: "POST",
      body: JSON.stringify(data),
    });
  },

  // Invoice processing
  analyzeInvoice(file: File) {
    const formData = new FormData();
    formData.append("file", file);
    return fetch(`${BASE}/invoices/analyze`, {
      method: "POST",
      credentials: "include",
      body: formData,
    }).then(async (res) => {
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }));
        throw new ApiError(res.status, body.error || body.message || res.statusText);
      }
      return res.json() as Promise<InvoiceAnalyzeResponse>;
    });
  },

  submitInvoice(data: unknown) {
    return request<{ mutNr: number; mutId: number }>("/invoices/submit", {
      method: "POST",
      body: JSON.stringify(data),
    });
  },

  submitInvoiceFull(data: InvoiceSubmitFullRequest) {
    return request<{ mutNr: number; mutId: number }>("/invoices/submit-full", {
      method: "POST",
      body: JSON.stringify(data),
    });
  },

  submitReceipt(data: InvoiceSubmitReceiptRequest) {
    return request<{ mutNr: number; mutId: number; archived: boolean; linked: boolean }>(
      "/invoices/submit-receipt",
      {
        method: "POST",
        body: JSON.stringify(data),
      },
    );
  },

  // Claude classification
  classifyTransaction(data: ClassifyRequest) {
    return request<ClassifyResult>("/classify", {
      method: "POST",
      body: JSON.stringify(data),
    });
  },

  // Inbox (AI-classified bank lines)
  classifyInbox(force = false) {
    const qs = force ? "?force=true" : "";
    return request<InboxClassifyResponse>(`/inbox/classify${qs}`, { method: "POST" });
  },

  getInboxSummary() {
    return request<InboxSummary>("/inbox/summary");
  },

  processInboxBatch(items: InboxProcessItem[]) {
    return request<InboxBatchResponse>("/inbox/process-batch", {
      method: "POST",
      body: JSON.stringify({ items }),
    });
  },

  matchInvoice(id: number, file: File, metadata?: Record<string, string>) {
    const formData = new FormData();
    formData.append("file", file);
    if (metadata) {
      Object.entries(metadata).forEach(([k, v]) => formData.append(k, v));
    }
    return fetch(`${BASE}/inbox/${id}/match-invoice`, {
      method: "POST",
      credentials: "include",
      body: formData,
    }).then(async (res) => {
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }));
        throw new ApiError(res.status, body.error || body.message || res.statusText);
      }
      return res.json() as Promise<InvoiceMatchResponse>;
    });
  },

  // KvK + Relations
  searchKvK(query: string) {
    return request<Array<{ kvkNummer: string; bedrijf: string; plaats: string; adres: string; vestigingsnummer: string }>>(`/kvk/search?q=${encodeURIComponent(query)}`);
  },

  getKvKAddress(vestigingsnummer: string) {
    return request<{ straatnaam: string; huisnummer: number; huisletter: string; postcode: string; plaats: string; land: string; volledigAdres: string }>(`/kvk/address/${encodeURIComponent(vestigingsnummer)}`);
  },

  createRelation(data: unknown) {
    return request<{ id: number; code: string; bedrijf: string }>("/relations", {
      method: "POST",
      body: JSON.stringify(data),
    });
  },

  // Avatar
  uploadAvatar(file: File) {
    const formData = new FormData();
    formData.append("avatar", file);
    return fetch(`${BASE}/avatar`, {
      method: "POST",
      credentials: "include",
      body: formData,
    }).then(async (res) => {
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }));
        throw new ApiError(res.status, body.error || res.statusText);
      }
      return res.json() as Promise<{ avatarKey: string; avatarUrl: string }>;
    });
  },

  deleteAvatar() {
    return request<{ status: string }>("/avatar", { method: "DELETE" });
  },

  // Settings
  getSettings() {
    return request<SettingsResponse>("/settings");
  },

  // Passkey management
  listPasskeys() {
    return request<{ passkeys: Array<{ id: string; friendlyName: string; createdAt: string; transport: string[] }> }>(
      "/settings/passkeys",
    );
  },

  renamePasskey(id: string, friendlyName: string) {
    return request<{ status: string }>(`/settings/passkeys/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify({ friendlyName }),
    });
  },

  deletePasskey(id: string) {
    return request<{ status: string }>(`/settings/passkeys/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  },

  // Learned classifications (AI memory)
  listLearned() {
    return request<{
      learned: Array<{
        signal: string;
        grootboekcode: string;
        btwCode: string;
        soort: string;
        count: number;
        sampleOmschrijving: string;
        createdAt: string;
        updatedAt: string;
        confirmedAt?: string | null;
      }>;
    }>("/settings/learned");
  },

  deleteLearned(signal: string) {
    return request<{ status: string }>(
      `/settings/learned/item?signal=${encodeURIComponent(signal)}`,
      { method: "DELETE" },
    );
  },

  deleteAllLearned() {
    return request<{ status: string }>("/settings/learned?confirm=true", {
      method: "DELETE",
    });
  },

  checkApiKeyStatus() {
    return request<{ status: string; message?: string }>("/settings/api-key/status");
  },

  setApiKey(apiKey: string) {
    return request<{ status: string }>("/settings/api-key", {
      method: "PUT",
      body: JSON.stringify({ apiKey }),
    });
  },

  deleteApiKey() {
    return request<{ status: string }>("/settings/api-key", { method: "DELETE" });
  },

  // SOAP credentials
  setSoapCredentials(username: string, securityCode1: string, securityCode2: string) {
    return request<{ status: string }>("/settings/soap-credentials", {
      method: "PUT",
      body: JSON.stringify({ username, securityCode1, securityCode2 }),
    });
  },

  deleteSoapCredentials() {
    return request<{ status: string }>("/settings/soap-credentials", { method: "DELETE" });
  },

  // REST token
  setRestToken(accessToken: string) {
    return request<{ status: string }>("/settings/rest-token", {
      method: "PUT",
      body: JSON.stringify({ accessToken }),
    });
  },

  deleteRestToken() {
    return request<{ status: string }>("/settings/rest-token", { method: "DELETE" });
  },

  setEntityType(entityType: "BV" | "ZZP" | "EM" | "ANDERS" | "") {
    return request<{ status: string; entityType: string }>("/settings/entity-type", {
      method: "PUT",
      body: JSON.stringify({ entityType }),
    });
  },

  // SOAP API endpoints
  getOpenPosten(soort: "Debiteuren" | "Crediteuren") {
    return request<OpenPost[]>(`/soap/openposten?soort=${encodeURIComponent(soort)}`);
  },

  getSaldi(datumVan: string, datumTot: string) {
    return request<Saldo[]>(`/soap/saldi?datumVan=${encodeURIComponent(datumVan)}&datumTot=${encodeURIComponent(datumTot)}`);
  },

  getSoapRelaties(q?: string) {
    const qs = q ? `?q=${encodeURIComponent(q)}` : "";
    return request<Relation[]>(`/soap/relaties${qs}`);
  },

  getSoapMutaties(datumVan?: string, datumTot?: string, mutatieNr?: number) {
    const params = new URLSearchParams();
    if (datumVan) params.set("datumVan", datumVan);
    if (datumTot) params.set("datumTot", datumTot);
    if (mutatieNr) params.set("mutatieNr", String(mutatieNr));
    const qs = params.toString();
    return request<Mutatie[]>(`/soap/mutaties${qs ? `?${qs}` : ""}`);
  },

  getArtikelen() {
    return request<unknown[]>("/soap/artikelen");
  },

  getKostenplaatsen() {
    return request<unknown[]>("/soap/kostenplaatsen");
  },

  // REST API endpoints
  getRestInvoices(limit?: number, offset?: number) {
    const params = new URLSearchParams();
    if (limit !== undefined) params.set("limit", String(limit));
    if (offset !== undefined) params.set("offset", String(offset));
    const qs = params.toString();
    return request<RestInvoicesResponse>(`/rest/invoices${qs ? `?${qs}` : ""}`);
  },

  createRestInvoice(data: CreateInvoiceRequest) {
    return request<unknown>("/rest/invoices", {
      method: "POST",
      body: JSON.stringify(data),
    });
  },

  getRestCostCenters() {
    return request<RestCostCenter[]>("/rest/costcenters");
  },

  getEmailTemplates() {
    return request<EmailTemplate[]>("/rest/emailtemplates");
  },
};

export { ApiError };
