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
| **Invoice OCR** | Upload PDF invoices, Claude AI extracts supplier, amounts, VAT, ledger account |
| **Transaction classification** | AI suggests grootboekrekening and BTW code based on Belastingdienst rules |
| **Dashboard** | Overview of unprocessed bank statements, quick navigation to all features |
| **Passkey authentication** | No passwords — login with Face ID, Touch ID, or security key |
| **Team support** | Multi-tenant: create a team, manage members |
| **e-Boekhouden proxy** | Full access to ledger accounts, relations, VAT codes, digital archive |
| **Zero credential storage** | e-Boekhouden passwords are never stored — entered fresh each session |

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
│       │   ├── auth/         Passkey login/register
│       │   ├── onboarding/   First-time setup wizard
│       │   ├── bankstatements/ Bank statement processing
│       │   ├── invoices/     Invoice upload + AI extraction
│       │   ├── settings/     API key + account settings
│       │   └── shared/       Reusable pickers (ledger, relations, VAT)
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
| Database | PostgreSQL 17 |
| Cache | Redis 7 |
| Landing | Static HTML + CSS (no JS framework) |
| Containers | Docker, nginx 1.27 |
| Orchestration | Kubernetes, Kustomize, Traefik |
| AI | Claude Sonnet 4.5 (invoices), Claude Haiku 4.5 (classification) |
| Auth | WebAuthn / Passkeys |
| Encryption | AES-256-GCM (API keys, session tokens) |
| TLS | Let's Encrypt (via Traefik) |

## API

### Authentication

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/v1/auth/register/begin` | — | Start passkey registration |
| `POST` | `/api/v1/auth/register/finish` | — | Complete passkey registration |
| `POST` | `/api/v1/auth/login/begin` | — | Start passkey login |
| `POST` | `/api/v1/auth/login/finish` | — | Complete passkey login |
| `GET` | `/api/v1/auth/me` | Session | Current user + team + e-boekhouden status |
| `POST` | `/api/v1/auth/logout` | Session | Destroy session |

### e-Boekhouden Connection

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/v1/eboekhouden/login` | Session | Connect e-boekhouden account |
| `POST` | `/api/v1/eboekhouden/mfa` | Session | Complete e-boekhouden MFA |
| `GET` | `/api/v1/eboekhouden/status` | Session | Check e-boekhouden connection |

### Bookkeeping Features (require e-boekhouden connection)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/v1/bankstatements` | Session + EB | List unprocessed bank statement lines |
| `GET` | `/api/v1/bankstatements/count` | Session + EB | Count unprocessed lines |
| `POST` | `/api/v1/bankstatements/:id/process` | Session + EB | Process a bank statement line |
| `POST` | `/api/v1/mutations` | Session + EB | Create a mutation |
| `GET` | `/api/v1/ledger-accounts` | Session + EB | Active ledger accounts |
| `GET` | `/api/v1/relations?q=` | Session + EB | Search relations |
| `GET` | `/api/v1/vat-codes` | Session + EB | VAT codes |
| `POST` | `/api/v1/invoices/analyze` | Session + EB | Upload PDF, Claude extracts data |
| `POST` | `/api/v1/invoices/submit` | Session + EB | Book an invoice |
| `POST` | `/api/v1/classify` | Session | AI transaction classification |
| `GET` | `/api/v1/employees` | Session + EB | List employees |
| `POST` | `/api/v1/hours` | Session + EB | Bulk submit hour entries |

### Settings

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/v1/settings` | Session | Get user settings |
| `PUT` | `/api/v1/settings/api-key` | Session | Store encrypted Anthropic API key |
| `DELETE` | `/api/v1/settings/api-key` | Session | Remove API key |

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

- **Passkey-only auth** — no passwords in the system. Phishing-resistant, device-bound.
- **e-Boekhouden credentials never stored** — forwarded and discarded within the request handler.
- **Encrypted storage** — API keys encrypted with AES-256-GCM. Session tokens encrypted in Redis.
- **HttpOnly + Secure + SameSite=Lax cookies** — inaccessible to JavaScript, HTTPS-only.
- **Rate limiting** — Redis-based limits on all auth endpoints.
- **HTTPS everywhere** — TLS on both sides (browser ↔ Speedy, Speedy ↔ e-boekhouden).
- **No bookkeeping data stored** — all data fetched on-the-fly from e-boekhouden.nl.
- **Open source** — verify everything yourself.

Read the full security deep-dive at [speedy-eboekhouden.nl/beveiliging](https://speedy-eboekhouden.nl/beveiliging).

Found a vulnerability? Email **info@speedy-eboekhouden.nl** — please don't open a public issue.

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Built With AI

A substantial portion of this codebase was written with the help of [Claude](https://claude.ai) by Anthropic. All generated code has been reviewed, tested, and approved before being shipped to production.

## Disclaimer

Speedy e-Boekhouden is an independent, open-source project. It is **not affiliated with, endorsed by, or connected to** e-Boekhouden.nl or e-Boekhouden B.V. in any way. Use at your own risk. See the full [disclaimer](https://speedy-eboekhouden.nl/disclaimer).

## License

[MIT](LICENSE)
