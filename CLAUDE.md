# CLAUDE.md

Instructions for Claude Code instances working on this repository.

## Project Overview

Speedy e-Boekhouden is a multi-tenant SaaS platform that supercharges [e-boekhouden.nl](https://www.e-boekhouden.nl). Features include a bookkeeping inbox for unprocessed bank lines with AI classification, a document inbox for the e-boekhouden "Mail & herken" folder (documents mailed to the administration are listed, analysed with Claude, matched to bank lines or open invoices, booked, then renamed and filed in the archive), invoice OCR for uploaded PDF/JPG/PNG files, and bulk hour logging. Users authenticate with passkeys (WebAuthn) and connect their e-boekhouden account on top of that.

## Architecture

Five services behind a single nginx entry point, plus two optional external services:

- **`landing/`** — Static HTML/CSS landing page, security page, and disclaimer. Served by nginx. Acts as the public entry point and reverse proxy for `/app/` and `/api/`.
- **`frontend/`** — React 19 + TypeScript + MUI 7 SPA, built with Vite. Served at `/app/` with `base: '/app/'`. Uses react-router-dom for client-side routing.
- **`backend/`** — Go 1.26 + Gin API server. Handles auth, proxies calls to e-boekhouden.nl, integrates the Claude API.
- **`postgres`** — PostgreSQL 18 (`postgres:18-alpine` in docker-compose and `deploy/base`). Accounts, passkeys, encrypted credentials and tokens, learned classifications. See Security Model for the full list.
- **`redis`** — Redis (`redis:8-alpine` in docker-compose, `redis:7-alpine` in `deploy/base`). Sessions, rate-limit counters and the AI caches. See "Redis keys".
- **Cloudflare R2** (optional, `R2_*` env) — avatars and files uploaded through `/invoices/analyze`. Not used for mail-folder documents.
- **SMTP** (optional, `SMTP_*` env) — welcome and passkey-recovery e-mails.

```
Browser → landing nginx (:3000)
              ├── /           → static landing page
              ├── /beveiliging → security page
              ├── /disclaimer  → disclaimer page
              ├── /app/       → proxy to frontend nginx (:80)
              └── /api/       → proxy to backend (:8080)
                                    ├── PostgreSQL (:5432)
                                    ├── Redis (:6379)
                                    ├── e-boekhouden.nl (HTTPS)
                                    ├── api.anthropic.com (HTTPS, user's key)
                                    └── R2 / SMTP (optional)
```

Frontend routes (`App.tsx`): `/` is the bookkeeping inbox (`BookkeepingInbox`, tabs "Afschriften" and "Documenten", state in `?lijst=`), `/uren` the hour form, `/instellingen` settings, `/herstel` passkey recovery. `Dashboard.tsx` and the `bankstatements/`, `invoices/`, `openitems/`, `salesinvoices/`, `balances/` and `mutations/` components still exist but are not routed.

## Key Technical Decisions

- **Passkey-only authentication.** No passwords. WebAuthn via go-webauthn library. Challenges stored in sync.Map (single-instance).
- **Two-level auth.** Users first authenticate with their Speedy passkey, then connect e-boekhouden. e-boekhouden credentials (password, MFA code) are never persisted; the resulting auth token is encrypted and kept in the Redis session and in PostgreSQL, so the connection survives a Speedy logout/login (`restoreEBToken` in `passkey.go`).
- **PostgreSQL for accounts.** Migrations run automatically on startup via embedded SQL (`internal/database/migrations/`).
- **Redis for sessions and caches.** Sessions have a 30-minute sliding TTL. AI results are cached per user with TTLs (see "Redis keys"); nothing in Redis is needed for correctness, only for cost and speed.
- **Encryption.** Anthropic API keys, the e-boekhouden token and the optional SOAP/REST credentials are AES-256-GCM encrypted with the server-side `ENCRYPTION_KEY` before storage.
- **Claude API integration.** Invoice/document reading uses Claude Sonnet 4.5 (vision/PDF, also JPG and PNG). Bank-line classification uses Claude Haiku 4.5. Users provide their own API key; Claude is only called when the user triggers it (classify, analyse, "Alles analyseren"), never automatically on connect.
- **One analysis core.** `analyzeBytes` in `handler/invoice.go` serves both `/invoices/analyze` (upload) and `/documents/:id/analyze` (mail folder). The mail-folder path runs with `skipR2: true`: the file already lives in e-boekhouden, so it is never copied to R2 and the response carries no `uploadKey`/`pdfUrl`.
- **Mail folder by id, never by name.** The "Mail & herken" folder is `menhFolderId` from `GET /v1/api/folder/0/header`. Its name and path are the user's business ("Inbox" for one user, anything for the next).
- **Raw JSON passthrough.** The backend does not deserialize most e-boekhouden API responses; it passes `json.RawMessage` to the frontend with `c.Data()`. Exceptions: the import grid and the open-post grid (column-indexed, parsed into named rows), the archive file list and folder header (typed in `eboekhouden/archive.go`), and the documents endpoints (built server-side).
- **Frontend builds in containers only.** There is no local Node.js setup. All `npm install`, `npm run build`, `tsc` and `vitest` runs happen inside Docker. Never run npm locally.
- **`base: '/app/'` in Vite.** The frontend is served at `/app/` via the landing nginx proxy. `BrowserRouter basename="/app"` matches.
- **Rate limiting.** Redis-based, per IP, disabled when `COOKIE_SECURE=false` (local dev): registration and recovery 5 per 15 min, passkey login 20 per 5 min, e-boekhouden login/MFA 10 per 5 min (`cmd/server/main.go`).

## Backend Package Structure

| Package | Purpose |
|---------|---------|
| `internal/auth` | WebAuthn passkey service |
| `internal/claude` | Claude API service: `ReadInvoice` (PDF/JPG/PNG → `InvoiceData` incl. `documentType` factuur/bonnetje/creditnota/overig), bank-line classification, `NormalizeDocumentType` |
| `internal/crypto` | AES-256-GCM encrypt/decrypt |
| `internal/database` | PostgreSQL connection, migrations, CRUD (users, teams, passkeys, settings, recovery tokens, learned classifications) |
| `internal/eboekhouden` | HTTP client for e-boekhouden.nl. `client.go` (auth, `apiGet/apiPost/apiPut/apiDelete/apiGetBinary`, `ErrSessionExpired`), `archive.go` (typed folder header, file list, download, rename, bulk move, delete, set mail folder), `openpost.go` (open-post grid without SOAP), `testing.go` (`NewClientForTesting`), plus hours, import grid, mutatie, reference, SOAP/REST |
| `internal/handler` | Gin HTTP handlers. Document inbox: `documents.go` (endpoints, Redis keys, list builder, global bank-line assignment), `documents_status.go` (status rules, titles, nl-NL formatting), `filename.go` (archive file-name contract, `uniqueName`, `archiveMailDocument` rename+move), `invoice_archive.go` (archive phases of SubmitFull/SubmitReceipt: prepare, stage, complete), `matching.go` (`rankBankLineMatches`, scores), `inbox_overlay.go` (factuurbetaling payload, open-post matcher, document annotation overlay on the classify cache), `fx.go` (approximate FX for matching only). Also `invoice.go` (analysis core, SubmitFull/SubmitReceipt), `inbox.go`, `archive.go`, `passkey.go`, `auth.go`, `settings.go`, `learned_settings.go`, `avatar.go`, `soapapi.go`, `restapi.go`, `eboekhouden_errors.go` (`HandleEBoekhoudenSessionExpired`) |
| `internal/middleware` | CORS + security headers, rate limiting |
| `internal/session` | Redis-backed session store + middleware (`RequireEBoekhouden`) |
| `internal/storage` | Cloudflare R2 client (S3 API); `nil` when not configured |
| `internal/mail` | SMTP mailer (welcome, recovery) |
| `internal/config` | Environment-based configuration |

## Frontend Structure

| Path | Purpose |
|------|---------|
| `src/components/inbox/BookkeepingInbox.tsx` | Home: tabs "Afschriften" (bank lines) and "Documenten" (mail folder); opens `InvoiceReviewDialog` for uploads, bank lines and mail-folder documents |
| `src/components/inbox/DocumentList.tsx` | "Documenten" tab: sections "Klaar om te boeken", "Te doen", "Al verwerkt in e-Boekhouden", "Wacht op betaling" (open-post grid), "Genegeerde documenten"; "Alles analyseren" with progress; move/delete dialogs |
| `src/components/inbox/DocumentRow.tsx` | One mail-folder file: chip, primary action, ⋮ menu (Bekijken, Negeren, Verplaatsen naar…, Verwijderen uit e-Boekhouden) |
| `src/components/inbox/MailFolderSetupCard.tsx` | No mail folder: "Mailmap instellen" / "Bestaande map kiezen"; empty folder: where the mail address lives |
| `src/components/inbox/AiStatusAlert.tsx` | API-key / credit / invalid-key alerts for both tabs |
| `src/components/inbox/InboxRow.tsx` | Bank line with "Betaalt open factuur" → "Koppelen" and "Document gevonden" → "Factuur koppelen en boeken" |
| `src/components/inbox/InvoiceReviewDialog.tsx` | Review + book; for a mail-folder document shows the "Verplaatsen naar" folder picker and the open-booking alert |
| `src/components/inbox/mailDocumentReview.ts` | Pure helpers for booking a mail-folder document (accepted types, preview kind, `isOpenBooking`, button labels, folder helper text, result line) |
| `src/components/inbox/invoiceSubmitPayload.ts` | Builds `/invoices/submit-full` and `/submit-receipt` bodies for both sources (upload vs `archiveFileId`) |
| `src/components/inbox/inboxBooking.ts` | Factuurbetaling rules shared by row, batch bar and dialog |
| `src/hooks/useMailDocuments.ts` | State + actions of the documents tab (load, analyze, analyzeAll with concurrency 3 and 409 retries, ignore, remove, archive, setup, open posts) |
| `src/hooks/mailDocumentsModel.ts` | Pure model: sections, "Alles analyseren" targets, applying an analyse response, titles, nl-NL formatting |
| `src/hooks/useArchiveFolders.ts` | Folder tree with paths, `findArchiveFolderForSupplier` |
| `src/utils/runWithConcurrency.ts` | Abortable concurrency helper used by "Alles analyseren" |
| `src/components/auth/`, `onboarding/`, `settings/`, `shared/` | Passkey login/register/recovery, setup wizard, settings (API key, SOAP/REST, passkeys, "Geleerde boekingen", avatar), pickers (ledger, relation, VAT, archive folder) |
| `src/components/` | Layout (tabs "Inbox", "Uren"), BulkEntryForm, EBoekhoudenConnectDialog; `Dashboard.tsx` is no longer routed |
| `src/hooks/` | Data fetching hooks (employees, projects, activities, ledger accounts, VAT codes) |
| `src/context/` | AuthContext (two-level: Speedy account + e-boekhouden connection) |
| `src/api/` | API client (`client.ts`, incl. `requestBlob` for document content) + TypeScript types (`MailDocument`, `MailDocumentStatus`, `OpenPostGridItem`, `SubmitArchiveOutcome`, …) |
| `src/analytics.ts` | `track()` wrapper around Plausible (counts and flags only, never bookkeeping data) |

Unit tests (vitest) sit next to the modules: `*.test.ts` in `components/inbox/`, `hooks/` and `utils/`.

## Document Inbox API (`/api/v1`, Session + e-boekhouden)

| Method | Path | Body / query | Response |
|--------|------|--------------|----------|
| `GET` | `/documents` | — | `{configured:false}`, `{configured:false, reason:"missing"}` (folder deleted), or `{configured:true, folder:{id,name}, documents:[MailDocument], ignoredCount}`. No Claude or download calls. |
| `POST` | `/documents/setup` | `{folderId?}` | `{folder:{id,name}}`. Without `folderId` a root folder "Inbox" is reused or created and set as mail folder. 409 when a mail folder is already configured and exists; 404 when `folderId` does not exist. |
| `POST` | `/documents/:id/analyze` | `?force=true` bypasses both caches | The `/invoices/analyze` response plus `archiveFileId`, `title`, `subtitle`, `status`, `analyzedAt`. Cached 30 days. 409 `Wordt al geanalyseerd` while a lock exists. |
| `GET` | `/documents/:id/content` | — | File bytes inline (PDF/JPEG/PNG sniffed, 415 otherwise), `Cache-Control: no-store`. |
| `POST` | `/documents/:id/ignore` | — | `{id, ignored:true}`. Speedy-only; nothing changes in e-boekhouden. |
| `DELETE` | `/documents/:id/ignore` | — | `{id, ignored:false}`. Skips the mail-folder check so a vanished file can be cleaned out. |
| `DELETE` | `/documents/:id` | — | `{id, deleted:true}`. Permanent delete in e-boekhouden; 409 when the file is linked (`koppelCount > 0`). |
| `POST` | `/documents/archive` | `{fileIds:[…] (max 100), targetFolderId?}` | `{moved:[{id,name,targetFolderId}], failed:[{id,error}]}`. Renames by the naming convention and moves out of the mail folder; without target `Facturen/<jaar>/<maand>` per file date. |
| `GET` | `/openposts` | `?soort=Crediteur|Debiteur` | `[]OpenPost` (`mutDatum, sBedrag, mutFactuur, mutNr, relatieCode, relatieId, bedrijf, sBetaald, teOntvangen, vervaldatum, mutOmschrijving, mutSoort`). |

Error mapping for all of the above (`writeDocumentsError`): expired e-boekhouden session → 412 `eboekhouden_session_expired` (token cleared); no mail folder → 409 `mail_folder_not_configured`; file not in the mail folder → 404; other upstream errors → 502; no Redis → 503.

`MailDocument` fields: `id, naam, datum, size, mediaType ("" = unsupported extension), koppelCount, status, title, subtitle, ignored`, and for analysed documents `documentType, analyzedAt, analysis` (the cached analyze response with the current `matchedBankLine`/`bankLineCandidates`), `match`, `candidates`, `alreadySubmitted`.

Statuses in priority order (`documentStatus` in `documents_status.go`): `linked` (koppelCount > 0), `ignored`, `unsupported`, `junk` (unanalysed image < 15 KiB), `duplicate` (an `invoice:submitted` marker exists for the bytes), `new`, `not_invoice` (analysed as "overig"), `matched` (assigned a bank line), `receipt_waiting` (bonnetje without a line), `unmatched`. Bank lines are assigned globally (`assignDocumentsToBankLines`, best score first, deterministic) so `GET /documents` and the bank-line inbox agree on which document pays which line.

**SubmitFull / SubmitReceipt** (`POST /invoices/submit-full`, `/submit-receipt`) accept a third document source next to `pdfBase64` and `uploadKey`:

- `archiveFileId` — a file in the mail folder. Nothing is uploaded; the file is linked to the mutation, renamed by the convention and moved. At most one source may be given.
- `targetFolderId` — optional explicit target; must exist, not be deleted, and be neither the mail folder nor a descendant of it (400 `De mailmap kan geen doelmap zijn` / `Een submap van de mailmap kan geen doelmap zijn`). `0` means `Facturen/<jaar>/<maand>`.
- `documentType` (SubmitFull only) — `"creditnota"` or anything else (= factuur). Only affects the archive name.

Both responses always carry `archived, linked, renamed, moved` and, when set, `archiveName, targetFolderId, targetFolderPath, archiveWarning`. A failed rename/move never fails the booking: the mutation stands and `archiveWarning` says what state the file is in (rename failed → untouched in the mail folder; move failed → renamed, still in the mail folder, linked, so "Alles opruimen" can finish it).

`GET /inbox/summary` includes `documentsCount` (mail-folder files that are not ignored, not linked and readable). `POST /inbox/classify` returns the cached classification base with two overlays recomputed on every response, cache hit or not: bank lines paying an open post become factuurbetaling suggestions (`openPostMatch`, `relatieId`, `factuurnummer`), and lines that are the assigned payment of an analysed document carry `documentMatch {fileId, title}`.

## e-Boekhouden Endpoints Learned for This Feature

All on `https://secure20.e-boekhouden.nl` with the `auth-token` cookie. Files and folders share one id space.

| Call | Notes | Client method |
|------|-------|---------------|
| `GET /v1/api/folder/{id}/header` | Works for any folder id, including `0`. Returns `menhFolderId` (the Mail & herken folder; `0` or missing = not configured), `credits`, `dataLimit`, `dataUsed`, `directoryInfo`. | `GetFolderHeader` |
| `POST /v1/api/administratie/menhfolder`, body is the bare number `90460978` | Sets the mail folder. Returns `true`. | `SetMailFolder` |
| `GET /v1/api/folder/directory` | All folders incl. `isDeleted`. `parentId: 0` is the implicit root ("Basismap" is not a real folder). | `GetArchiveFolders` |
| `POST /v1/api/folder/directory` `{parentFolderId, name}` | `{id, naam, parentId, isDeleted}` | `CreateArchiveFolder` |
| `GET /v1/api/folder/{id}/filelist?includeDelete=false` | `[{koppelCount, id, parentId, soort "F"/"D", naam, datum "2026-10-24T15:43:35.67", size, isDeleted}]` | `ListArchiveFiles` (files only, not deleted) |
| `GET /v1/api/folder/{fileId}/download` | Raw bytes with `Content-Type` and `Content-Disposition`. Speedy caps at 10 MB. | `DownloadArchiveFile` |
| `PUT /v1/api/folder/filename` `{folderId:<fileId>, parentFolderId:<current folder>, name}` | Rename in place; the id field is called `folderId`. Returns `true`. | `RenameArchiveFile` |
| `POST /v1/api/folder/bulk/update/{targetFolderId}`, body `[fileId, …]` | `{updatedIds, notFoundIds, errorMessages, successMessages}`. The file keeps its id, so links survive. Success only when the id is in `updatedIds`. | `MoveArchiveFiles` |
| `DELETE /v1/api/folder/{fileId}` | Permanent. Returns `true`. | `DeleteArchiveFile` |
| `POST /v1/api/folder/upload` `{fileName, data (base64), overwrite, folderId}` | Returns the folder id, not the file id: list the folder again and find the file by name. | `UploadArchiveFile` |
| `POST /v1/api/folderkoppel/koppel/item` `{koppelId: mutNr, folders:[{id, soort:"F"}], koppelType:"MUT"}` | Links a file to a mutation. | `LinkFileToMutation` |
| `GET /v1/api/openpost/gridtable/openpost?offset=0&limit=500&orderBy=&sortDirection=1&soort=Crediteur` (or `Debiteur`) | Column-indexed grid with `colMetadata`; resolved by column name. Works without SOAP. | `GetOpenPostGrid` + `ParseOpenPostGrid` |
| `POST /v1/api/mutatie` with soort 3/4 | A factuurbetaling has one regel `{index, bedrag, btw:0, btwCode:"GEEN", relatieId, factuur}` plus `importId`; no `tegenRekening`, no `inEx`, no BTW split. | `buildFactuurbetalingRegel`, SubmitFull step 3 |

The folder endpoints answer a bare JSON boolean; `expectTrue` turns a literal `false` into an error.

## Archive File-Name Contract

Implemented as the pure function `archiveFileName(leverancier, kenmerk, datum, docType, mediaType)` in `backend/internal/handler/filename.go`, used by uploads and mail-folder documents alike. Other tools and MCPs may parse these names, so treat the format as a contract:

```
{datum}_{leverancier}_{kenmerk}[_{n}].{ext}
2026-10-12_Anthropic_INV-0042.pdf
2026-10-03_Albert-Heijn_bonnetje.jpg
2026-09-30_Coolblue_2026-0042.pdf        invoice number "2026/0042"
2026-09-30_Coolblue_2026-0042_2.pdf      name conflict in the target folder
```

| Field | Rule |
|-------|------|
| `datum` | Invoice date as `YYYY-MM-DD`; without one the file's received date, then today (callers fall back before calling; the function itself emits `0000-00-00` for unparseable input). First so the list sorts chronologically and the name never starts with `-`, `.` or a Windows-reserved word. |
| `leverancier` | NFKD transliteration with combining marks removed ("Café" → "Cafe", plus ß/æ/ø/ł/… table). Trailing legal forms removed (B.V., BV, N.V., V.O.F., Inc., Ltd, LLC, GmbH, PBC, S.A., SARL, …; `legalFormTokens`, shared with the matcher). Every other character becomes `-`, runs collapse, no leading/trailing `-`. Case preserved. Max 40 characters. Empty → `onbekend`. |
| `kenmerk` | Invoice number cleaned the same way (`/` → `-`), max 30. Empty → `bonnetje`, `creditnota` or `document` by `docType`. |
| `n` | Only on a conflict in the target folder: `_2`, `_3`, … (`uniqueName`, case-insensitive against the folder's current files). |
| `ext` | Lowercase from the media type: `pdf`, `jpg` (never `jpeg`), `png`. Unsupported files keep their own extension (e.g. `xml`), cleaned to `[a-z0-9]`, max 10; empty → `bin`. |

Rules: only `[A-Za-z0-9._-]`; `_` is **exclusively** the field separator, so `split("_")` gives `[datum, leverancier, kenmerk]` or `[datum, leverancier, kenmerk, n.ext]`; `.` appears only before the extension; max 100 characters in total (field limits keep it well below, the guard trims supplier then reference); deterministic. Titles shown in the UI (`documentTitle`) are a different thing and may contain anything.

## Redis Keys

`{v}` is `invoicePromptVersion` in `handler/invoice.go` (currently `2`). Bump it when the invoice prompt's output schema changes in a way `NormalizeDocumentType` cannot paper over; both versioned caches then miss and refill.

| Key | TTL | Contents |
|-----|-----|----------|
| `session:{uuid}` | 30 min sliding | `SessionData`; the e-boekhouden token inside is AES-256-GCM encrypted |
| `ratelimit:{register\|login\|ebauth}:{ip}` | 15 min / 5 min / 5 min | request counters |
| `inbox:classify:{userID}` | 1 h | cached bank-line classification base: per line id, date, amount, description, suggested ledger/VAT code, soort, confidence (overlays are never cached) |
| `invoice:analyze:v{v}:{userID}:{sha256(file)}:{accountsHash}` | 7 days | `InvoiceData` of an analysed upload (supplier, number, dates, amounts, suggestion) |
| `invoice:submitted:{userID}:{sha256(file)}` | 90 days | duplicate marker: `mutNr, paymentMutNr, leverancier, factuur, bedragIncl, submittedAt`. Deliberately not versioned. |
| `doc:analysis:v{v}:{userID}:{fileID}` | 30 days | `{pdfHash, analyzedAt, response}` — the full analyze response of a mail-folder document |
| `doc:analyzing:{userID}:{fileID}` | 120 s | lock while an analysis runs (released on return with a fresh context) |
| `doc:ignored:{userID}` | none | set of ignored file ids |

`forgetDocument` drops `doc:analysis` (current version only) and the ignore flag when a file leaves the mail folder (moved or deleted). WebAuthn challenges and pending MFA state live in process memory, not Redis.

## Common Pitfalls

1. **Field names from e-boekhouden are Dutch** — `naam` (not `name`), `relatieBedrijf` (not `companyName`), `id` (lowercase). The frontend types in `api/types.ts` must match.
2. **Two auth layers** — The `auth:expired` event in `api/client.ts` must NOT fire for `/auth/*` paths. The session middleware checks Speedy auth; `RequireEBoekhouden` middleware checks the e-boekhouden connection. New handlers pass e-boekhouden errors through `HandleEBoekhoudenSessionExpired` so the frontend gets a 412 and shows the reconnect prompt instead of a bare 502.
3. **Cookie domain** — Leave `COOKIE_DOMAIN` empty for local dev.
4. **MFA is optional** — Not all e-boekhouden users have MFA. The login handler detects it by checking the response HTML for `txtCode` or `SCODE`.
5. **ENCRYPTION_KEY is required** — The server refuses to start without it. For local dev, `.env` has a zero-key.
6. **MFA-pending sessions** — The middleware does NOT create an e-boekhouden client when `MFAPending=true`. This prevents access to bookkeeping endpoints during incomplete MFA.
7. **Grid format** — `/v1/api/import/gridtable` and `/v1/api/openpost/gridtable/openpost` return column-indexed arrays, not named objects. The Go side parses them into named rows (`ParseImportGrid` by position, `ParseOpenPostGrid` via `colMetadata`).
8. **Never look up the mail folder by name.** Always `menhFolderId` from the folder header. The folder can have any name and any depth; the default "Inbox" is only what `POST /documents/setup` creates when asked. A target folder for a booked document must never be the mail folder or one of its subfolders (`mailFolderTargetError`).
9. **Factuurbetaling regels (soort 3/4) have no `tegenRekening`, `inEx` or BTW split.** e-boekhouden finds the open invoice through `relatieId` + `factuur` and silently refuses the payment when those fields are present. `buildFactuurbetalingRegel` is the one place that builds this shape.
10. **Credit notes carry positive amounts.** `BedragInclBTW` is positive for every document type; `documentType: "creditnota"` carries the sign. The matcher looks for incoming bank lines for a creditnota and outgoing for factuur/bonnetje; `SubmitFull` only uses `documentType` for the archive name.
11. **The analysis cache keys include `invoicePromptVersion`.** Bump it when the invoice prompt's output schema changes; `invoice:submitted` is intentionally unversioned so duplicates booked before a bump are still recognised.
12. **Mail-folder documents are never uploaded to R2.** `analyzeOptions{skipR2: true}`: R2 never deletes objects and a presigned URL must not end up in a 30-day cache. Only `/invoices/analyze` (uploads) and avatars touch R2.
13. **Status and overlays are recomputed on every request.** Only the classification base and the document analyses are cached; the open-post overlay, the document↔bank-line assignment and the statuses are derived fresh. Do not cache `matchedBankLine`/`documentMatch`.
14. **Log ids and error classes, not content.** Never log file names, supplier names, amounts or descriptions; upstream rename/move errors may echo the file name, so log `%T` there.

## Running Locally

```bash
docker compose up --build
# Landing page: http://localhost:3000
# App: http://localhost:3000/app/
# Backend API: http://localhost:8080
# PostgreSQL: localhost:5432 (speedy/dev)
# Redis: localhost:6379
```

Host port 3000 may be taken by an unrelated container. Use a compose override instead of editing `docker-compose.yml`:

```bash
printf 'services:\n  landing:\n    ports: !override ["3100:80"]\n' > /tmp/compose.port.yml
docker compose -f docker-compose.yml -f /tmp/compose.port.yml up -d --build
./test/smoke.sh http://localhost:3100
docker compose -f docker-compose.yml -f /tmp/compose.port.yml down
```

For backend-only iteration (requires running PostgreSQL and Redis):

```bash
cd backend
POSTGRES_DSN=postgres://speedy:dev@localhost:5432/speedy?sslmode=disable \
REDIS_URL=redis://localhost:6379 \
ENCRYPTION_KEY=0000000000000000000000000000000000000000000000000000000000000000 \
FRONTEND_ORIGIN=http://localhost:3000 \
COOKIE_SECURE=false \
go run ./cmd/server
```

## Testing

A change is done when all of this is green:

- **Backend:** `cd backend && go build ./... && go vet ./... && go test ./... -race`. Handler tests do not need a real e-boekhouden: `eboekhouden.NewClientForTesting(srv.URL, token)` points a client at an `httptest.Server` that plays e-boekhouden (see `newFakeEBoekhouden` and `submitTestEnv` in `handler/invoice_archive_test.go`, and `handler/documents_test.go`). Handlers are constructed without Redis, R2 or a database where the test allows it (`&InvoiceHandler{}`); `h.redis == nil` paths are real code paths.
- **Frontend, in Docker only:** the builder stage already runs `tsc` (`npm run build` is `tsc && vite build`); run the vitest suite from that stage:

  ```bash
  docker build --target builder -t speedy-frontend-test ./frontend
  docker run --rm speedy-frontend-test npx vitest run
  ```

- **Images:** `docker compose build` (the landing image bakes in the HTML).
- **Smoke:** `./test/smoke.sh [base_url]` against a running stack (see the port-3100 override above; the script talks to the backend on `localhost:8080` directly for the CORS and header checks).
- **Manual:** `TESTPLAN.md`, in particular the "Documents (Mail & herken)" section for the behaviour only a real administration can confirm (rename conflicts, bulk move, folder header values).

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Backend listen port |
| `FRONTEND_ORIGIN` | `http://localhost:3000` | CORS allowed origin |
| `COOKIE_DOMAIN` | *(empty)* | Cookie domain (leave empty for local) |
| `COOKIE_SECURE` | `true` | `false` for local HTTP dev; also disables rate limiting |
| `POSTGRES_DSN` | `postgres://speedy:dev@localhost:5432/speedy?sslmode=disable` | PostgreSQL connection string |
| `REDIS_URL` | `redis://localhost:6379` | Redis connection string |
| `WEBAUTHN_ORIGIN` | *(FRONTEND_ORIGIN)* | WebAuthn relying party origin |
| `WEBAUTHN_RP_ID` | `localhost` | WebAuthn relying party ID (domain) |
| `ENCRYPTION_KEY` | *(required)* | 64 hex chars (32 bytes) for AES-256-GCM |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` | *(optional)* | Welcome and recovery e-mails; no mail without a host |
| `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET`, `R2_JURISDICTION`, `R2_PUBLIC_URL` | *(optional)* | Cloudflare R2 for avatars and uploaded invoice files; the storage client is `nil` without account id, key id and bucket |

## Content Guidelines

- **Landing page and frontend UI are in Dutch.** All user-facing text must be in Dutch.
- **Code comments, README, CONTRIBUTING, and technical docs are in English.**
- **Dutch compound nouns are one word** — `kalenderselectie`, not `kalender selectie`; `mailmap`, `doelmap`, `archiefnaam`. Watch for this.
- **"word je" not "wordt je"** — when "je" follows the verb (inversion), the -t drops.
- **No personal information in the repo.** The LICENSE copyright is "Speedy e-Boekhouden Contributors". Use fictitious examples in docs and tests (`1234567@e-Boekhouden.nl`, "Voorbeeld Leverancier"), never real administration numbers, names or IBANs from HAR files or fixtures.
- **No user data in logs.** Never log credentials, tokens, API keys, or bookkeeping data (file names, suppliers, amounts, descriptions included).

## Deployment

- **Docker Compose** for local dev (port 3000).
- **Kubernetes** with Kustomize overlays (`deploy/base/`, `deploy/overlays/local/`, `deploy/overlays/production/`). Secrets (`backend-secrets`, incl. `ENCRYPTION_KEY`, SMTP and R2) are applied manually from a gitignored `secret.yaml`.
- **Traefik ingress** with Let's Encrypt TLS in production.
- **GHCR** for container images. CI builds on push to main, release pushes tagged images on `v*` tags.

## Security Model

This is critical context: the security page (`landing/beveiliging.html`) and the disclaimer make specific claims that must stay truthful. Verify against the migrations and handlers before changing either.

- **e-Boekhouden credentials are never stored.** Password and MFA code are forwarded to e-boekhouden.nl and discarded within the request handler.
- **The e-boekhouden auth token is stored, encrypted.** AES-256-GCM in the Redis session (30-minute sliding TTL) and in PostgreSQL `user_settings.eb_token_enc`, from where `LoginFinish` restores it at the next passkey login. It is cleared by `POST /eboekhouden/disconnect`, by a failed keepalive, and whenever a handler sees `ErrSessionExpired`. Note: the frontend currently has no button that calls `/eboekhouden/disconnect`.
- **Passkey-only Speedy auth.** No passwords. PostgreSQL holds public keys, credential flags (`backup_eligible` etc., so synced passkeys work) and the WebAuthn user handle.
- **PostgreSQL stores** (`internal/database/migrations/`): `users` (email, name, `avatar_key`, `webauthn_handle`), `passkey_credentials`, `teams` and `team_members`, `user_settings` (encrypted Anthropic key, encrypted e-boekhouden token, encrypted SOAP credentials, encrypted REST token, `preferences` JSON), `recovery_tokens` (SHA-256 hash only, 15-minute expiry, single use), `learned_classifications` (per user: a signal built from the normalised bank description plus counterparty IBAN, the chosen `grootboekcode`, `btw_code`, `soort`, a count and one `sample_omschrijving`; the user can list and delete these under Instellingen → Geleerde boekingen). No mutations, relations, balances or bank statements.
- **Redis stores** the keys listed under "Redis keys". The AI caches contain bookkeeping data: supplier names, invoice numbers, amounts, bank-line descriptions and the chosen ledger codes, for 1 hour (classification), 7 days (upload analysis), 30 days (mail-folder analysis) or 90 days (duplicate markers). `doc:ignored` holds file ids only and has no TTL. Everything is scoped per user id.
- **R2 (when configured) stores** avatars under `avatars/{uuid}.{ext}` (served from `R2_PUBLIC_URL`) and files analysed through `/invoices/analyze` under `uploads/{uuid}/document.{ext}` so SubmitFull/SubmitReceipt can fetch them by `uploadKey`. The application never deletes these upload objects; any retention has to come from a bucket lifecycle rule. Mail-folder documents are never written to R2.
- **Claude receives** a document only when the user starts an analysis or classification, only with the user's own key, over HTTPS to api.anthropic.com. The key is decrypted per call and never logged.
- **Cookies are HttpOnly + SameSite=Lax**, and Secure when `COOKIE_SECURE=true` (production). Session ids are random UUIDs.
- **All traffic to e-boekhouden.nl is HTTPS** (`secure.e-boekhouden.nl`, `secure20.e-boekhouden.nl`) with no HTTP fallback.
- **Rate limiting** per IP on the auth endpoints (see Key Technical Decisions for the numbers), disabled in local dev.
- **File size limits:** 10 MB for uploads (`MaxMultipartMemory`, `maxInvoiceFileSize`) and for mail-folder downloads (`DownloadArchiveFile` cap).
- **Analytics:** Plausible (self-hosted, cookieless) is loaded on the landing pages and in the app; `track()` sends event names with counts/flags only. Never add bookkeeping data to an event.
