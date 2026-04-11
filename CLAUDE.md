# CLAUDE.md

Instructions for Claude Code instances working on this repository.

## Project Overview

Speedy e-Boekhouden is a multi-tenant SaaS platform that supercharges [e-boekhouden.nl](https://www.e-boekhouden.nl). Features include bulk hour logging, AI-powered bank statement processing, invoice OCR with automatic classification, and a bookkeeping dashboard. Users authenticate with passkeys (WebAuthn) and connect their e-boekhouden account per session.

## Architecture

Five services behind a single nginx entry point:

- **`landing/`** — Static HTML/CSS landing page, security page, and disclaimer. Served by nginx. Acts as the public entry point and reverse proxy for `/app/` and `/api/`.
- **`frontend/`** — React 19 + TypeScript 6 + MUI 7 SPA, built with Vite 8. Served at `/app/` with `base: '/app/'`. Uses react-router-dom for client-side routing.
- **`backend/`** — Go 1.26 + Gin API server. Handles auth, proxies calls to e-boekhouden.nl, integrates Claude API.
- **`postgres`** — PostgreSQL 17. Stores users, teams, passkey credentials, encrypted API keys.
- **`redis`** — Redis 7. Session store with sliding TTL. e-Boekhouden auth tokens stored encrypted.

```
Browser → landing nginx (:3000)
              ├── /           → static landing page
              ├── /beveiliging → security page
              ├── /disclaimer  → disclaimer page
              ├── /app/       → proxy to frontend nginx (:80)
              └── /api/       → proxy to backend (:8080)
                                    ├── PostgreSQL (:5432)
                                    └── Redis (:6379)
```

## Key Technical Decisions

- **Passkey-only authentication.** No passwords. WebAuthn via go-webauthn library. Challenges stored in sync.Map (single-instance).
- **Two-level auth.** Users first authenticate with their Speedy passkey, then connect e-boekhouden per session. e-boekhouden credentials are never persisted.
- **PostgreSQL for accounts.** Users, teams, passkey credentials, and encrypted API keys are stored in PostgreSQL. Migrations run automatically on startup via embedded SQL.
- **Redis for sessions.** Sessions are stored in Redis with 30-minute sliding TTL. e-Boekhouden auth tokens are AES-256-GCM encrypted before storage.
- **API key encryption.** User-provided Anthropic API keys are encrypted with AES-256-GCM using a server-side ENCRYPTION_KEY before storage in PostgreSQL.
- **Claude API integration.** Invoice reading uses Claude Sonnet 4.5 (vision/PDF). Transaction classification uses Claude Haiku 4.5 (fast/cheap). Users provide their own API key.
- **Raw JSON passthrough.** The backend does not deserialize e-boekhouden API responses. It passes `json.RawMessage` directly to the frontend with `c.Data()`. Exception: the import grid endpoint parses the column-indexed format into named objects.
- **Frontend builds in containers only.** There is no local Node.js setup. All `npm install` and `npm run build` happen inside Docker. Never run npm locally.
- **`base: '/app/'` in Vite.** The frontend is served at `/app/` via the landing nginx proxy. `BrowserRouter basename="/app"` matches.
- **Rate limiting.** Redis-based rate limiting on auth endpoints (10/5min passkey, 5/5min e-boekhouden).

## Backend Package Structure

| Package | Purpose |
|---------|---------|
| `internal/auth` | WebAuthn passkey service |
| `internal/claude` | Claude API service (invoice reading, transaction classification) |
| `internal/crypto` | AES-256-GCM encrypt/decrypt |
| `internal/database` | PostgreSQL connection, migrations, CRUD (users, teams, passkeys, settings) |
| `internal/eboekhouden` | HTTP client for e-boekhouden.nl (auth, hours, import, mutatie, reference, archive) |
| `internal/handler` | Gin HTTP handlers (passkey, auth, bankstatements, mutations, reference, archive, invoice, classify, settings) |
| `internal/middleware` | CORS + security headers, rate limiting |
| `internal/session` | Redis-backed session store + middleware |
| `internal/config` | Environment-based configuration |

## Frontend Structure

| Path | Purpose |
|------|---------|
| `src/components/auth/` | Passkey login/register |
| `src/components/onboarding/` | First-time setup wizard |
| `src/components/bankstatements/` | Bank statement list + process dialog |
| `src/components/invoices/` | Invoice upload + AI extraction + review form |
| `src/components/settings/` | API key + account + team settings |
| `src/components/shared/` | LedgerAccountPicker, RelationPicker, VATCodePicker |
| `src/components/` | Dashboard, Layout, BulkEntryForm, EBoekhoudenConnectDialog |
| `src/hooks/` | Data fetching hooks (employees, projects, activities, ledger accounts, VAT codes) |
| `src/context/` | AuthContext (two-level: Speedy account + e-boekhouden connection) |
| `src/api/` | API client + TypeScript types |

## Common Pitfalls

1. **Field names from e-boekhouden are Dutch** — `naam` (not `name`), `relatieBedrijf` (not `companyName`), `id` (lowercase). The frontend types in `api/types.ts` must match.
2. **Two auth layers** — The `auth:expired` event in `api/client.ts` must NOT fire for `/auth/*` paths. The session middleware checks Speedy auth; `RequireEBoekhouden` middleware checks e-boekhouden connection.
3. **Cookie domain** — Leave `COOKIE_DOMAIN` empty for local dev.
4. **MFA is optional** — Not all e-boekhouden users have MFA. The login handler detects it by checking the response HTML for `txtCode` or `SCODE`.
5. **ENCRYPTION_KEY is required** — The server refuses to start without it. For local dev, the docker-compose.yml has a zero-key.
6. **MFA-pending sessions** — The middleware does NOT create an e-boekhouden client when `MFAPending=true`. This prevents access to bookkeeping endpoints during incomplete MFA.
7. **Grid format** — The `/v1/api/import/gridtable` endpoint returns column-indexed arrays, not named objects. The Go handler parses this into named rows before sending to the frontend.

## Running Locally

```bash
docker-compose up --build
# Landing page: http://localhost:3000
# App: http://localhost:3000/app/
# Backend API: http://localhost:8080
# PostgreSQL: localhost:5432 (speedy/dev)
# Redis: localhost:6379
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

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Backend listen port |
| `FRONTEND_ORIGIN` | `http://localhost:3000` | CORS allowed origin |
| `COOKIE_DOMAIN` | *(empty)* | Cookie domain (leave empty for local) |
| `COOKIE_SECURE` | `true` | `false` for local HTTP dev |
| `POSTGRES_DSN` | `postgres://speedy:dev@localhost:5432/speedy?sslmode=disable` | PostgreSQL connection string |
| `REDIS_URL` | `redis://localhost:6379` | Redis connection string |
| `WEBAUTHN_ORIGIN` | *(FRONTEND_ORIGIN)* | WebAuthn relying party origin |
| `WEBAUTHN_RP_ID` | `localhost` | WebAuthn relying party ID (domain) |
| `ENCRYPTION_KEY` | *(required)* | 64 hex chars (32 bytes) for AES-256-GCM |

## Content Guidelines

- **Landing page and frontend UI are in Dutch.** All user-facing text must be in Dutch.
- **Code comments, README, CONTRIBUTING, and technical docs are in English.**
- **Dutch compound nouns are one word** — `kalenderselectie`, not `kalender selectie`. Watch for this.
- **"word je" not "wordt je"** — when "je" follows the verb (inversion), the -t drops.
- **No personal information in the repo.** The LICENSE copyright is "Speedy e-Boekhouden Contributors".
- **No user data in logs.** Never log credentials, tokens, API keys, or bookkeeping data.

## Deployment

- **Docker Compose** for local dev (port 3000).
- **Kubernetes** with Kustomize overlays (`deploy/base/`, `deploy/overlays/local/`, `deploy/overlays/production/`).
- **Traefik ingress** with Let's Encrypt TLS in production.
- **GHCR** for container images. CI builds on push to main, release pushes tagged images on `v*` tags.

## Security Model

This is critical context — the security page makes specific claims that must remain truthful:

- **e-Boekhouden credentials never stored.** Forwarded to e-boekhouden.nl and discarded within the request handler.
- **Passkey-only Speedy auth.** No passwords in the system. WebAuthn credentials (public keys only) stored in PostgreSQL.
- **PostgreSQL stores:** user accounts, team membership, passkey public keys, AES-256-GCM encrypted API keys. Nothing else.
- **Redis stores:** sessions with encrypted e-boekhouden auth tokens. 30-minute sliding TTL.
- **Cookies are HttpOnly + Secure + SameSite=Lax** in production.
- **All traffic to e-boekhouden.nl is HTTPS** with no HTTP fallback.
- **Rate limiting** on all auth endpoints via Redis.
- **File uploads limited** to 10 MB.
