# Contributing to Speedy e-Boekhouden

Thanks for considering contributing! This project is a community tool built out of shared frustration with manual hour entry in e-boekhouden.nl. Every contribution helps.

## Getting Started

### Prerequisites

- **Go 1.26+** (backend)
- **Docker + Docker Compose** (full stack)
- An **e-boekhouden.nl** account (for integration testing)

> **Note:** You do not need Node.js installed locally. All frontend builds happen inside Docker containers.

### Running Locally

```bash
# Full stack (recommended)
docker-compose up --build
```

This starts the backend on port 8080, the frontend on port 80 (inside Docker), and the landing page on port 3000.

For backend-only development:

```bash
cd backend
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
4. **Backend changes** — ensure `go vet ./...` and `go test ./...` pass
5. **Open a pull request** — describe what and why, not how (the code shows how)

### Pull Request Guidelines

- Keep PRs small and focused. One feature or fix per PR.
- Write clear commit messages. Prefer imperative mood ("Add X" not "Added X").
- Don't introduce new dependencies without good reason.
- All CI checks must pass before merge.

## Project Structure

```
backend/         Go API server (Gin)
frontend/        React app (Vite + TypeScript + MUI)
landing/         Static landing page + security page
deploy/          Kubernetes manifests (Kustomize)
```

### Backend

The backend is a thin proxy between the browser and e-boekhouden.nl. It handles authentication, session management, and forwards API calls. Key packages:

| Package | Purpose |
|---------|---------|
| `internal/eboekhouden` | HTTP client for e-boekhouden.nl API |
| `internal/session` | In-memory session store + middleware |
| `internal/handler` | Gin HTTP handlers |
| `internal/middleware` | CORS |
| `internal/config` | Environment-based configuration |

### Frontend

The frontend is a single-page React app. Components live in `src/components/`, API calls in `src/api/`, and state management in `src/context/`.

### Landing

Static HTML + CSS. No build step. Served by nginx.

## Code Style

- **Go:** Standard `gofmt` formatting. No linter config needed — just run `go vet`.
- **TypeScript:** Follow the existing patterns. No explicit linter config; the TypeScript compiler catches most issues.
- **CSS:** BEM-ish class names. Use the existing CSS variables in `landing/style.css`.

## Security

This project handles user credentials (forwarded to e-boekhouden.nl, never stored). If you're contributing code that touches authentication, sessions, or data handling:

- Never log credentials, tokens, or user data
- Never persist sensitive data to disk
- Keep the HttpOnly and Secure cookie flags
- Ensure all external calls use HTTPS

If you find a security vulnerability, please **do not open a public issue**. Instead, email info@speedy-eboekhouden.nl directly.

## AI-Assisted Development

This project was substantially developed with the help of [Claude](https://claude.ai) by Anthropic. If you use AI tools for your contributions, please review and test all generated code before submitting.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
