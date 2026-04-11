# Contributing to Speedy e-Boekhouden

Thanks for considering contributing! This project started as a simple hour-logging tool and has grown into a full bookkeeping platform with AI features. Every contribution helps.

## Getting Started

### Prerequisites

- **Go 1.26+** (backend)
- **Docker + Docker Compose** (full stack — recommended)
- An **e-boekhouden.nl** account (for integration testing)
- (Optional) **Anthropic API key** (for testing AI features)

> **Note:** You do not need Node.js installed locally. All frontend builds happen inside Docker containers.

### Running Locally

```bash
# Full stack (recommended)
docker-compose up --build
```

This starts: backend (:8080), frontend (inside Docker), landing page (:3000), PostgreSQL (:5432), Redis (:6379).

For backend-only development (requires running PostgreSQL and Redis):

```bash
cd backend
POSTGRES_DSN=postgres://speedy:dev@localhost:5432/speedy?sslmode=disable \
REDIS_URL=redis://localhost:6379 \
ENCRYPTION_KEY=0000000000000000000000000000000000000000000000000000000000000000 \
COOKIE_SECURE=false \
go run ./cmd/server
```

## How to Contribute

### Reporting Bugs

Open an issue on GitHub with:
- What you expected to happen
- What actually happened
- Steps to reproduce
- Browser and OS (for frontend bugs)

### Suggesting Features

Open an issue with the `enhancement` label. Describe the use case, not just the solution.

### Submitting Code

1. **Fork** the repo and create a branch from `main`
2. **Make your changes** — keep them focused on a single concern
3. **Test locally** — run `docker-compose up --build` and verify the full flow
4. **Backend changes** — ensure `go vet ./...` and `go build ./...` pass
5. **Open a pull request** — describe what and why, not how (the code shows how)

### Pull Request Guidelines

- Keep PRs small and focused. One feature or fix per PR.
- Write clear commit messages. Prefer imperative mood ("Add X" not "Added X").
- Don't introduce new dependencies without good reason.
- All CI checks must pass before merge.

## Project Structure

```
backend/         Go API server (Gin + PostgreSQL + Redis)
frontend/        React app (Vite + TypeScript + MUI + react-router-dom)
landing/         Static landing page + security page
deploy/          Kubernetes manifests (Kustomize)
```

### Backend

The backend handles authentication (passkeys), session management (Redis), proxies calls to e-boekhouden.nl, and integrates the Claude API for AI features.

| Package | Purpose |
|---------|---------|
| `internal/auth` | WebAuthn passkey service |
| `internal/claude` | Claude API (invoice OCR, transaction classification) |
| `internal/crypto` | AES-256-GCM encryption |
| `internal/database` | PostgreSQL: users, teams, passkeys, settings |
| `internal/eboekhouden` | HTTP client for e-boekhouden.nl API |
| `internal/handler` | Gin HTTP handlers |
| `internal/middleware` | CORS, security headers, rate limiting |
| `internal/session` | Redis-backed session store + middleware |
| `internal/config` | Environment-based configuration |

### Frontend

React SPA with two-level auth (Speedy passkey + e-boekhouden connection). Components in `src/components/`, API calls in `src/api/`, state management in `src/context/`.

### Landing

Static HTML + CSS. No build step. Served by nginx.

## Code Style

- **Go:** Standard `gofmt` formatting. Run `go vet`.
- **TypeScript:** Follow existing patterns. TypeScript compiler catches most issues.
- **CSS:** BEM-ish class names. Use existing CSS variables in `landing/style.css`.

## Security

This project handles user credentials and API keys. If you're contributing code that touches authentication, sessions, encryption, or data handling:

- Never log credentials, tokens, API keys, or bookkeeping data
- Never persist e-boekhouden credentials to disk or database
- Keep HttpOnly, Secure, and SameSite cookie flags
- Ensure all external calls use HTTPS
- Use parameterized queries for all database operations
- Encrypt sensitive data at rest with the existing AES-256-GCM utilities

If you find a security vulnerability, please **do not open a public issue**. Instead, email info@speedy-eboekhouden.nl directly.

## AI-Assisted Development

This project was substantially developed with the help of [Claude](https://claude.ai) by Anthropic. If you use AI tools for your contributions, please review and test all generated code before submitting.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
