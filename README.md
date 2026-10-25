# Speedy e-Boekhouden

Speedy e-Boekhouden is a web app on top of your [e-boekhouden.nl](https://www.e-boekhouden.nl) administration. It books unprocessed bank lines with suggestions from Claude, reads invoices and receipts, handles the documents mailed to your Mail & herken folder, and logs hours for many days at once.

[![CI](https://github.com/joooostb/speedy-eboekhouden/actions/workflows/ci.yml/badge.svg)](https://github.com/joooostb/speedy-eboekhouden/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](backend/go.mod)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](frontend/package.json)
[![TypeScript](https://img.shields.io/badge/TypeScript-7-3178C6?logo=typescript&logoColor=white)](frontend/package.json)

[Live app](https://speedy-eboekhouden.nl/app/) | [Security (Dutch)](https://speedy-eboekhouden.nl/beveiliging) | [Contributing](CONTRIBUTING.md)

## Features

- The home screen is a bookkeeping inbox with two tabs: "Afschriften" lists the unprocessed bank lines of your administration, "Documenten" the files in your Mail & herken folder. Speedy suggests a ledger account and VAT code for each bank line, using Claude, and you approve lines one at a time or in a batch. When both lists are empty, the inbox says "Inbox zero!".
- When you book bank lines with the same description and counterparty to the same ledger account and VAT code twice, Speedy suggests that booking from then on without asking Claude. You can review and delete these under Instellingen → Geleerde boekingen.
- A bank line that pays an outstanding purchase or sales invoice in e-boekhouden is marked "Betaalt open factuur"; "Koppelen" books it as the payment of that invoice.
- Upload an invoice or receipt as PDF, JPG or PNG and Claude reads the supplier, invoice number, dates, amounts and VAT, and proposes a ledger account. Speedy ranks the bank lines that could be the payment, books the invoice (with its payment when you link a line) and can store the file in your e-boekhouden archive.
- Files mailed to your administration's Mail & herken address land in a folder of the e-boekhouden archive. Speedy lists them and analyses them when you ask, one at a time or with "Alles analyseren". It matches each invoice or receipt to its bank line and books it in the same review dialog; an invoice that has not been paid yet is booked as an open invoice. After booking, Speedy renames the file to a fixed pattern such as `2026-10-12_Voorbeeld-Leverancier_INV-0042.pdf` and moves it out of the mail folder. You can also ignore files in Speedy or delete them from e-boekhouden. Credit notes are recognised but not booked yet; book those in e-boekhouden itself.
- For hours, pick employees, a project and an activity, select days on a calendar ("Alle werkdagen" skips Dutch public holidays) and submit all entries at once. Speedy warns before it books hours that already exist.
- You sign in to Speedy with a passkey (Face ID, Touch ID or a security key); Speedy has no passwords. You then connect your e-boekhouden account. Speedy keeps the encrypted session token, never your e-boekhouden password or MFA code, and "Verbinding verbreken" removes the token and your cached data.
- Claude runs on your own Anthropic API key, stored encrypted. Without a key the AI features are off: you can still book bank lines by hand and view or ignore documents.

## How it fits together

With docker compose, one nginx container is the entry point:

```text
Browser → landing nginx (:3000)
            ├── /            landing, security and disclaimer pages
            ├── /app/      → frontend nginx (React SPA)
            └── /api/      → backend (Go, :8080)
                               ├── PostgreSQL
                               ├── Redis
                               ├── e-boekhouden.nl (HTTPS)
                               ├── api.anthropic.com (HTTPS, the user's key)
                               └── Cloudflare R2, SMTP (optional)
```

In Kubernetes the Traefik ingress sends `/api` and `/app` straight to the backend and frontend services.

## Quick start

You need Docker with Compose v2 and an [e-boekhouden.nl](https://www.e-boekhouden.nl) account. An [Anthropic API key](https://console.anthropic.com/) is optional and only needed for the AI features.

```bash
git clone https://github.com/joooostb/speedy-eboekhouden.git
cd speedy-eboekhouden
cp .env.example .env
docker compose up --build
```

Open <http://localhost:3000/app/>, create an account with a passkey and connect e-boekhouden. Add the Anthropic API key under Instellingen.

`.env.example` is set up for local HTTP: `COOKIE_SECURE=false` (which also turns off rate limiting) and an all-zero `ENCRYPTION_KEY`. Generate a real key with `openssl rand -hex 32` for anything beyond a local test.

You don't need Node.js on your machine; the frontend is built inside its container.

## Project structure

```text
.
├── backend/                  Go API server
│   ├── cmd/server/           Entry point and routes
│   └── internal/
│       ├── auth/             WebAuthn passkey service
│       ├── claude/           Claude API: document reading, bank-line classification
│       ├── config/           Environment-based config
│       ├── crypto/           AES-256-GCM encryption
│       ├── database/         PostgreSQL: users, teams, passkeys, settings, learned bookings
│       ├── eboekhouden/      HTTP client for e-boekhouden.nl
│       ├── handler/          HTTP handlers
│       ├── mail/             SMTP mailer (welcome, recovery)
│       ├── middleware/       CORS, security headers, rate limiting
│       ├── session/          Redis-backed session store
│       └── storage/          Cloudflare R2 client (avatars)
│
├── frontend/                 React SPA
│   └── src/
│       ├── api/              API client and TypeScript types
│       ├── components/
│       │   ├── auth/         Passkey login, registration, recovery
│       │   ├── inbox/        Bookkeeping inbox: bank lines, documents, review dialog
│       │   ├── settings/     API key, connection, passkeys, learned bookings
│       │   └── shared/       Pickers (ledger, relation, VAT, archive folder)
│       ├── context/          Auth state
│       ├── hooks/            Data fetching and the documents tab
│       └── utils/            Helpers
│
├── landing/                  Static landing, security and disclaimer pages
├── deploy/                   Kubernetes manifests (Kustomize): base/, overlays/production/
├── test/smoke.sh             Smoke tests against a running stack
├── .github/workflows/        CI and release
├── .env.example              Environment template for docker compose
└── docker-compose.yml        Full stack: backend, frontend, landing, postgres, redis
```

## Tech stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.26, Gin, pgx, go-redis, go-webauthn, anthropic-sdk-go |
| Frontend | React 19, TypeScript 7, MUI 7, Vite 8, react-router-dom, @simplewebauthn/browser |
| Database | PostgreSQL 18 |
| Cache | Redis 8 (sessions, rate limits, AI caches); the Kubernetes manifests use Redis 7 |
| Landing | Static HTML and CSS with one small script, no framework |
| Containers | Docker, nginx 1.31 |
| Orchestration | Kubernetes, Kustomize, Traefik |
| AI | Claude Sonnet 4.5 (documents), Claude Haiku 4.5 (bank-line classification) |
| Auth | WebAuthn passkeys |
| Encryption | AES-256-GCM (Anthropic key, e-boekhouden token, SOAP and REST credentials) |
| Storage (optional) | Cloudflare R2 for avatars |
| TLS | Let's Encrypt via cert-manager |

## API

All routes live under `/api/v1`. "Session" means a Speedy passkey session; "EB" means a connected e-boekhouden account on top of that. `CLAUDE.md` has the request and response shapes of the document endpoints.

### Authentication and connection

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/auth/register/begin`, `/auth/register/finish` | Public | Passkey registration |
| `POST` | `/auth/login/begin`, `/auth/login/finish` | Public | Passkey login; restores a stored e-boekhouden token |
| `POST` | `/auth/recover`, `/auth/recover/begin`, `/auth/recover/finish` | Public | Passkey recovery by e-mail (needs SMTP) |
| `GET` | `/auth/me` | Session | Current user, team, e-boekhouden status |
| `POST` | `/auth/logout` | Session | End the Speedy session |
| `POST` | `/eboekhouden/login`, `/eboekhouden/mfa` | Session | Connect an e-boekhouden account (MFA optional) |
| `GET` | `/eboekhouden/status`, `/eboekhouden/keepalive` | Session | Connection state; keepalive pings e-boekhouden |
| `POST` | `/eboekhouden/disconnect` | Session | Log out at e-boekhouden, clear the token, purge the user's caches; returns `{status, ebLoggedOut, purgedKeys}` |

### Settings

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/settings` | Session | Settings flags and preferences |
| `GET` | `/settings/api-key/status` | Session | Whether the stored Anthropic key works |
| `PUT`/`DELETE` | `/settings/api-key`, `/settings/soap-credentials`, `/settings/rest-token` | Session | Encrypted secrets |
| `PUT` | `/settings/entity-type` | Session | B.V., ZZP, eenmanszaak or other; used by the classifier |
| `GET`/`PATCH`/`DELETE` | `/settings/passkeys[/:id]` | Session | Manage passkeys |
| `GET`/`DELETE` | `/settings/learned`, `/settings/learned/item` | Session | List learned bookings, delete one or all |
| `POST`/`DELETE` | `/avatar` | Session | Profile photo (needs R2) |

### Bookkeeping inbox

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/inbox/summary` | Session | Counts: unprocessed bank lines, open documents, last classification summary, settings flags |
| `POST` | `/inbox/classify` | Session + EB | Classify the unprocessed bank lines (learned bookings and open-invoice payments first, the rest by Claude; cached 1 h) |
| `POST` | `/inbox/process-batch` | Session + EB | Book up to 100 lines, including payments of open invoices |
| `POST` | `/invoices/analyze` | Session + EB | Upload a PDF, JPG or PNG; Claude extracts the data and the matching bank lines are ranked |
| `POST` | `/invoices/submit-full`, `/invoices/submit-receipt` | Session + EB | Book an invoice or receipt, archive and link the file |
| `GET` | `/documents` | Session + EB | The Mail & herken folder with status, title and match per file |
| `POST` | `/documents/setup`, `/documents/:id/analyze`, `/documents/archive` | Session + EB | Set the mail folder, analyse a document, rename and move files |
| `GET` | `/documents/:id/content` | Session + EB | Inline preview of a document |
| `POST`/`DELETE` | `/documents/:id/ignore`, `/documents/:id` | Session + EB | Ignore (in Speedy only) or delete (in e-boekhouden) |
| `GET` | `/openposts?soort=Crediteur\|Debiteur` | Session + EB | Open invoices from the open-post grid |

### e-Boekhouden data

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/employees`, `/projects`, `/activities`, `/hours/overview` | Session + EB | Hour registration data |
| `POST` | `/hours` | Session + EB | Submit hour entries in bulk |
| `GET` | `/ledger-accounts`, `/relations?q=`, `/vat-codes` | Session + EB | Reference data |
| `POST` | `/relations`, `/mutations` | Session + EB | Create a relation or a mutation |
| `GET`/`POST` | `/archive/folders`, `/archive/files/:folderId`, `/archive/upload`, `/archive/link` | Session + EB | Digital archive |
| `GET` | `/kvk/search`, `/kvk/address/:vestigingsnummer` | Session + EB | KvK lookup |
| `GET`/`POST` | `/bankstatements…`, `/classify`, `/inbox/:id/match-invoice`, `/invoices/submit`, `/soap/…`, `/rest/…` | Session (+ EB or own SOAP/REST credentials) | Older routes. The inbox falls back to `/bankstatements` when classification fails; the others are only called by pages that are no longer routed, or not at all. |

## Configuration

The backend reads its configuration from environment variables. `.env.example` lists them with local defaults.

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Backend listen port |
| `FRONTEND_ORIGIN` | `http://localhost:3000` | Allowed CORS origin |
| `COOKIE_DOMAIN` | *(empty)* | Session cookie domain |
| `COOKIE_SECURE` | `true` | `false` for local HTTP; also turns off rate limiting |
| `POSTGRES_DSN` | `postgres://speedy:dev@localhost:5432/speedy?sslmode=disable` | PostgreSQL DSN |
| `REDIS_URL` | `redis://localhost:6379` | Redis URL |
| `ENCRYPTION_KEY` | *(required)* | 64 hex characters for AES-256-GCM; the server does not start without it |
| `WEBAUTHN_ORIGIN` | *(FRONTEND_ORIGIN)* | WebAuthn relying party origin |
| `WEBAUTHN_RP_ID` | `localhost` | WebAuthn relying party ID |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` | *(empty)*, `587`, *(empty)*, *(empty)*, `noreply@speedy-eboekhouden.nl` | Welcome and recovery e-mails. Without `SMTP_HOST` no mail is sent and passkey recovery is unavailable. |
| `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET`, `R2_JURISDICTION`, `R2_PUBLIC_URL` | *(empty)*, bucket `speedy-eboekhouden` | Cloudflare R2 for avatars; off without account id and key id. When R2 is on, an upload booked without an archive folder is filed under `Facturen/<jaar>/<maand>`; without R2 such an upload is not archived. |

## Security

The Dutch security page at [speedy-eboekhouden.nl/beveiliging](https://speedy-eboekhouden.nl/beveiliging) covers the same ground for users. The Security Model in `CLAUDE.md` names the code behind each point.

- Speedy accounts use passkeys only, so there are no passwords to steal. PostgreSQL holds the public keys and credential flags.
- Your e-boekhouden password and MFA code are forwarded to e-boekhouden.nl and discarded within the request. The session token that comes back is stored, AES-256-GCM encrypted, in your Redis session (30-minute sliding TTL) and in PostgreSQL, so the connection survives a new Speedy login. "Verbinding verbreken" asks e-boekhouden to log the token out, deletes it from the current session and from PostgreSQL, and purges your cached data. The response says whether e-boekhouden confirmed the logout.
- PostgreSQL stores accounts (e-mail, name), teams, passkey public keys, encrypted secrets (Anthropic key, e-boekhouden token, optional SOAP and REST credentials), recovery-token hashes and learned bookings. A learned booking is a normalised bank description plus counterparty IBAN, the chosen ledger account and VAT code, and one sample description; you can list and delete them under Instellingen. Speedy does not store mutations, relations, balances or bank statements.
- Redis stores sessions, rate-limit counters and per-user caches that do contain bookkeeping data: supplier, invoice number, dates, amounts, bank-line descriptions and the suggested codes. The bank-line classification is kept for 1 hour, the analysis of a file (upload or mail-folder document) for 7 days, and the full analysis of a mail-folder document for 30 days. The duplicate marker (90 days) holds only mutation numbers and a timestamp, and the ignore list only file ids. "Verbinding verbreken" deletes the caches, the duplicate markers and the ignore list of your account.
- Object storage (R2, optional) holds avatars only. An uploaded invoice is analysed in memory and the browser sends it again when you book it; mail-folder documents stay in your e-boekhouden archive.
- Claude receives data only with your own API key, over HTTPS to api.anthropic.com. A document goes to Claude only when you start its analysis. Unprocessed bank lines (date, amount, description, counterparty account) are sent for classification automatically when the inbox loads; the result is cached for an hour, and "Vernieuwen" or a booking from the review dialog starts a new run. Lines that a learned booking or an open invoice (matched by name) already explain are not sent. The key is decrypted per call and never logged.
- Logs carry ids, statuses, byte lengths and error classes. They never contain e-boekhouden response bodies, Claude output, names, amounts, descriptions or file names.
- Analytics run on self-hosted, cookieless Plausible; events carry counts, flags and fixed tokens only.
- All traffic to e-boekhouden.nl uses HTTPS without HTTP fallback. Cookies are HttpOnly and SameSite=Lax, and Secure in production; session ids are random. The auth endpoints are rate limited per IP in Redis. Uploads and mail-folder downloads are capped at 10 MB.
- The code is open source, so you can check all of this yourself.

Report vulnerabilities to info@speedy-eboekhouden.nl rather than in a public issue.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Built with AI

A large part of this codebase was written with [Claude](https://claude.ai) by Anthropic. All generated code is reviewed and tested before it goes to production.

## Disclaimer

Speedy e-Boekhouden is an independent open-source project. It is not affiliated with, endorsed by or connected to e-Boekhouden.nl or e-Boekhouden B.V. Use it at your own risk. The full disclaimer (in Dutch) is at [speedy-eboekhouden.nl/disclaimer](https://speedy-eboekhouden.nl/disclaimer).

## License

[MIT](LICENSE)
