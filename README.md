# Speedy e-Boekhouden

**Bulk hour logging for [e-boekhouden.nl](https://www.e-boekhouden.nl)**

Enter hours for multiple employees, across multiple days, in a single action.
No more clicking through 15 form fields per entry.

[![CI](https://github.com/joooostb/speedy-eboekhouden/actions/workflows/ci.yml/badge.svg)](https://github.com/joooostb/speedy-eboekhouden/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](backend/go.mod)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](frontend/package.json)
[![TypeScript](https://img.shields.io/badge/TypeScript-6-3178C6?logo=typescript&logoColor=white)](frontend/package.json)

[Live App](https://speedy-eboekhouden.nl/app/) | [Security](https://speedy-eboekhouden.nl/beveiliging) | [Contributing](CONTRIBUTING.md)

---

## The Problem

Logging hours in e-boekhouden.nl requires **15 clicks per day per employee**: navigate to the form, select a date, pick an employee, find the project, choose an activity, enter hours, save, repeat. For a team of 4 people over a month, that's **1,200+ clicks**.

## The Solution

Select employees, pick a project, choose an activity, click the days on a calendar, submit. **Done in 30 seconds.**

```
┌─────────────┐         ┌──────────────┐         ┌────────────────────┐
│   Browser   │──HTTPS──▶  Go Backend  │──HTTPS──▶  e-boekhouden.nl  │
│  React/MUI  │         │    (Gin)     │         │    (API + Auth)    │
│  TypeScript │◀─────────│ Session Mgr │◀─────────│                   │
└─────────────┘         └──────────────┘         └────────────────────┘
```

## Features

| Feature | Description |
|---------|-------------|
| **Bulk entry** | Select employees, project, activity, pick dates, submit all at once |
| **Multi-employee** | Log hours for your entire team in one go |
| **Calendar selection** | Click individual dates, shift+click for ranges, "all weekdays" shortcut |
| **Concurrent submission** | 3 parallel workers — 60 entries in ~20 seconds |
| **Per-entry feedback** | See exactly what succeeded and what failed |
| **Session persistence** | Survives page reloads within the 30-minute session window |
| **MFA support** | Full two-factor authentication support |
| **Zero storage** | No database, no credential storage, no tracking |

## Quick Start

### Prerequisites

- Docker + Docker Compose
- An [e-boekhouden.nl](https://www.e-boekhouden.nl) account

### Run

```bash
git clone https://github.com/joooostb/speedy-eboekhouden.git
cd speedy-eboekhouden
docker-compose up --build
```

Open **http://localhost:3000** — that's it.

> **Note:** No local Node.js required. All frontend builds happen inside containers.

## Project Structure

```
.
├── backend/                  Go API server
│   ├── cmd/server/           Entry point + route setup
│   └── internal/
│       ├── eboekhouden/      HTTP client for e-boekhouden API
│       ├── session/          In-memory session store + middleware
│       ├── handler/          HTTP handlers
│       ├── middleware/       CORS
│       └── config/           Environment-based config
│
├── frontend/                 React SPA
│   └── src/
│       ├── api/              API client + TypeScript types
│       ├── components/       UI components
│       ├── hooks/            Data fetching hooks
│       └── context/          Auth state management
│
├── landing/                  Static landing page + security page
│
├── deploy/                   Kubernetes manifests (Kustomize)
│   ├── base/                 Base resources
│   └── overlays/
│       ├── local/            Local dev overrides
│       └── production/       Production config
│
└── docker-compose.yml
```

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.26, Gin |
| Frontend | React 19, TypeScript 6, MUI 7, Vite 8 |
| Landing | Static HTML + CSS (no JS framework) |
| Containers | Docker, nginx 1.27 |
| Orchestration | Kubernetes, Kustomize, Traefik |
| CI/CD | GitHub Actions |
| TLS | Let's Encrypt (via Traefik) |

## API

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/v1/login` | — | Authenticate with e-boekhouden |
| `POST` | `/api/v1/mfa` | Session | Submit MFA code |
| `GET` | `/api/v1/me` | Session | Check session validity |
| `POST` | `/api/v1/logout` | Session | Destroy session |
| `GET` | `/api/v1/employees` | Session | List employees |
| `GET` | `/api/v1/projects` | Session | List active projects |
| `GET` | `/api/v1/activities` | Session | List active activities |
| `POST` | `/api/v1/hours` | Session | Bulk submit hour entries |
| `GET` | `/healthz` | — | Health check |

### Example: Bulk Hours Request

```json
{
  "entries": [
    {
      "employeeId": 12345,
      "projectId": 67890,
      "activityId": 11111,
      "hours": "8.00",
      "dates": ["2026-03-02", "2026-03-03", "2026-03-04"],
      "description": ""
    }
  ]
}
```

### Example: Bulk Hours Response

```json
{
  "results": [
    { "employeeId": 12345, "date": "2026-03-02", "status": "ok" },
    { "employeeId": 12345, "date": "2026-03-03", "status": "ok" },
    { "employeeId": 12345, "date": "2026-03-04", "status": "ok" }
  ]
}
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Backend listen port |
| `FRONTEND_ORIGIN` | `http://localhost:3000` | Allowed CORS origin |
| `COOKIE_DOMAIN` | *(empty)* | Session cookie domain |
| `COOKIE_SECURE` | `true` | `false` for local HTTP dev |

## Deployment

### Docker Compose (local)

```bash
docker-compose up --build
```

### Kubernetes (production)

```bash
kubectl apply -k deploy/overlays/production
```

Deploys to namespace `speedy-eboekhouden` with Traefik ingress, Let's Encrypt TLS, resource limits, and health probes.

## Security

Speedy is designed around a **zero-storage architecture**:

- **No database** — no MySQL, no Postgres, no Redis. Nothing.
- **No credential storage** — passwords are forwarded to e-boekhouden.nl and immediately discarded.
- **In-memory sessions only** — expire after 30 minutes, wiped on server restart.
- **HttpOnly + Secure cookies** — inaccessible to JavaScript, HTTPS-only in production.
- **HTTPS everywhere** — TLS on both sides (browser to Speedy, Speedy to e-boekhouden).
- **No tracking** — no analytics, no third-party cookies, no tracking pixels.

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
