# CLAUDE.md

Instructions for Claude Code instances working on this repository.

## Project Overview

Speedy e-Boekhouden is a multi-tenant SaaS platform that supercharges [e-boekhouden.nl](https://www.e-boekhouden.nl). Features include a bookkeeping inbox for unprocessed bank lines with AI classification, a document inbox for the e-boekhouden "Mail & herken" folder (documents mailed to the administration are listed, analysed with Claude, matched to bank lines or open invoices, booked, then renamed and filed in the archive), invoice OCR for uploaded PDF/JPG/PNG files, and bulk hour logging. Users authenticate with passkeys (WebAuthn) and connect their e-boekhouden account on top of that.

## Architecture

Five services behind a single nginx entry point, plus two optional external services:

- **`landing/`** — Static HTML/CSS landing page, security page, and disclaimer. Served by nginx. In docker compose it is the public entry point and reverse proxy for `/app/` and `/api/`; in Kubernetes the Traefik ingress routes `/api` to the backend and `/app` to the frontend directly (`deploy/base/ingress.yaml`).
- **`frontend/`** — React 19 + TypeScript + MUI 7 SPA, built with Vite. Served at `/app/` with `base: '/app/'`. Uses react-router-dom for client-side routing.
- **`backend/`** — Go 1.26 + Gin API server. Handles auth, proxies calls to e-boekhouden.nl, integrates the Claude API.
- **`postgres`** — PostgreSQL 18 (`postgres:18-alpine` in docker-compose and `deploy/base`). Accounts, passkeys, encrypted credentials and tokens, learned classifications. See Security Model for the full list.
- **`redis`** — Redis (`redis:8-alpine` in docker-compose, `redis:7-alpine` in `deploy/base`). Sessions, rate-limit counters and the AI caches. See "Redis keys".
- **Cloudflare R2** (optional, `R2_*` env) — avatars only. No document is ever written to it (see Security Model). Whether R2 is configured still decides if an upload booked without an archive folder is archived (`stageArchiveFile`, see SubmitFull below).
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
- **Redis for sessions and caches.** Sessions have a 30-minute sliding TTL in Redis; the session middleware re-issues the cookie (`session.SetCookie`, `Max-Age` 30 minutes) on every authenticated request, so cookie and TTL slide together. AI results are cached per user with TTLs (see "Redis keys"); those caches only save cost and time. Sessions and the ignore list (`doc:ignored`) are real state, and the document endpoints answer 503 without Redis.
- **Encryption.** Anthropic API keys, the e-boekhouden token and the optional SOAP/REST credentials are AES-256-GCM encrypted with the server-side `ENCRYPTION_KEY` before storage.
- **Claude API integration.** Invoice/document reading uses Claude Sonnet 4.5 (vision/PDF, also JPG and PNG). Bank-line classification uses Claude Haiku 4.5. Users provide their own API key. Documents are only analysed when the user triggers it ("Analyseren", "Alles analyseren", an upload). Bank lines are classified automatically: `BookkeepingInbox` calls `POST /inbox/classify` whenever the inbox loads while connected (cached 1 h per user), and with `force=true` after "Vernieuwen" and after the review dialog books something; lines explained by a confirmed learned booking or by an open post (name hit) are not sent to Claude. Neither are **verification payments** (`detectVerificationPayments` in `inbox_verification.go`, pure, decided before the learned lookup): a card check (`card_check`: card wording such as `Betaalpas`/`eCom`/`TERMBNET`/`KAARTNUMMER`, ≤ €2,00 with an opposite-sign partner of the same amount and reference token (`P` + ≥ 6 digits) at most 3 days apart, or ≤ €1,00 alone) or an IBAN check (`iban_check`: 0 < |bedrag| < €1,00 without card wording, both directions; confidence 1 up to €0,10, then linear to 0,6 at €0,99, `auto` when ≥ 0,9). They are booked as Geld ontvangen/uitgegeven on **Bijzondere baten** with BTW GEEN (`findBijzondereBaten`: `omschrijving` matching `/bijzondere\s+baten/i` in `actieveGBR`, fallback code 9300; without either the row is `review` and the user picks the account). The row carries `verification {kind, pairLineId}` (deterministic, cached with the base); overlays keep their precedence. `BatchProcess` re-detects them on the grid and never learns them: the learned signal ignores the amount, so a learned "merchant → 9300" would swallow the merchant's real charges.
- **One analysis core.** `analyzeBytes` in `handler/invoice.go` serves both `/invoices/analyze` (upload) and `/documents/:id/analyze` (mail folder). Neither path keeps a copy of the file: an upload stays in the browser and is inlined as `pdfBase64` when booked, a mail-folder document already lives in e-boekhouden. `uploadKey` and `pdfUrl` in the response are always empty and only kept for the shape.
- **The relation's default ledger account wins.** `analyzeBytes` returns `matchedRelation.grootboekrekeningId` (the first relation search hit's default account, internal id, same id space as actieveGBR; looked up with `GET /v1/api/relatie/{id}` when the search hit lacks it, `relation.go`). The review dialog uses it as tegenrekening before Claude's `grootboekcode` and re-applies it when the user picks another relation, unless they chose the account by hand (`invoiceLedger.ts`); an unknown default (null, or missing in an older cached analysis) is fetched through `GET /api/v1/relations/:id`, which answers only `{id, code, bedrijf, grootboekrekeningId}`. Crediteuren, debiteuren, bank and kas accounts are never taken as a default.
- **Mail folder by id, never by name.** The "Mail & herken" folder is `menhFolderId` from `GET /v1/api/folder/0/header`. Its name and path are the user's business ("Inbox" for one user, anything for the next).
- **Raw JSON passthrough.** The backend does not deserialize most e-boekhouden API responses; it passes `json.RawMessage` to the frontend with `c.Data()`. Exceptions: the import grid and the open-post grid (column-indexed, parsed into named rows), the archive file list and folder header (typed in `eboekhouden/archive.go`), and the documents endpoints (built server-side).
- **Frontend builds in containers only.** There is no local Node.js setup. All `npm install`, `npm run build`, `tsc` and `vitest` runs happen inside Docker. Never run npm locally.
- **`base: '/app/'` in Vite.** The frontend is served at `/app/` via the landing nginx proxy. `BrowserRouter basename="/app"` matches.
- **Rate limiting.** Redis-based, per IP, disabled when `COOKIE_SECURE=false` (local dev): registration and recovery 5 per 15 min, passkey login 20 per 5 min, e-boekhouden login/MFA 10 per 5 min (`cmd/server/main.go`).

