#!/usr/bin/env bash
# Smoke tests for Speedy e-Boekhouden
# Run after: docker compose up -d
# Usage: ./test/smoke.sh [base_url]

set -euo pipefail

BASE="${1:-http://localhost:3000}"
API="${BASE}/api/v1"
BACKEND="http://localhost:8080"
PASS=0
FAIL=0
ERRORS=""

green() { printf "\033[32m%s\033[0m\n" "$1"; }
red()   { printf "\033[31m%s\033[0m\n" "$1"; }
bold()  { printf "\033[1m%s\033[0m\n" "$1"; }

assert() {
    local name="$1"
    local result="$2"
    local expected="$3"
    if [ "$result" = "$expected" ]; then
        green "  PASS  $name"
        PASS=$((PASS + 1))
    else
        red "  FAIL  $name (expected: $expected, got: $result)"
        FAIL=$((FAIL + 1))
        ERRORS="${ERRORS}\n  - ${name}"
    fi
}

assert_contains() {
    local name="$1"
    local haystack="$2"
    local needle="$3"
    if echo "$haystack" | grep -q "$needle"; then
        green "  PASS  $name"
        PASS=$((PASS + 1))
    else
        red "  FAIL  $name (expected to contain: $needle)"
        FAIL=$((FAIL + 1))
        ERRORS="${ERRORS}\n  - ${name}"
    fi
}

assert_not_contains() {
    local name="$1"
    local haystack="$2"
    local needle="$3"
    if echo "$haystack" | grep -q "$needle"; then
        red "  FAIL  $name (should NOT contain: $needle)"
        FAIL=$((FAIL + 1))
        ERRORS="${ERRORS}\n  - ${name}"
    else
        green "  PASS  $name"
        PASS=$((PASS + 1))
    fi
}

# ============================================================
bold "Section 1: Infrastructure"
# ============================================================

# Health check
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BACKEND}/healthz")
assert "Backend health check returns 200" "$STATUS" "200"

BODY=$(curl -s "${BACKEND}/healthz")
assert_contains "Health check body has status:ok" "$BODY" '"status":"ok"'

# Landing page
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BASE}/")
assert "Landing page returns 200" "$STATUS" "200"

# Frontend app
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BASE}/app/")
assert "Frontend /app/ returns 200" "$STATUS" "200"

# Frontend JS bundle serves (not HTML fallback)
# Extract the actual bundle filename from the HTML
JS_FILE=$(curl -s "${BASE}/app/" | grep -o 'src="/app/assets/[^"]*\.js"' | head -1 | sed 's/src="//;s/"//')
if [ -n "$JS_FILE" ]; then
    JS_SIZE=$(curl -s -o /dev/null -w "%{size_download}" "${BASE}${JS_FILE}" 2>/dev/null || echo "0")
    if [ "$JS_SIZE" -gt 10000 ] 2>/dev/null; then
        green "  PASS  JS bundle serves correctly (${JS_SIZE} bytes)"
        PASS=$((PASS + 1))
    else
        red "  FAIL  JS bundle returned ${JS_SIZE} bytes (expected >10KB — may be HTML fallback)"
        FAIL=$((FAIL + 1))
        ERRORS="${ERRORS}\n  - JS bundle serves incorrectly"
    fi
else
    red "  FAIL  Could not find JS bundle reference in /app/ HTML"
    FAIL=$((FAIL + 1))
    ERRORS="${ERRORS}\n  - JS bundle not found in HTML"
fi

# API through proxy
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BASE}/api/v1/auth/me")
assert "API through landing proxy returns 401" "$STATUS" "401"

# ============================================================
bold ""
bold "Section 5: Navigation / Routing"
# ============================================================

# Landing subpages
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BASE}/beveiliging")
assert "Security page returns 200" "$STATUS" "200"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BASE}/disclaimer")
assert "Disclaimer page returns 200" "$STATUS" "200"

# /app redirect
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BASE}/app")
assert "/app redirects (301)" "$STATUS" "301"

# ============================================================
bold ""
bold "Section 13: Security Checks"
# ============================================================

# --- Auth guards ---
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BASE}/api/v1/bankstatements")
assert "Bankstatements without session returns 401" "$STATUS" "401"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BASE}/api/v1/employees")
assert "Employees without session returns 401" "$STATUS" "401"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BASE}/api/v1/settings")
assert "Settings without session returns 401" "$STATUS" "401"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BASE}/api/v1/classify" -X POST -H "Content-Type: application/json" -d '{}')
assert "Classify without session returns 401" "$STATUS" "401"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BASE}/api/v1/invoices/analyze" -X POST)
assert "Invoice analyze without session returns 401" "$STATUS" "401"

# --- CORS ---
CORS_HEADERS=$(curl -s -I -H "Origin: https://evil.com" -X OPTIONS "${BACKEND}/api/v1/auth/me" 2>&1)
assert_not_contains "CORS rejects evil.com origin" "$CORS_HEADERS" "evil.com"

CORS_HEADERS=$(curl -s -I -H "Origin: http://localhost:3000" -X OPTIONS "${BACKEND}/api/v1/auth/me" 2>&1)
assert_contains "CORS allows localhost:3000" "$CORS_HEADERS" "localhost:3000"

# Check CORS methods include PUT and DELETE
assert_contains "CORS allows PUT method" "$CORS_HEADERS" "PUT"
assert_contains "CORS allows DELETE method" "$CORS_HEADERS" "DELETE"

# Check CORS allows X-Challenge-ID header
assert_contains "CORS allows X-Challenge-ID header" "$CORS_HEADERS" "X-Challenge-ID"

