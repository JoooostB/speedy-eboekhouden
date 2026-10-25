# Contributing to Speedy e-Boekhouden

Thanks for wanting to help. Speedy started as a tool for logging hours in e-boekhouden.nl and now also handles bank lines, invoices and the Mail & herken folder, with Claude doing the reading and classifying.

## Getting started

### Prerequisites

- Docker with Compose v2, to run the full stack
- Go 1.26 or newer, for backend work outside Docker
- An e-boekhouden.nl account, to try changes against a real administration
- Optionally, an Anthropic API key to try the AI features

You don't need Node.js on your machine. The frontend is built, type-checked and tested inside Docker.

### Running locally

```bash
cp .env.example .env
docker compose up --build
```

This starts the backend (:8080), the frontend (only reachable through the landing proxy), the landing page with the app at <http://localhost:3000/app/>, PostgreSQL (:5432) and Redis (:6379).

For backend-only work, with PostgreSQL and Redis already running:

```bash
cd backend
POSTGRES_DSN=postgres://speedy:dev@localhost:5432/speedy?sslmode=disable \
REDIS_URL=redis://localhost:6379 \
ENCRYPTION_KEY=0000000000000000000000000000000000000000000000000000000000000000 \
COOKIE_SECURE=false \
go run ./cmd/server
```

### Running the tests

```bash
# Backend
cd backend && go build ./... && go vet ./... && go test ./... -race

# Frontend: tsc runs as part of the image build, then vitest
docker build --target builder -t speedy-frontend-test ./frontend
docker run --rm speedy-frontend-test npx vitest run

# Smoke tests against a running stack
./test/smoke.sh http://localhost:3000
```

Handler tests run against a fake e-boekhouden (`httptest`), so they need no account. `TESTPLAN.md` is the manual checklist for what only a real administration can show.

## How to contribute

### Reporting bugs

Open an issue on GitHub with:

- what you expected to happen
- what happened instead
- steps to reproduce
- browser and OS, for frontend bugs

### Suggesting features

Open an issue with the `enhancement` label. Describe the problem you want solved as well as the solution you have in mind.

### Submitting code

1. Fork the repo and create a branch from `main`.
2. Keep the change to one concern.
3. Run the tests above, and for UI changes try the flow in the browser with `docker compose up --build`.
4. Open a pull request that explains what changes and why.

### Pull request guidelines

- One feature or fix per PR.
- Write commit messages in the imperative mood ("Add X", not "Added X").
- Don't add dependencies without a good reason.
- CI must pass before merge. It builds, vets and tests the backend with `-race`, type-checks the frontend and builds the three Docker images. CI does not run vitest, so run it yourself.

## Project structure

```text
backend/         Go API server (Gin, PostgreSQL, Redis)
frontend/        React app (Vite, TypeScript, MUI, react-router-dom)
landing/         Static landing, security and disclaimer pages
deploy/          Kubernetes manifests (Kustomize)
test/smoke.sh    Smoke tests
```

### Backend

The backend handles passkey authentication and sessions, talks to e-boekhouden.nl on the user's behalf and calls the Claude API.

| Package | Purpose |
|---------|---------|
| `internal/auth` | WebAuthn passkey service |
| `internal/claude` | Claude API: document reading, bank-line classification |
| `internal/config` | Environment-based configuration |
| `internal/crypto` | AES-256-GCM encryption |
| `internal/database` | PostgreSQL: users, teams, passkeys, settings, learned bookings |
| `internal/eboekhouden` | HTTP client for e-boekhouden.nl |
| `internal/handler` | Gin HTTP handlers |
| `internal/mail` | SMTP mailer for welcome and recovery e-mails |
| `internal/middleware` | CORS, security headers, rate limiting |
| `internal/session` | Redis-backed session store and middleware |
| `internal/storage` | Cloudflare R2 client for avatars |

### Frontend

A React SPA with two levels of authentication: the Speedy passkey session, then the e-boekhouden connection. Components live in `src/components/`, API calls in `src/api/`, auth state in `src/context/`. Unit tests (`*.test.ts`) sit next to the code they test.

### Landing

Static HTML and CSS with one small shared script, served by nginx. There is no build step.

## Code style

- Go: `gofmt`, and `go vet` must pass.
- TypeScript: follow the existing patterns; the compiler catches most mistakes.
- CSS: kebab-case class names, and the CSS variables already defined in `landing/style.css`.
- Text: the landing pages and the app UI are in Dutch; code comments and docs are in English.

## Security

This project handles e-boekhouden credentials, tokens and API keys. If your change touches authentication, sessions, encryption or data handling:

- Never log credentials, tokens, API keys or bookkeeping data.
- Never store e-boekhouden passwords or MFA codes, on disk or in the database.
- Keep the HttpOnly, Secure and SameSite cookie flags.
- Use HTTPS for every external call.
- Use parameterized queries for every database operation.
- Encrypt sensitive data at rest with the existing AES-256-GCM helpers.

Report security vulnerabilities to info@speedy-eboekhouden.nl, not in a public issue.

## AI-assisted development

Much of this project was written with [Claude](https://claude.ai) by Anthropic. If you use AI tools for a contribution, review and test the generated code before you submit it.

## License

By contributing, you agree that your contributions are licensed under the MIT License.