## Backend Package Structure

| Package | Purpose |
|---------|---------|
| `internal/auth` | WebAuthn passkey service |
| `internal/claude` | Claude API service: `ReadInvoice` (PDF/JPG/PNG → `InvoiceData` incl. `documentType` factuur/bonnetje/creditnota/overig, and since prompt version 3 the optional split proposal `regels []InvoiceRegel {omschrijving, bedragIncl, btwPercentage, btwCode, grootboekcode, soort: product\|verzending\|fooi\|korting\|overig}` plus `splitsAdvies {reden: wkr\|btw_tarieven\|fooi\|verzending_apart\|representatie\|overig, toelichting}`, both `omitempty`; the prompt asks for them only when the split is tax-relevant: several BTW rates, a WKR gift next to shipping, a tip, representation next to other items), bank-line classification, `NormalizeDocumentType`, `NormalizeRegels` (drops a proposal with fewer than 2 or more than 8 lines, a non-positive line, or lines that do not add up to `bedragInclBtw` within a cent, together with the advice; run on fresh and cached analyses). `readInvoiceMaxTokens` is 6144. The regels and the advice are never logged |
| `internal/crypto` | AES-256-GCM encrypt/decrypt |
| `internal/database` | PostgreSQL connection, migrations, CRUD (users, teams, passkeys, settings, recovery tokens, learned classifications) |
| `internal/eboekhouden` | HTTP client for e-boekhouden.nl. Every web-API request (login, MFA and logout included) is bounded by `requestTimeout` (60 s); the SOAP and REST clients have their own 30 s timeout. `client.go` (auth, `apiGet/apiPost/apiPut/apiDelete/apiGetBinary`, `ErrSessionExpired`), `archive.go` (typed folder header, file list, download, rename, bulk move, delete, set mail folder), `openpost.go` (open-post grid without SOAP), `mutatie.go` (`CreateMutatie`, `GetMutatieModel` + `ParseMutatieTotals`), `testing.go` (`NewClientForTesting`), plus hours, import grid, mutatie, reference, SOAP/REST |
| `internal/handler` | Gin HTTP handlers. Document inbox: `documents.go` (endpoints, Redis keys, list builder, global bank-line assignment), `documents_status.go` (status rules, titles, nl-NL formatting), `filename.go` (archive file-name contract, `uniqueName`, `archiveMailDocument` rename+move), `invoice_archive.go` (archive phases of SubmitFull/SubmitReceipt: prepare, stage, complete), `matching.go` (`rankBankLineMatches`, scores), `inbox_overlay.go` (factuurbetaling payload, open-post matcher, document annotation overlay on the classify cache), `inbox_groups.go` (payment groups: several incoming lines paying one sales invoice, the BatchProcess open-post guard), `inbox_verification.go` (card and IBAN checks → Bijzondere baten, `findBijzondereBaten`, the `upsertLearned` seam), `fx.go` (approximate FX for matching only), `regels.go` (split bookings: `regelInput`, `normalizeRegels` with an optional grootboekcode resolver for the bank-line split, `regelBTW` per regel, `mutatieRegelMaps`). Also `invoice.go` (analysis core, SubmitFull/SubmitReceipt), `inbox.go`, `archive.go`, `passkey.go`, `auth.go`, `settings.go`, `learned_settings.go`, `avatar.go`, `soapapi.go`, `restapi.go`, `eboekhouden_errors.go` (`HandleEBoekhoudenSessionExpired`) |
| `internal/middleware` | CORS + security headers, rate limiting |
| `internal/session` | Redis-backed session store + middleware (`RequireEBoekhouden`) |
| `internal/storage` | Cloudflare R2 client (S3 API); `nil` when not configured |
| `internal/mail` | SMTP mailer (welcome, recovery) |
| `internal/config` | Environment-based configuration |

## Frontend Structure