# --- Security headers ---
HEADERS=$(curl -s -I "${BACKEND}/api/v1/auth/me" 2>&1)
assert_contains "X-Content-Type-Options: nosniff present" "$HEADERS" "nosniff"
assert_contains "X-Frame-Options present" "$HEADERS" "DENY"
assert_contains "Referrer-Policy present" "$HEADERS" "strict-origin"

# --- Registration validation (run BEFORE rate limit test) ---
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" \
    -d '{"email":"bad","name":"test"}' "${BACKEND}/api/v1/auth/register/begin")
assert "Registration rejects invalid email" "$STATUS" "400"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" \
    -d '{"email":"test@example.com"}' "${BACKEND}/api/v1/auth/register/begin")
assert "Registration requires name" "$STATUS" "400"

# --- Rate limiting (run LAST in security section — burns the rate limit) ---
# Rate limiting is disabled in dev mode (COOKIE_SECURE=false).
# Test it only if COOKIE_SECURE is not explicitly false.
if [ "${COOKIE_SECURE:-}" != "false" ]; then
    bold ""
    bold "  Rate limit test (21 rapid requests to login/begin, limit=20)..."
    RATE_LIMITED="no"
    for i in $(seq 1 21); do
        STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" "${BACKEND}/api/v1/auth/login/begin")
        if [ "$STATUS" = "429" ]; then
            RATE_LIMITED="yes"
            break
        fi
    done
    assert "Rate limiting triggers on login endpoint" "$RATE_LIMITED" "yes"
else
    green "  SKIP  Rate limiting disabled in dev mode (COOKIE_SECURE=false)"
fi

# --- e-Boekhouden auth without Speedy session ---
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" \
    -d '{"email":"test@test.com","password":"test"}' "${BACKEND}/api/v1/eboekhouden/login")
assert "e-Boekhouden login without Speedy session returns 401" "$STATUS" "401"

# ============================================================
bold ""
bold "Section 15: Landing Page Content"
# ============================================================

LANDING=$(curl -s "${BASE}/")
assert_contains "Landing has new hero: Supercharge" "$LANDING" "Supercharge"
assert_contains "Landing mentions afschriften" "$LANDING" "afschriften"
assert_contains "Landing mentions factuur" "$LANDING" "factuur"
assert_contains "Landing mentions passkey" "$LANDING" "passkey"
assert_contains "Landing mentions AI" "$LANDING" "AI"
assert_not_contains "Landing no longer says 'geen account nodig'" "$LANDING" "geen account nodig"

# Security page
SECURITY=$(curl -s "${BASE}/beveiliging")
assert_contains "Security page mentions passkey" "$SECURITY" "passkey"
assert_contains "Security page mentions PostgreSQL" "$SECURITY" "PostgreSQL"
assert_contains "Security page mentions AES-256" "$SECURITY" "AES-256"
assert_contains "Security page mentions rate limiting" "$SECURITY" "rate limiting"
assert_not_contains "Security page no longer says 'geen database'" "$SECURITY" "geen database"

# ============================================================
bold ""
bold "Section 14: Accessibility (HTML structure)"
# ============================================================

APP_HTML=$(curl -s "${BASE}/app/")
assert_contains "Frontend HTML has root div" "$APP_HTML" 'id="root"'
assert_contains "Frontend HTML lang=nl" "$APP_HTML" 'lang="nl"'

LANDING_HTML=$(curl -s "${BASE}/")
assert_contains "Landing has single h1" "$LANDING_HTML" "<h1>"
assert_contains "Landing has semantic main element" "$LANDING_HTML" "<main>"
assert_contains "Landing has semantic nav" "$LANDING_HTML" '<nav'

# ============================================================
bold ""
bold "API Contract Tests"
# ============================================================

# Public endpoints accept POST
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" \
    -d '{}' "${BACKEND}/api/v1/auth/login/begin")
# Should be 200 (returns challenge) or 400 (bad request) but NOT 404 or 405
if [ "$STATUS" = "200" ] || [ "$STATUS" = "400" ] || [ "$STATUS" = "429" ]; then
    green "  PASS  POST /auth/login/begin is routed (${STATUS})"
    PASS=$((PASS + 1))
else
    red "  FAIL  POST /auth/login/begin unexpected status: ${STATUS}"
    FAIL=$((FAIL + 1))
fi

# Health check is public (no auth)
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BACKEND}/healthz")
assert "GET /healthz is public" "$STATUS" "200"

# All authenticated endpoints return 401 without session
for ENDPOINT in \
    "GET /api/v1/auth/me" \
    "GET /api/v1/eboekhouden/status" \
    "GET /api/v1/settings" \
    "GET /api/v1/ledger-accounts" \
    "GET /api/v1/relations" \
    "GET /api/v1/vat-codes" \
    "GET /api/v1/bankstatements" \
    "GET /api/v1/bankstatements/count" \
    "GET /api/v1/employees" \
    "GET /api/v1/projects" \
    "GET /api/v1/activities" \
    "GET /api/v1/archive/folders"; do
    METHOD=$(echo "$ENDPOINT" | cut -d' ' -f1)
    PATH_=$(echo "$ENDPOINT" | cut -d' ' -f2)
    STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X "$METHOD" "${BACKEND}${PATH_}")
    assert "${ENDPOINT} without auth returns 401" "$STATUS" "401"
done

# ============================================================
# Summary
# ============================================================
bold ""
bold "============================================"
TOTAL=$((PASS + FAIL))
if [ "$FAIL" -eq 0 ]; then
    green "ALL ${TOTAL} TESTS PASSED"
else
    red "${FAIL} FAILED out of ${TOTAL} tests"
    echo -e "\nFailed tests:${ERRORS}"
fi
bold "============================================"

exit "$FAIL"
