# Speedy e-Boekhouden

**Supercharge your [e-boekhouden.nl](https://www.e-boekhouden.nl) bookkeeping with AI**

Bulk hour logging, AI-powered bank statement processing, invoice OCR with automatic ledger classification — all in one platform.

[![CI](https://github.com/joooostb/speedy-eboekhouden/actions/workflows/ci.yml/badge.svg)](https://github.com/joooostb/speedy-eboekhouden/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](backend/go.mod)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](frontend/package.json)
[![TypeScript](https://img.shields.io/badge/TypeScript-6-3178C6?logo=typescript&logoColor=white)](frontend/package.json)

[Live App](https://speedy-eboekhouden.nl/app/) | [Security](https://speedy-eboekhouden.nl/beveiliging) | [Contributing](CONTRIBUTING.md)

---

## Features

| Feature | Description |
|---------|-------------|
| **Bulk hour entry** | Select employees, project, activity, pick dates on a calendar, submit all at once |
| **Bank statement processing** | View unprocessed lines, get AI-powered booking suggestions, process with one click |
| **Invoice OCR** | Upload PDF, JPG or PNG invoices, Claude AI extracts supplier, amounts, VAT, ledger account |
| **Document inbox (Mail & herken)** | Documents mailed to your e-boekhouden archive show up automatically; analyse them with Claude, book them against the matching bank line or as an open invoice, and Speedy renames and files them in your archive |
| **Open-invoice matching** | Bank lines that pay an outstanding invoice are recognised and settled with one click |
| **Transaction classification** | AI suggests grootboekrekening and BTW code based on Belastingdienst rules; confirmed bookings are learned per user |
| **Bookkeeping inbox** | One home screen with the unprocessed bank lines and the mail-folder documents, each with one suggestion you confirm with a click; "Inbox zero" when both are empty |
| **Passkey authentication** | No passwords — login with Face ID, Touch ID, or security key |
| **Team support** | Multi-tenant: create a team, manage members |
| **e-Boekhouden proxy** | Full access to ledger accounts, relations, VAT codes, digital archive |
| **No password storage** | e-Boekhouden passwords and MFA codes are never stored; the session token is kept encrypted and "Verbinding verbreken" logs it out and wipes the cached data |

## Architecture

```
┌─────────────┐         ┌──────────────┐         ┌────────────────────┐
│   Browser   │──HTTPS──▶  Go Backend  │──HTTPS──▶  e-boekhouden.nl  │
│  React/MUI  │         │    (Gin)     │         │   (API + Auth)     │
│  Passkeys   │         │             ╔╩═══════╗ │                    │
│             │◀─────────│             ║ Redis  ║ │                    │
└─────────────┘         │             ╚════════╝ └────────────────────┘
                        │             ╔════════╗
                        │             ║Postgres║  ┌────────────────────┐
                        │             ╚╦═══════╝  │   Claude API       │
                        │              │──HTTPS──▶│  (Anthropic)       │
                        └──────────────┘          └────────────────────┘
```

## Quick Start

### Prerequisites

- Docker + Docker Compose
- An [e-boekhouden.nl](https://www.e-boekhouden.nl) account
- (Optional) An [Anthropic API key](https://console.anthropic.com/) for AI features

### Run

```bash
git clone https://github.com/joooostb/speedy-eboekhouden.git
cd speedy-eboekhouden
docker-compose up --build
```

Open **http://localhost:3000** — register with a passkey, connect your e-boekhouden account, and you're ready.

> **Note:** No local Node.js required. All frontend builds happen inside containers.

## Project Structure

```
.
├── backend/                  Go API server
│   ├── cmd/server/           Entry point + route setup
│   └── internal/
│       ├── auth/             WebAuthn passkey service
│       ├── claude/           Claude API integration (invoice OCR, classification)
│       ├── crypto/           AES-256-GCM encryption
│       ├── database/         PostgreSQL (users, teams, passkeys, settings)
│       ├── eboekhouden/      HTTP client for e-boekhouden.nl API
│       ├── handler/          HTTP handlers
│       ├── middleware/       CORS, security headers, rate limiting
│       ├── session/          Redis-backed session store
│       └── config/           Environment-based config
│
├── frontend/                 React SPA
│   └── src/
│       ├── api/              API client + TypeScript types
│       ├── components/
│       │   ├── auth/         Passkey login/register/recovery
│       │   ├── onboarding/   First-time setup wizard
│       │   ├── inbox/        Bookkeeping inbox: bank lines, documents, review dialog
│       │   ├── settings/     API key, connection, passkeys, learned bookings
│       │   └── shared/       Reusable pickers (ledger, relations, VAT, archive folder)
│       ├── hooks/            Data fetching hooks
│       └── context/          Auth state management
│
├── landing/                  Static landing page + security page
│
├── deploy/                   Kubernetes manifests (Kustomize)
│   ├── base/
│   └── overlays/
│       ├── local/
│       └── production/
│
└── docker-compose.yml        Full stack: backend, frontend, landing, postgres, redis
```

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.26, Gin, pgx, go-redis, go-webauthn, anthropic-sdk-go |
| Frontend | React 19, TypeScript 6, MUI 7, Vite 8, react-router-dom, @simplewebauthn/browser |
| Database | PostgreSQL 18 |
| Cache | Redis 8 (sessions, rate limits, AI caches) |
| Landing | Static HTML + CSS (no JS framework) |
| Containers | Docker, nginx 1.31 |
| Orchestration | Kubernetes, Kustomize, Traefik |
| AI | Claude Sonnet 4.5 (invoices), Claude Haiku 4.5 (classification) |
| Auth | WebAuthn / Passkeys |
| Encryption | AES-256-GCM (API keys, e-boekhouden token, SOAP/REST credentials) |
| Storage (optional) | Cloudflare R2 for avatars |
| TLS | Let's Encrypt (via Traefik) |

## API

All routes live under `/api/v1`. "Session" means a Speedy passkey session; "EB" means a connected e-boekhouden account on top of that. The full request and response shapes of the document inbox are documented in `CLAUDE.md`.

### Authentication and connection

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/auth/register/begin`, `/auth/register/finish` | — | Passkey registration |
| `POST` | `/auth/login/begin`, `/auth/login/finish` | — | Passkey login |
| `POST` | `/auth/recover`, `/auth/recover/begin`, `/auth/recover/finish` | — | Passkey recovery by e-mail |
| `GET` | `/auth/me` | Session | Current user, team, e-boekhouden status |
| `POST` | `/auth/logout` | Session | Destroy the Speedy session |
| `POST` | `/eboekhouden/login`, `/eboekhouden/mfa` | Session | Connect an e-boekhouden account (MFA optional) |
| `GET` | `/eboekhouden/status`, `/eboekhouden/keepalive` | Session | Connection state; keepalive pings e-boekhouden |
| `POST` | `/eboekhouden/disconnect` | Session | Log out at e-boekhouden, clear the token, purge the user's caches; returns `{ebLoggedOut, purgedKeys}` |

### Settings

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/settings` | Session | Settings flags and preferences |
| `PUT`/`DELETE` | `/settings/api-key`, `/settings/soap-credentials`, `/settings/rest-token` | Session | Encrypted secrets |
| `PUT` | `/settings/entity-type` | Session | B.V. / ZZP / eenmanszaak, steers the classifier |
| `GET`/`PATCH`/`DELETE` | `/settings/passkeys[/:id]` | Session | Manage passkeys |
| `GET`/`DELETE` | `/settings/learned`, `/settings/learned/item` | Session | Learned bookings |

### Bookkeeping inbox

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/inbox/summary` | Session | Unprocessed bank lines, open documents, last classification summary |
| `POST` | `/inbox/classify` | Session + EB | Classify the unprocessed bank lines (cached 1 h, learned bookings first) |
| `POST` | `/inbox/process-batch` | Session + EB | Book up to 100 lines, incl. payments of open invoices |
| `POST` | `/invoices/analyze` | Session + EB | Upload a PDF/JPG/PNG; Claude extracts the data and the matching bank lines are ranked |
| `POST` | `/invoices/submit-full`, `/invoices/submit-receipt` | Session + EB | Book an invoice or receipt, archive and link the file |
| `GET` | `/documents` | Session + EB | The Mail & herken folder with status, title and match per file |
| `POST` | `/documents/setup`, `/documents/:id/analyze`, `/documents/archive` | Session + EB | Configure the mail folder, analyse a document, rename and move files |
| `GET` | `/documents/:id/content` | Session + EB | Inline preview of a document |
| `POST`/`DELETE` | `/documents/:id/ignore`, `/documents/:id` | Session + EB | Ignore (Speedy only) or delete (in e-boekhouden) |
| `GET` | `/openposts?soort=Crediteur\|Debiteur` | Session + EB | Open invoices from the open-post grid |

### e-Boekhouden data

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/employees`, `/projects`, `/activities`, `/hours/overview` | Session + EB | Hour registration reference data |
| `POST` | `/hours` | Session + EB | Bulk submit hour entries |
| `GET` | `/ledger-accounts`, `/relations?q=`, `/vat-codes` | Session + EB | Reference data |
| `POST` | `/relations`, `/mutations` | Session + EB | Create a relation or a mutation |
| `GET`/`POST` | `/archive/folders`, `/archive/files/:folderId`, `/archive/upload`, `/archive/link` | Session + EB | Digital archive |
| `GET` | `/kvk/search`, `/kvk/address/:vestigingsnummer` | Session + EB | KvK lookup |
| `GET`/`POST` | `/bankstatements…`, `/soap/…`, `/rest/…` | Session (+ EB / own credentials) | Older bank-statement, SOAP and REST routes, kept for compatibility |

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Backend listen port |
| `FRONTEND_ORIGIN` | `http://localhost:3000` | Allowed CORS origin |
| `COOKIE_DOMAIN` | *(empty)* | Session cookie domain |
| `COOKIE_SECURE` | `true` | `false` for local HTTP dev |
| `POSTGRES_DSN` | `postgres://speedy:dev@localhost:5432/speedy?sslmode=disable` | PostgreSQL DSN |
| `REDIS_URL` | `redis://localhost:6379` | Redis URL |
| `ENCRYPTION_KEY` | *(required)* | 64 hex chars for AES-256-GCM |
| `WEBAUTHN_ORIGIN` | *(FRONTEND_ORIGIN)* | WebAuthn RP origin |
| `WEBAUTHN_RP_ID` | `localhost` | WebAuthn RP ID |

## Security

The short, honest version. The Dutch write-up at [speedy-eboekhouden.nl/beveiliging](https://speedy-eboekhouden.nl/beveiliging), the Security Model in `CLAUDE.md` and this list are kept in step with the code.

- **Passkey-only auth.** No passwords in the system. PostgreSQL holds public keys and credential flags, nothing a phisher can use.
- **e-Boekhouden passwords and MFA codes are never stored.** They are forwarded to e-boekhouden.nl and discarded within the request. The resulting session token *is* stored, AES-256-GCM encrypted, in the Redis session (30-minute sliding TTL) and in PostgreSQL so the connection survives a Speedy login. **Verbinding verbreken** logs the token out at e-boekhouden, deletes it from both and purges the user's cached data; the response says whether e-boekhouden confirmed the logout.
- **PostgreSQL stores** accounts (e-mail, name), teams, passkey public keys, encrypted secrets (Anthropic key, e-boekhouden token, optional SOAP/REST credentials), recovery-token hashes and learned bookings: per user a normalised bank description plus counterparty IBAN, the chosen ledger and VAT code and one sample description, listed and deletable under Instellingen. No mutations, relations, balances or bank statements.
- **Redis stores** sessions, rate-limit counters and per-user AI caches that **do** contain bookkeeping data: supplier, invoice number, dates, amounts, bank-line descriptions and the suggested codes, for 1 hour (classification), 7 days (upload analysis) or 30 days (mail-folder analysis). The duplicate marker (90 days) holds mutation numbers and a timestamp only. Everything of a user is deleted on disconnect; nothing in Redis is needed for correctness.
- **Object storage (R2, optional) holds avatars only.** Uploaded invoices are analysed in memory and sent inline by the browser when booked; mail-folder documents stay in your e-boekhouden archive. No document is written to object storage.
- **Claude** sees a document or bank line only when you start an analysis or classification, with your own API key, over HTTPS to api.anthropic.com. The key is decrypted per call and never logged.
- **Logs** carry ids, statuses, byte lengths and error classes; never response bodies, Claude output, names, amounts, descriptions or file names.
- **Analytics:** self-hosted, cookieless Plausible; events carry counts, flags and enum values only.
- **Transport and sessions:** HTTPS to e-boekhouden.nl without HTTP fallback; HttpOnly + SameSite=Lax cookies (Secure in production); random session ids; Redis-based rate limiting on the auth endpoints; 10 MB file limit for uploads and mail-folder downloads.
- **Open source**: verify everything yourself.

Found a vulnerability? Email **info@speedy-eboekhouden.nl** — please don't open a public issue.

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Built With AI

A substantial portion of this codebase was written with the help of [Claude](https://claude.ai) by Anthropic. All generated code has been reviewed, tested, and approved before being shipped to production.

## Disclaimer

Speedy e-Boekhouden is an independent, open-source project. It is **not affiliated with, endorsed by, or connected to** e-Boekhouden.nl or e-Boekhouden B.V. in any way. Use at your own risk. See the full [disclaimer](https://speedy-eboekhouden.nl/disclaimer).

## License

[MIT](LICENSE)
