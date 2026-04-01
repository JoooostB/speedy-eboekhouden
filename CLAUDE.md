# CLAUDE.md

Instructions for Claude Code instances working on this repository.

## Project Overview

Speedy e-Boekhouden is a bulk hour logging tool for [e-boekhouden.nl](https://www.e-boekhouden.nl). It replaces the tedious manual hour entry process (15 clicks per entry) with a single-page form where users select employees, a project, an activity, pick dates on a calendar, and submit everything at once.

## Architecture

Three containers behind a single nginx entry point:

- **`landing/`** — Static HTML/CSS landing page, security page, and disclaimer. Served by nginx. Acts as the public entry point and reverse proxy for `/app/` and `/api/`.
- **`frontend/`** — React 19 + TypeScript 6 + MUI 7 SPA, built with Vite 8. Served at `/app/` with `base: '/app/'` in vite.config.ts.
- **`backend/`** — Go 1.26 + Gin API server. Proxies all calls to e-boekhouden.nl. No database.

```
Browser → landing nginx (:3000)
              ├── /           → static landing page
              ├── /beveiliging → security page
              ├── /disclaimer  → disclaimer page
              ├── /app/       → proxy to frontend nginx (:80)
              └── /api/       → proxy to backend (:8080)
```

## Key Technical Decisions

- **No database.** Sessions are in-memory (`map[string]*Session` with `sync.RWMutex`). Server restart = all sessions gone. This is intentional for security.
- **Raw JSON passthrough.** The backend does not deserialize e-boekhouden API responses. It passes `json.RawMessage` directly to the frontend with `c.Data()`. This avoids field name mismatches (the API uses Dutch field names like `naam`, `relatieBedrijf`).
- **Frontend builds in containers only.** There is no local Node.js setup. All `npm install` and `npm run build` happen inside Docker. Never run npm locally.
- **`base: '/app/'` in Vite.** The frontend is served at `/app/` via the landing nginx proxy. All asset paths are prefixed accordingly.
- **`^~` on `/app/` location in landing nginx.** Without this, the regex location for static asset caching (`.js`, `.css`, etc.) would intercept `/app/assets/*.js` requests before they reach the proxy.
- **`absolute_redirect off`** in landing nginx. Without this, `return 301 /app/;` generates an absolute redirect to port 80 (the container's internal port) instead of the host-mapped port.
- **Session check on mount.** `GET /api/v1/me` lets the frontend check if an existing session cookie is still valid, so the page survives reloads.
- **`HandleMethodNotAllowed = false`** on the Gin router. Without this, Gin auto-rejects OPTIONS preflight requests with 405 for routes only registered for POST.

## Common Pitfalls

1. **Field names from e-boekhouden are Dutch** — `naam` (not `name`), `relatieBedrijf` (not `companyName`), `id` (lowercase). The frontend types in `api/types.ts` must match.
2. **CORS on login** — The `auth:expired` event in `api/client.ts` must NOT fire for `/login`, `/mfa`, or `/me` paths, or wrong credentials will show "session expired" instead of the actual error.
3. **Cookie domain** — Leave `COOKIE_DOMAIN` empty for local dev. Setting it to `localhost` breaks cookie behavior in some browsers.
4. **MFA is optional** — Not all users have MFA. The login handler detects it by checking the response HTML for `txtCode` or `SCODE`.

## Running Locally

```bash
docker-compose up --build
# Landing page: http://localhost:3000
# App: http://localhost:3000/app/
# Backend API: http://localhost:8080
```

For backend-only iteration:

```bash
cd backend
FRONTEND_ORIGIN=http://localhost:3000 COOKIE_SECURE=false go run ./cmd/server
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Backend listen port |
| `FRONTEND_ORIGIN` | `http://localhost:3000` | CORS allowed origin |
| `COOKIE_DOMAIN` | *(empty)* | Cookie domain (leave empty for local) |
| `COOKIE_SECURE` | `true` | `false` for local HTTP dev |

## Content Guidelines

- **Landing page and frontend UI are in Dutch.** All user-facing text must be in Dutch.
- **Code comments, README, CONTRIBUTING, and technical docs are in English.**
- **Dutch compound nouns are one word** — `kalenderselectie`, not `kalender selectie`. Watch for this.
- **"word je" not "wordt je"** — when "je" follows the verb (inversion), the -t drops.
- **No personal information in the repo.** The LICENSE copyright is "Speedy e-Boekhouden Contributors".
- **No user data in logs.** Debug log lines that output API responses were removed. Do not add them back.

## Deployment

- **Docker Compose** for local dev (port 3000).
- **Kubernetes** with Kustomize overlays (`deploy/base/`, `deploy/overlays/local/`, `deploy/overlays/production/`).
- **Traefik ingress** with Let's Encrypt TLS in production.
- **GHCR** for container images. CI builds on push to main, release pushes tagged images on `v*` tags.

## Security Model

This is critical context — the security page makes specific claims that must remain truthful:

- Credentials are never stored (forwarded and discarded within the request handler).
- No database exists anywhere in the stack.
- Sessions are in-memory only with 30-minute sliding expiry.
- Cookies are HttpOnly + Secure (in production).
- All traffic to e-boekhouden.nl is HTTPS with no HTTP fallback.
- The `auth-token` from e-boekhouden is the only sensitive data held, and only in RAM.