| Path | Purpose |
|------|---------|
| `src/components/inbox/BookkeepingInbox.tsx` | Home: tabs "Afschriften" (bank lines) and "Documenten" (mail folder); opens `InvoiceReviewDialog` for uploads, bank lines and mail-folder documents |
| `src/components/inbox/DocumentList.tsx` | "Documenten" tab: sections "Klaar om te boeken", "Te doen", "Al verwerkt in e-Boekhouden", "Wacht op betaling" (open-post grid, Crediteur only, loaded on first expand), "Genegeerd" (behind "Toon genegeerd"); "Alles analyseren" with progress; move/delete dialogs |
| `src/components/inbox/DocumentRow.tsx` | One mail-folder file: chip, primary action (`primaryActionFor` in `mailDocumentsModel.ts`: Analyseren, Boeken, Opruimen, Negeren, Niet meer negeren, Opnieuw), ⋮ menu (Bekijken, Negeren / Niet meer negeren, Toch analyseren, Toch boeken, Opnieuw analyseren, Verplaatsen naar…, Verwijderen uit e-Boekhouden; the last is hidden for linked files) |
| `src/components/inbox/MailFolderSetupCard.tsx` | No mail folder: "Mailmap instellen" / "Bestaande map kiezen"; empty folder: where the mail address lives |
| `src/components/inbox/AiStatusAlert.tsx` | API-key / credit / invalid-key alerts for both tabs |
| `src/components/inbox/InboxRow.tsx` | Bank line with "Betaalt open factuur" → "Koppelen" and "Document gevonden" → "Factuur koppelen en boeken"; an expanded incoming line offers "Hoort bij een openstaande factuur" (opens `PaymentGroupDialog`). `TruncatedText.tsx` is the shared ellipsis-with-tooltip text |
| `src/components/inbox/PaymentGroupRow.tsx` | One `paymentGroup` in the Afschriften list (its lines leave the flat list, `buildRenderList`): chip "Betaling in delen" / "Deelbetaling" / "Kies factuur", summary line, read-only sub-rows (date, amount, own account as "{code} {omschrijving}", description), "Koppelen (n)" / "Kiezen" |
| `src/components/inbox/PaymentGroupDialog.tsx` | Books N lines as factuurbetalingen on one sales invoice: fixed invoice, radio group of candidates (ambiguous) or picker over `api.getOpenPosts("Debiteur")` (manual, also from `BatchApproveBar` "Koppelen aan één factuur"); include/exclude lines, "Betaling toevoegen", sticky `role="status"` summary that blocks an overpayment; stays open after a partial failure with "Opnieuw proberen (n)" |
| `src/components/inbox/VerificationPairRow.tsx` | A card check and its refund as one row (chip "Kaartcontrole" as a button described by its tooltip, "−€ 1,00 en +€ 1,00 heffen elkaar op · boeken op {code} {omschrijving}", read-only sub-rows, "Boeken (2)"). The confirm dialog is owned by `BookkeepingInbox` (`pairDialog`, a `BookingConfirmDialog` with `confirmLabel`), so after a partial failure it survives the booked leg leaving the list and shows the failed leg with "Opnieuw proberen (1)". `verificationPairs.ts` holds the pure rules (`buildVerificationPairs` from mutual `pairLineId`s, never a grouped line; `withPairPartners` so a pair shows whole when either leg passes the category filter; copy; `failedPairLines`, `pairResultAlert`); tested. A single verification row in `InboxRow` gets the chip, the caption and a one-click "Boeken" (outlined, like "Koppelen"); the chip style and copy, `isVerification`, `ledgerLabel` and `amountAriaLabel` live in `inboxBooking.ts` |
| `src/components/inbox/paymentGroups.ts` | Pure rules and copy of payment groups (sums in cents, fit state, labels, `buildGroupPayload`, `buildRenderList`, picker sorting); tested |
| `src/components/inbox/InvoiceReviewDialog.tsx` | Review + book; for a mail-folder document shows the "Verplaatsen naar" folder picker and the open-booking alert. "Splitsen over meerdere regels" (invoice and receipt) and the "Splits in fooi" hint swap the amount fields for `RegelsEditor`; "Niet splitsen" keeps regel 1's values. Tracks `booking_split {path: factuur\|bonnetje, regels: n, proposed, reden}` |
| `src/components/inbox/proposeRegels.ts` | Pure: `proposeRegels(analysis, ledgers, mainLedger)` turns `InvoiceData.regels` + `splitsAdvies` into `RegelEdit` lines (codes resolved like the single-line suggestion, the main ledger for an unknown code; null without advice, with one line or when the lines do not add up), `undoProposal` restores the single-line values; tested. The dialog opens such a card split with the note "Speedy stelt voor deze factuur te splitsen: {toelichting}" and "Ongedaan maken" (the only way back until the user edits a line; then "Niet splitsen" takes over, `proposalEdited`); a relation change never touches the lines; `booking_split` carries `proposed`, `edited` and `reden` |
| `src/components/inbox/regels.ts` | Pure rules of a split booking (cents, `regelBtwCents` per regel like e-boekhouden, `remainderCents`, `splitFromSingle`, `splitWithTip`, `addRegel`, `validateRegels`, `remainderStatus`, `toRegelPayload` with mode `id`/`code`); tested, parity with `regels.go` |
| `src/components/shared/RegelsEditor.tsx` | The lines of a split booking: a fieldset "Regel n" per line with omschrijving, amount incl. BTW (read-only BTW under it), `VATCodePicker`, `LedgerAccountPicker`, "Regel n verwijderen" (disabled at two lines); footer "Regel toevoegen" (max 10), the state "Nog te verdelen: € x" / the first line that is not ready / "Verdeeld" plus the BTW total, echoed debounced in a hidden `role="status"`; container-query layout (5 columns from 720 px) |
| `src/components/inbox/mailDocumentReview.ts` | Pure helpers for booking a mail-folder document (accepted types, preview kind, `isOpenBooking`, button labels, folder helper text, result line) |
| `src/components/inbox/invoiceSubmitPayload.ts` | Builds `/invoices/submit-full` and `/submit-receipt` bodies for both sources (upload vs `archiveFileId`); sends `regels` only with 2 or more lines and then never `extraLines` |
| `src/components/inbox/inboxBooking.ts` | Factuurbetaling rules shared by row, batch bar and dialog |
| `src/hooks/useMailDocuments.ts` | State + actions of the documents tab (load, analyze, analyzeAll with concurrency 3 and 409 retries, ignore, remove, archive, setup, open posts) |
| `src/hooks/mailDocumentsModel.ts` | Pure model: sections, "Alles analyseren" targets, applying an analyse response, titles, nl-NL formatting |
| `src/hooks/useArchiveFolders.ts` | Folder tree with paths, `findArchiveFolderForSupplier` |
| `src/utils/runWithConcurrency.ts` | Abortable concurrency helper used by "Alles analyseren" |
| `src/components/auth/`, `onboarding/`, `settings/`, `shared/` | Passkey login/register/recovery, setup wizard, settings (API key, SOAP/REST, passkeys, "Geleerde boekingen", avatar), pickers (ledger, relation, VAT, archive folder) |
| `src/components/` | Layout (tabs "Inbox", "Uren"; the connection chip connects when disconnected and opens the disconnect confirmation when connected), BulkEntryForm, EBoekhoudenConnectDialog, EBoekhoudenDisconnectDialog (texts and outcome rule in `ebDisconnect.ts`, tested); `Dashboard.tsx` is no longer routed |
| `src/hooks/` | Data fetching hooks (employees, projects, activities, ledger accounts, VAT codes) |
| `src/context/` | AuthContext (two-level: Speedy account + e-boekhouden connection) |
| `src/api/` | API client (`client.ts`, incl. `requestBlob` for document content) + TypeScript types (`MailDocument`, `MailDocumentStatus`, `OpenPostGridItem`, `SubmitArchiveOutcome`, `EBDisconnectResponse`, …) |
| `src/analytics.ts` | `track()` wrapper around Plausible: `sanitizeTrackProps` lets only enum-like tokens, counts and flags through, `errorClass()` turns an error into a token; `analytics.test.ts` scans every call site for `.message`, names, amounts and descriptions |

Unit tests (vitest) sit next to the modules: `*.test.ts` in `src/` (`analytics.test.ts`), `api/`, `components/` (`ebDisconnect.test.ts`), `components/hours/`, `components/inbox/`, `hooks/` and `utils/`. CI runs only `tsc` for the frontend, not vitest.

## Document Inbox API (`/api/v1`, Session + e-boekhouden)

| Method | Path | Body / query | Response |
|--------|------|--------------|----------|
| `GET` | `/documents` | — | `{configured:false}`, `{configured:false, reason:"missing"}` (folder deleted), or `{configured:true, folder:{id,name}, documents:[MailDocument], ignoredCount}`. No Claude or download calls. |
| `POST` | `/documents/setup` | `{folderId?}` | `{folder:{id,name}}`. Without `folderId` a root folder "Inbox" is reused or created and set as mail folder. 409 when a mail folder is already configured and exists; 404 when `folderId` does not exist. |
| `POST` | `/documents/:id/analyze` | `?force=true` bypasses both caches | The `/invoices/analyze` response plus `archiveFileId`, `title`, `subtitle`, `status`, `analyzedAt`. Cached 30 days. 409 `{error:"Dit document wordt al geanalyseerd.", code:"analysis_in_progress"}` while a lock exists; the frontend retries only on that code (`isAnalyzeBusyError`). |
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
- `targetFolderId` — optional explicit target; must exist, not be deleted, and be neither the mail folder nor a descendant of it (400 `De mailmap kan geen doelmap zijn` / `Een submap van de mailmap kan geen doelmap zijn`). `0` means `Facturen/<jaar>/<maand>` for a mail-folder document; for an upload it means `Facturen/<jaar>/<maand>` only when R2 is configured, and no archiving at all without R2 (`stageArchiveFile`).
- `documentType` (SubmitFull only) — `"creditnota"` is refused with 400 (see pitfall 10); anything else is a factuur. Otherwise it only affects the archive name.
- `regels` — a split booking: 1 to 10 lines `{omschrijving?, tegenRekeningId, btwCode, bedragIncl}` (`regelInput` in `regels.go`; `grootboekcode` instead of the id is reserved for the bank-line split and refused here). Each line needs an amount above zero and its own tegenrekening (no fallback to the request's); an empty `btwCode` is `GEEN`, then `normalizeBTWForSoort`. The lines must add up, within a cent, to `bedragIncl` (SubmitFull) or to the bank line's amount (SubmitReceipt with `importId`; `bedragIncl` without one), else 400 "De regels tellen op tot € x, het totaal is € y." before anything is staged or booked. BTW is rounded per regel like the e-boekhouden UI (`regelBTW`: excl = round-half-away-from-zero(incl / (1 + rate)), btw = incl − excl, computed in integer cents and basis points by `regelExclCents`; the frontend's `regelBtwCents` uses the same rule and both test files assert the same table), so the regels' BTW may differ a cent from the invoice's own BTW line; `index` is 0..n−1, each regel carries its own `tegenRekening` and, as in the UI's "Incl btw" mode, `bedrag` = the gross amount with the mutation's `inEx:"IN"` (the single-line paths keep `inEx:"EX"` + net `bedrag`), and only a receipt carries a per-regel `omschrijving`. With `regels` the request's `tegenRekeningId`, `btwCode` and `btwBedrag` are ignored (the frontend still sends the totals: `btwBedrag` is the sum of the per-regel BTW). The soort 4 payment stays one regel. SubmitReceipt's `extraLines` is the legacy split (lines appended after the main line, defaulting to its tegenrekening); `regels` next to `extraLines` is 400.

Both responses always carry `archived, linked, renamed, moved` and, when set, `archiveName, targetFolderId, targetFolderPath, archiveWarning`. A failed rename/move never fails the booking: the mutation stands and `archiveWarning` says what state the file is in (rename failed → untouched in the mail folder; move failed → renamed, still in the mail folder, linked, so "Alles opruimen" can finish it). An upload is staged in the archive before the mutation; when e-boekhouden refuses the booking (an error answer or `mutNr` 0) the staged file is deleted again (`discardStagedUpload`), while without an answer or with an unreadable one it stays, because the booking may exist. A mail-folder document is never deleted. An uploaded file that could not be linked also gets an `archiveWarning`. An e-boekhouden session that expires at the booking itself answers 412 `eboekhouden_session_expired`; one that expires after the booking (payment, link, folder lookup, rename, move) keeps the 200, says so in `paymentWarning` or `archiveWarning`, and still clears the stored token (`clearExpiredEBoekhoudenToken`, the part of `HandleEBoekhoudenSessionExpired` that writes no response).

`GET /inbox/summary` includes `documentsCount` (mail-folder files that are not ignored, not linked and readable; the same rule as `pendingDocumentCount` behind the Documents tab badge, so junk counts: it waits for "Negeren"). `POST /inbox/classify` returns the cached classification base with overlays recomputed on every response, cache hit or not: bank lines paying an open post become factuurbetaling suggestions (`openPostMatch`, `relatieId`, `factuurnummer`), groups of incoming lines that together pay one sales invoice are returned as `paymentGroups` (each row names its group in `paymentGroupId`), and lines that are the assigned payment of an analysed document carry `documentMatch {fileId, title}`.

**Payment groups** (`matchOpenPostGroups` in `inbox_groups.go`, pure, never cached): a customer under a G-rekening arrangement (WKA) pays one sales invoice in parts (excl − 10%, 10% of excl, the BTW), spread over the bank account and the G-rekening. Candidates for a Debiteur post are incoming lines not claimed by a name-hit single match, dated from the invoice date to +120 days, that name the invoice number (`mentionsInvoiceNumber`: both sides reduced to `[A-Z0-9]`; a truncated number counts when a prefix of ≥ 4 characters missing at most 2 ends where a token ends, so "VB0042" finds VB00420; a purely numeric number only as a whole token, never inside an IBAN) or the company (`mentionsCompany`). Never on the amount alone. A **complete** group is a subset of 2 to 6 candidates (at most 12 per post, exhaustive) summing to `teOntvangen` within a cent; `category` is `auto` when every line names the invoice, else `review`. A **partial** group (sum below `teOntvangen`, 1 line allowed, always `review`) needs strong evidence per line: the invoice number, or the company plus a WKA amount computed from the invoice model (`GET /v1/api/mutatie/nr/{mutNr}/model`, fetched best effort for at most 10 posts per request, only posts with a name-only candidate and no complete group). Proposals sharing a line, or several subsets fitting one post, merge into one **ambiguous** group with `candidates` (each with the `lineIds` that fit it) and empty invoice fields; Speedy never picks. Precedence: name-hit single match > complete group > partial group > amount-only single match (`withoutGroupedLines` drops the overruled singles). A line is in at most one group; `id` is a hash of the sorted line ids. Group JSON: `{id, state: complete|partial|ambiguous, category, relatieId, relatieNaam, factuurnummer, factuurdatum, sBedrag, sBetaald, teOntvangen, lineIds, total, candidates:[{…post, lineIds}], evidence: factuurnummer|naam}`. Rows of a group get the factuurbetaling suggestion (soort `FactuurbetalingOntvangen`, `relatieId`, `factuurnummer`; empty for an ambiguous group). On a fresh classify the lines of a complete `auto` group are not sent to Claude, like name-hit singles. Booking a group is N ordinary `POST /inbox/process-batch` items (the frontend's `buildGroupPayload`); the partially paid post stays in the grid with a lower `teOntvangen`.

**BatchProcess guard** (`factuurbetalingGuard`): before the first mutation the soort 3/4 items are grouped by (`relatieId`, `factuur`) and checked against a fresh open-post grid; every item of a group is refused with "De factuur is in e-Boekhouden veranderd: er staat nu {open} open. Controleer de betalingen en probeer het opnieuw." when the post is no longer open or the sum exceeds `teOntvangen` by more than a cent. An expired session answers 412 before anything is booked; a grid that cannot be fetched or parsed refuses that side's soort 3/4 items ("De openstaande facturen konden niet worden opgehaald uit e-Boekhouden. Probeer het opnieuw.") rather than booking blind, while the other items of the batch still go through. After the guard it is one mutation per item, one result per item, no rollback.

## e-Boekhouden Endpoints Learned for This Feature

All on `https://secure20.e-boekhouden.nl` with the `auth-token` cookie. Files and folders share one id space.

| Call | Notes | Client method |
|------|-------|---------------|
| `GET /v1/api/folder/{id}/header` | Works for any folder id, including `0`. Returns `menhFolderId` (the Mail & herken folder; `0` or missing = not configured), `credits`, `dataLimit`, `dataUsed`, `directoryInfo`. | `GetFolderHeader` |
| `POST /v1/api/administratie/menhfolder`, body is the bare number `12340001` | Sets the mail folder. Returns `true`. | `SetMailFolder` |
| `GET /v1/api/folder/directory` | All folders incl. `isDeleted`. `parentId: 0` is the implicit root ("Basismap" is not a real folder). | `GetArchiveFolders` |
| `POST /v1/api/folder/directory` `{parentFolderId, name}` | `{id, naam, parentId, isDeleted}` | `CreateArchiveFolder` |
| `GET /v1/api/folder/{id}/filelist?includeDelete=false` | `[{koppelCount, id, parentId, soort "F"/"D", naam, datum "2026-10-24T15:43:35.67", size, isDeleted}]` | `ListArchiveFiles` (files only, not deleted) |
| `GET /v1/api/folder/{fileId}/download` | Raw bytes with `Content-Type` and `Content-Disposition`. Speedy caps at 10 MB. | `DownloadArchiveFile` |
| `PUT /v1/api/folder/filename` `{folderId:<fileId>, parentFolderId:<current folder>, name}` | Rename in place; the id field is called `folderId`. Returns `true`. | `RenameArchiveFile` |
| `POST /v1/api/folder/bulk/update/{targetFolderId}`, body `[fileId, …]` | `{updatedIds, notFoundIds, errorMessages, successMessages}`. The file keeps its id, so links survive. Success only when the id is in `updatedIds`. | `MoveArchiveFiles` |
| `DELETE /v1/api/folder/{fileId}` | Permanent. Returns `true`. | `DeleteArchiveFile` |
| `POST /v1/api/folder/upload` `{fileName, data (base64), overwrite, folderId}` | Returns the folder id, not the file id: list the folder again and find the file by name. | `UploadArchiveFile` |
| `POST /v1/api/folderkoppel/koppel/item` `{koppelId: mutNr, folders:[{id, soort:"F"}], koppelType:"MUT"}` | Links a file to a mutation. | `LinkFileToMutation` |
| `GET /v1/api/relatie/{id}` | Full relation model incl. `grootboekrekeningId` (default ledger account, `0` = none). `/v1/api/relatie/search` items carry it too (`id, code, bedrijf, grootboekrekeningId, iban, kvk, btwNr, la`), so this lookup is only the fallback for a search hit without it. | `GetRelation` |
| `GET /v1/api/openpost/gridtable/openpost?offset=0&limit=500&orderBy=&sortDirection=1&soort=Crediteur` (or `Debiteur`) | Column-indexed grid with `colMetadata`; resolved by column name. Works without SOAP. | `GetOpenPostGrid` + `ParseOpenPostGrid` |
| `POST /v1/api/mutatie` with soort 3/4 | A factuurbetaling has one regel `{index, bedrag, btw:0, btwCode:"GEEN", relatieId, factuur}` plus `importId`; no `tegenRekening`, no `inEx`, no BTW split. A partial payment is the same body with only the part as `bedrag`; e-boekhouden accepts several of them on one invoice (same `relatieId` + `factuur`), also with `rekening` set to a G-rekening ledger. The web UI also sends a random negative `folderKoppelId`; Speedy never does and it works without. | `buildFactuurbetalingRegel`, SubmitFull step 3, `buildGroupPayload` |
| `POST /v1/api/mutatie` with soort 6 (or 5) on Bijzondere baten | How the web UI books the −€1 leg of a card check: `{mutatie:{rekening:<bank id>, datum, soort:6, inEx, omschrijving}, mutatieRegels:[{index:0, tegenRekening:<9300 id>, btwCode:"GEEN", bedrag:1, btw:0, bedragInclusief:1, bedragExclusief:1}], importId}` — positive `bedrag`, `importId` top-level, answer `{mutNr, mutId}`. `buildBatchMutatiePayload` already produces this; it sends `inEx:"EX"` for soort 6 where the web UI sent `"IN"`, and e-boekhouden accepts both. `actieveGBR` has no category for Bijzondere baten (`rekeningCategorie` is `VW` for every P&L account): find it by name, fallback code 9300. | `BatchProcess`, `findBijzondereBaten` |
| `GET /v1/api/openpost/gridtable/openpost?…&soort=Debiteur` after a partial payment | The post stays in the grid with `sBetaald` raised by the part and `teOntvangen` lowered; it only leaves the grid when `teOntvangen` reaches 0. | `fetchOpenPosts`, `factuurbetalingGuard` |
| `POST /v1/api/mutatie` with soort 1 and several regels | The web UI's "Factuur ontvangen" with two regels: header `{rekening, relatieId, datum, termijn, factuur, soort:1, inEx:"IN", omschrijving}` and no total; `mutatieRegels` with `index` 0..n−1, a `tegenRekening` per regel, the same `btwCode` allowed on several regels, no per-regel `omschrijving`, and BTW rounded per regel (35,80 at 9 % → excl 32,84 / btw 2,96; 3,95 → 3,62 / 0,33, so 3,29 in total where the invoice says 3,28). Captured offline (request only); a live UI check in "Incl btw" mode gives 10,11 at 9 % → btw 0,83 / excl 9,28, the same rule. Speedy's multi-regel path (SubmitFull and SubmitReceipt with `regels`) sends exactly this shape: `inEx:"IN"` and per regel `bedrag` = incl, `bedragInclusief` = incl, `bedragExclusief` = excl, `btw` = incl − excl; in "EX" mode the server might recompute btw = round(excl × rate) (0,84) and leave a cent open. The legacy single-line payloads stay `inEx:"EX"` with net `bedrag`, byte-identical. | `normalizeRegels`, `mutatieRegelMaps`, SubmitFull/SubmitReceipt `regels` |
| `GET /v1/api/mutatie/nr/{mutNr}/model` | The booked mutation behind an open post (`mutNr` from the grid): `{mutatie:{nr, relatieId, datum, soort, factuur, rekening, inEx, termijn, …}, mutatieRegels:[{index, tegenRekening, btwCode, bedrag, btw, bedragInclusief, bedragExclusief, omschrijving}], folderKoppelId}`. `ParseMutatieTotals` sums `bedragExclusief` and `btw` over the regels for the WKA amounts. | `GetMutatieModel` |

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

`{v}` is `invoicePromptVersion` in `handler/invoice.go` (currently `3`; version 3 added the proposed `regels` and `splitsAdvies` to `InvoiceData`). Bump it when the invoice prompt's output schema changes in a way `NormalizeDocumentType` cannot paper over; both versioned caches then miss and refill.

| Key | TTL | Contents |
|-----|-----|----------|
| `session:{uuid}` | 30 min sliding | `SessionData`; the e-boekhouden token inside is AES-256-GCM encrypted |
| `ratelimit:{register\|login\|ebauth}:{ip}` | 15 min / 5 min / 5 min | request counters |
| `inbox:classify:{userID}` | 1 h | cached bank-line classification base: per line id, date, amount, description, suggested ledger/VAT code, soort, confidence (overlays are never cached) |
| `invoice:analyze:v{v}:{userID}:{sha256(file)}:{accountsHash}` | 7 days | `InvoiceData` of an analysed file (supplier, number, dates, amounts, suggestion); written by `analyzeBytes` for uploads and mail-folder documents alike |
| `invoice:submitted:{userID}:{sha256(file)}` | 90 days | duplicate marker: `mutNr, paymentMutNr, submittedAt` and nothing else (entries written by older versions also carry `leverancier, factuur, bedragIncl`; they are ignored and never echoed). Written by SubmitFull only, so a booked receipt gets no marker. Deliberately not versioned. |
| `doc:analysis:v{v}:{userID}:{fileID}` | 30 days | `{pdfHash, analyzedAt, response}` — the full analyze response of a mail-folder document |
| `doc:analyzing:{userID}:{fileID}` | 120 s | lock while an analysis runs (released on return with a fresh context) |
| `doc:ignored:{userID}` | none | set of ignored file ids |
| `apikey:status:{userID}` | 15 min | SHA-256 of the stored, encrypted Anthropic key whose last `GET /settings/api-key/status` was "active". A hit answers without the billed check and without decrypting the key; refusals (`no_credits`, `invalid`, `error`) are never cached. Dropped when the key is saved or deleted (`apiKeyStatus` in `settings.go`) |

`forgetDocument` drops `doc:analysis` (current version only) and the ignore flag when a file leaves the mail folder (moved or deleted). WebAuthn challenges and pending MFA state live in process memory, not Redis.

`userCachePatterns` in `handler/userdata.go` lists the SCAN pattern of every family above except `session:*` and `ratelimit:*`; `POST /eboekhouden/disconnect` purges them with SCAN (never KEYS). `TestUserCachePatternsCoverKeyBuilders` fails as soon as a key builder is added without a pattern, and `TestUserCachePatternsAreScopedToTheUser` guards against a pattern that would reach another user's keys.

## Common Pitfalls

1. **Field names from e-boekhouden are Dutch** — `naam` (not `name`), `relatieBedrijf` (not `companyName`), `id` (lowercase). The frontend types in `api/types.ts` must match.
2. **Two auth layers** — The `auth:expired` event in `api/client.ts` must NOT fire for `/auth/*` paths. The session middleware checks Speedy auth; `RequireEBoekhouden` middleware checks the e-boekhouden connection. New handlers pass e-boekhouden errors through `HandleEBoekhoudenSessionExpired` so the frontend gets a 412 and shows the reconnect prompt instead of a bare 502.
3. **Cookie domain** — Leave `COOKIE_DOMAIN` empty for local dev.
4. **MFA is optional** — Not all e-boekhouden users have MFA. The login handler detects it by checking the response HTML for `txtCode` or `SCODE`.
5. **ENCRYPTION_KEY is required** — The server refuses to start without it. For local dev, `.env.example` has a zero-key; copy it to `.env` (gitignored, and `docker-compose.yml` reads it via `env_file`, so compose fails without it).
6. **MFA-pending sessions** — The middleware does NOT create an e-boekhouden client when `MFAPending=true`. This prevents access to bookkeeping endpoints during incomplete MFA.
7. **Grid format** — `/v1/api/import/gridtable` and `/v1/api/openpost/gridtable/openpost` return column-indexed arrays, not named objects. The Go side parses them into named rows (`ParseImportGrid` by position, `ParseOpenPostGrid` via `colMetadata`).
8. **Never look up the mail folder by name.** Always `menhFolderId` from the folder header. The folder can have any name and any depth; the default "Inbox" is only what `POST /documents/setup` creates when asked. A target folder for a booked document must never be the mail folder or one of its subfolders (`mailFolderTargetError`).
9. **Factuurbetaling regels (soort 3/4) have no `tegenRekening`, `inEx` or BTW split.** e-boekhouden finds the open invoice through `relatieId` + `factuur` and silently refuses the payment when those fields are present. `buildFactuurbetalingRegel` (`inbox_overlay.go`) builds this shape for BatchProcess; SubmitFull's step-3 payment builds the same regel inline (`invoice.go`), so change both together.
10. **Credit notes carry positive amounts and cannot be booked yet.** `BedragInclBTW` is positive for every document type; `documentType: "creditnota"` carries the sign. `SubmitFull` books a FactuurOntvangen with positive amounts, which for a credit note would be a cost instead of a credit, so it refuses `documentType: "creditnota"` (`errCreditnotaNotSupported`) and the UI offers no booking (`CREDITNOTA_NOT_BOOKABLE`): the user books it in e-boekhouden, after which the mail-folder file is `linked` and can be cleaned up. `assignsBankLine` keeps credit notes out of bank-line assignment so no line points at a refused booking; `rankBankLineMatches` still ranks incoming lines for a creditnota for when booking is supported. Supporting it needs a HAR of how e-boekhouden books a received credit note.
11. **The analysis cache keys include `invoicePromptVersion`.** Bump it when the invoice prompt's output schema changes; `invoice:submitted` is intentionally unversioned so duplicates booked before a bump are still recognised.
12. **Documents are never written to R2.** Only avatars go there. The analyser keeps no copy of an upload (the review dialog always inlines it as `pdfBase64` when booking) and mail-folder documents stay in e-boekhouden. `uploadKey` on submit is a legacy field for a client that analysed with an older version, which parked uploads in R2; do not reintroduce an upload at analyse time.
13. **Status and overlays are recomputed on every request.** Only the classification base and the document analyses are cached; the open-post overlay, the document↔bank-line assignment and the statuses are derived fresh. Do not cache `matchedBankLine`/`documentMatch`.
14. **Log ids and error classes, not content.** The policy lives in `handler/logging.go`: log e-boekhouden and Claude errors through `errClass(err)` (session-expired sentinel, or `APIError` path/status/code/length), never with `%v`; never log response bodies, Claude output, file names, supplier names, amounts, descriptions or e-mail addresses. `eboekhouden.APIError` never embeds a body, and the error of a request whose path carries user input (relation search, KvK) is not logged at all.
15. **Every new per-user Redis key needs a purge pattern.** Add it to `userCachePatterns` next to the key builder, or `TestUserCachePatternsCoverKeyBuilders` fails. `Disconnect` must keep clearing the token even when the logout or the purge fails.
16. **Analytics props are enum-like tokens, counts and flags.** `track()` drops anything else at runtime and `analytics.test.ts` scans the call sites; use `errorClass(err)`, never `err.message`.

## Running Locally

```bash
cp .env.example .env   # once; docker-compose.yml reads .env via env_file
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
| `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET`, `R2_JURISDICTION`, `R2_PUBLIC_URL` | *(optional)*; `R2_BUCKET` defaults to `speedy-eboekhouden` | Cloudflare R2 for avatars (and the read path of legacy `uploadKey`s); the storage client is `nil` without account id and key id |

## Content Guidelines

- **Landing page and frontend UI are in Dutch.** All user-facing text must be in Dutch.
- **Code comments, README, CONTRIBUTING, and technical docs are in English.**
- **Dutch compound nouns are one word** — `kalenderselectie`, not `kalender selectie`; `mailmap`, `doelmap`, `archiefnaam`. Watch for this.
- **"word je" not "wordt je"** — when "je" follows the verb (inversion), the -t drops.
- **No personal information in the repo.** The LICENSE copyright is "Speedy e-Boekhouden Contributors". Use fictitious examples in docs and tests (`1234567@e-Boekhouden.nl`, "Voorbeeld Leverancier"), never real administration numbers, names or IBANs from HAR files or fixtures.
- **No user data in logs.** Never log credentials, tokens, API keys, or bookkeeping data (file names, suppliers, amounts, descriptions included).

## Deployment

- **Docker Compose** for local dev (port 3000).
- **Kubernetes** with Kustomize (`deploy/base/`, `deploy/overlays/production/`; there is no local overlay). Secrets (`backend-secrets`, incl. `ENCRYPTION_KEY`, SMTP and R2) are applied manually from a gitignored `secret.yaml`.
- **Traefik ingress** in production; TLS from Let's Encrypt through a cert-manager `Certificate` (`deploy/base/certificate.yaml`).
- **GHCR** for container images. CI (`ci.yml`, on pushes to main and on pull requests) runs Go build/vet/`test -race`, `tsc --noEmit` and a Docker build of the three images without pushing; `release.yml` pushes `<version>-<image>`, `sha-…-<image>` and `latest-<image>` tags on `v*` tags.

## Security Model

This is critical context: the security page (`landing/beveiliging.html`), the disclaimer and the README's Security section make specific claims that must stay truthful. Verify against the migrations and handlers before changing any of them.

- **e-Boekhouden credentials are never stored.** Password and MFA code are forwarded to e-boekhouden.nl and discarded within the request handler.
- **The e-boekhouden auth token is stored, encrypted.** AES-256-GCM in the Redis session (30-minute sliding TTL) and in PostgreSQL `user_settings.eb_token_enc`, from where `LoginFinish` restores it at the next passkey login. It is cleared by a failed keepalive, whenever a handler sees `ErrSessionExpired`, and by "Verbinding verbreken".
- **Disconnect flow** (`POST /eboekhouden/disconnect`, `EBoekhoudenAuthHandler.Disconnect`): (1) best-effort `Logout()` (GET `/bh/uitloggen.asp` on the login host with the token cookie) followed by `CheckAuth()` (POST `/v1/api/auth` without body), whose `valid:false` decides `ebLoggedOut`; (2) clear the token from the session and PostgreSQL and drop a pending MFA login; (3) purge the user's caches with SCAN over `userCachePatterns`, reported as `purgedKeys`. Steps 1 and 3 never prevent step 2. The frontend (connection chip, user menu, Instellingen) shows a confirmation dialog that lists exactly this, and the snackbar only says "afgemeld bij e-Boekhouden" when `ebLoggedOut` is true. Learned classifications are not purged (preferences; Instellingen → Geleerde boekingen). Factuurbetalingen and verification payments are never learned. Only the current session's copy of the token is cleared (there is no per-user session index): another live session of the same user keeps its token and only gets 412 `eboekhouden_session_expired` on its next bookkeeping call if e-boekhouden really ended the token (`ebLoggedOut:true`). The logout is bounded by `disconnectLogoutTimeout` (15 s).
- **Passkey-only Speedy auth.** No passwords. PostgreSQL holds public keys, credential flags (`backup_eligible` etc., so synced passkeys work) and the WebAuthn user handle.
- **Passkey recovery is bound to the account of the link.** `POST /auth/recover/begin` consumes the token and starts `BeginRecoveryRegistration`: the challenge carries the account id and the new passkey gets the account's own WebAuthn handle (discoverable login refuses any other handle). `POST /auth/recover/finish` takes the account from the challenge; `X-User-ID` is only compared with it. Every challenge is single use and only finishes the ceremony that issued it (`takeChallenge`: register, login, recovery).
- **PostgreSQL stores** (`internal/database/migrations/`): `users` (email, name, `avatar_key`, `webauthn_handle`), `passkey_credentials`, `teams` and `team_members`, `user_settings` (encrypted Anthropic key, encrypted e-boekhouden token, encrypted SOAP credentials, encrypted REST token, `preferences` JSON), `recovery_tokens` (SHA-256 hash only, 15-minute expiry, single use), `learned_classifications` (per user: a signal built from the normalised bank description plus counterparty IBAN, the chosen `grootboekcode`, `btw_code`, `soort`, a count and one `sample_omschrijving`; the user can list and delete these under Instellingen → Geleerde boekingen). No mutations, relations, balances or bank statements.
- **Redis stores** the keys listed under "Redis keys". The AI caches contain bookkeeping data: supplier names, invoice numbers, amounts, bank-line descriptions and the chosen ledger codes, for 1 hour (classification), 7 days (file analysis, uploads and mail-folder documents alike) or 30 days (full mail-folder analysis). The duplicate marker (90 days) holds `mutNr`, `paymentMutNr` and `submittedAt` only. `doc:ignored` holds file ids only and has no TTL. Everything is scoped per user id and deleted by "Verbinding verbreken" (sessions and rate-limit counters excepted). Only the AI caches are disposable; the ignore list is user state.
- **R2 (when configured) stores avatars only**, under `avatars/{uuid}.{ext}` (served from `R2_PUBLIC_URL`). No document is written to R2: the analyser keeps no copy of an upload (the browser inlines it as `pdfBase64` when booking) and mail-folder documents stay in e-boekhouden. `prepareUploadSource` can still read a legacy `uploadKey` from a client that analysed with an older version; `uploads/` objects written by older versions are not deleted by the application (bucket lifecycle rule).
- **Claude receives** data only with the user's own key, over HTTPS to api.anthropic.com. A document goes to Claude only when the user starts its analysis. Unprocessed bank lines (date, amount, description, counterparty account) are sent automatically when the inbox loads and the 1-hour classification cache is empty or forced (see Key Technical Decisions). The key is decrypted per call and never logged.
- **Logs** follow the policy in `handler/logging.go`: ids, statuses, byte lengths, stop reasons and error classes (`errClass`), never response bodies, Claude output, names, amounts, descriptions, file names or e-mail addresses. `eboekhouden.APIError` never embeds a response body.
- **Cookies are HttpOnly + SameSite=Lax**, and Secure when `COOKIE_SECURE=true` (production). Session ids are random UUIDs.
- **All traffic to e-boekhouden.nl is HTTPS** (`secure.e-boekhouden.nl`, `secure20.e-boekhouden.nl`) with no HTTP fallback.
- **Rate limiting** per IP on the auth endpoints (see Key Technical Decisions for the numbers), disabled in local dev.
- **File size limits:** 10 MB for uploads (`MaxMultipartMemory`, `maxInvoiceFileSize`) and for mail-folder downloads (`DownloadArchiveFile` cap); the landing and frontend nginx accept 16 MB on `/api/` so an inlined 10 MB upload fits (in Kubernetes `/api` bypasses both and goes through Traefik).
- **Analytics:** Plausible (self-hosted, cookieless) is loaded on the landing pages and in the app; `track()` only forwards enum-like tokens, counts and flags (`sanitizeTrackProps`) and `errorClass()` replaces error messages. `analytics.test.ts` scans every call site. Never add bookkeeping data to an event.
