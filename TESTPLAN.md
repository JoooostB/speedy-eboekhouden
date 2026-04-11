# Test Plan — Speedy e-Boekhouden

Manual acceptance test plan covering all features. Run through this after `docker-compose up --build`.

## Prerequisites

- [ ] Docker + Docker Compose running
- [ ] Access to an e-boekhouden.nl account with at least one employee, project, and activity
- [ ] (Optional) Anthropic API key for AI features
- [ ] Modern browser with passkey support (Chrome, Safari, Firefox)

---

## 1. Infrastructure

- [ ] `docker-compose up --build` starts all 5 services without errors
- [ ] Landing page loads at http://localhost:3000
- [ ] App loads at http://localhost:3000/app/
- [ ] Backend health check returns OK: `curl http://localhost:8080/healthz`
- [ ] PostgreSQL is reachable: `docker-compose exec postgres psql -U speedy -c '\dt'` shows tables
- [ ] Redis is reachable: `docker-compose exec redis redis-cli ping` returns PONG

## 2. Passkey Registration

- [ ] Navigate to http://localhost:3000/app/
- [ ] See passkey login screen with "Speedy e-Boekhouden" heading
- [ ] Click "Registreer hier" link — registration form appears
- [ ] Enter email and name, click "Registreren met passkey"
- [ ] Browser passkey prompt appears (Touch ID / Face ID / security key)
- [ ] After passkey creation, redirected to onboarding wizard
- [ ] Verify: user + team created in database: `docker-compose exec postgres psql -U speedy -c 'SELECT email, name FROM users'`

## 3. Onboarding Wizard

- [ ] Step 1: e-Boekhouden connection form shows
- [ ] Warning text about skipping is visible
- [ ] Enter e-boekhouden credentials, click "Verbinden"
- [ ] If MFA required: MFA code input appears, enter code, verify succeeds
- [ ] After connection: automatically advances to step 2
- [ ] Step 2: API key input shows (optional)
- [ ] Enter Anthropic API key (sk-ant-...) or click "Overslaan"
- [ ] Step 3: Success screen with "Naar het dashboard" button
- [ ] Click button — redirected to dashboard
- [ ] Verify: refreshing the page does NOT show onboarding again

## 4. Dashboard

- [ ] Dashboard shows 4 cards: Afschriften, Facturen, Uren, API-sleutel
- [ ] If e-boekhouden connected: afschriften card shows unprocessed count (number)
- [ ] If not connected: warning banner shows "Verbind met e-Boekhouden"
- [ ] If no API key: info banner shows "Stel een Anthropic API-sleutel in"
- [ ] Clicking each card navigates to the correct page
- [ ] API key card shows "Geconfigureerd" or "Niet ingesteld" correctly

## 5. Navigation

- [ ] App bar shows: Speedy, tabs (Dashboard, Uren, Afschriften, Facturen), connection chip, settings icon, logout icon
- [ ] Clicking each tab navigates to the correct page
- [ ] Active tab is highlighted with indicator
- [ ] On /instellingen: no tab is highlighted (settings is not a tab)
- [ ] Connection chip shows "Verbonden" (green) or "Niet verbonden"
- [ ] Clicking "Niet verbonden" chip opens e-boekhouden connect dialog
- [ ] Settings icon navigates to /instellingen
- [ ] Logout icon logs out and shows passkey login screen
- [ ] On mobile (narrow viewport): tabs become scrollable, chip label hides

## 6. e-Boekhouden Connect Dialog

- [ ] Opens from the connection chip or when accessing a feature without connection
- [ ] Enter e-boekhouden email + password
- [ ] Press Enter to submit (form element)
- [ ] If MFA required: MFA code input appears
- [ ] After successful connection: dialog closes, chip updates to "Verbonden"
- [ ] "Annuleren" closes the dialog without connecting

## 7. Bulk Hour Entry (Uren)

- [ ] Navigate to /uren
- [ ] Employee selector loads employees from e-boekhouden
- [ ] Project selector loads projects (search works)
- [ ] Activity selector loads activities
- [ ] Calendar shows current month with Dutch day names
- [ ] Can select individual days, shift+click for range, "Alle werkdagen" button
- [ ] Dutch holidays are marked
- [ ] Submit button sends hours
- [ ] Results show per-entry success/failure status
- [ ] If e-boekhouden not connected: shows error message

## 8. Bank Statement Processing (Afschriften)

- [ ] Navigate to /afschriften
- [ ] If not connected: shows warning to connect
- [ ] If connected: table loads with unprocessed bank statement lines
- [ ] Table shows: datum, omschrijving, bedrag (colored +/-), rekening, status
- [ ] Positive amounts show green with + prefix
- [ ] Negative amounts show red with - prefix
- [ ] "Onverwerkt" chip shows total count
- [ ] Refresh button reloads data
- [ ] If all processed: shows success icon "Alle afschriftregels zijn verwerkt"
- [ ] **Keyboard**: can Tab to a row, press Enter to open it
- [ ] Click a row: ProcessDialog opens

### 8a. Process Dialog

- [ ] Shows bank line details (omschrijving, bedrag)
- [ ] If API key set: AI classification runs automatically
- [ ] AI suggestion alert appears with confidence percentage
- [ ] Form fields are pre-filled from AI suggestion (soort, tegenrekening, BTW)
- [ ] All fields are editable: type boeking, tegenrekening, relatie, BTW-code, bedrag, BTW-bedrag, factuurnummer, omschrijving
- [ ] LedgerAccountPicker: type to search, grouped by category
- [ ] RelationPicker: type 2+ chars to search (async)
- [ ] VATCodePicker: dropdown with code + percentage
- [ ] "AI-suggestie" button re-runs classification
- [ ] "Verwerken" button submits the booking
- [ ] After success: row disappears from the table, count decreases
- [ ] "Annuleren" closes dialog without changes

## 9. Invoice Processing (Facturen)

- [ ] Navigate to /facturen
- [ ] If not connected: shows warning
- [ ] Upload zone shows with drag-and-drop area
- [ ] **Keyboard**: can Tab to the upload zone and activate it
- [ ] Click "Bestand kiezen" or drop a PDF file
- [ ] Loading spinner shows "Factuur wordt geanalyseerd door AI..."
- [ ] If no API key: error "Stel eerst een Anthropic API-sleutel in"
- [ ] After analysis: review form shows with extracted data
  - [ ] Leverancier field populated
  - [ ] Factuurnummer populated
  - [ ] Datum populated (date picker)
  - [ ] Bedrag excl./incl. BTW and BTW-bedrag populated
  - [ ] Grootboekrekening suggested
  - [ ] BTW-code suggested
  - [ ] Omschrijving populated
  - [ ] Confidence percentage shown
- [ ] All fields are editable
- [ ] RelationPicker for linking to a supplier
- [ ] "Factuur boeken" submits the invoice
- [ ] Success screen shows mutatienummer
- [ ] "Volgende factuur" resets the form

## 10. Settings (Instellingen)

- [ ] Navigate to /instellingen
- [ ] Page heading is "Instellingen"
- [ ] **API key section:**
  - [ ] Shows "Ingesteld" chip if key exists
  - [ ] Can enter new key (sk-ant-...) and save
  - [ ] Invalid key format shows error
  - [ ] "Verwijderen" button removes the key
  - [ ] After save: chip updates to "Ingesteld"
- [ ] **Account section:**
  - [ ] Shows user name, email, "Passkey" as auth method
- [ ] **Team section:**
  - [ ] Shows team name

## 11. Passkey Login (returning user)

- [ ] Log out
- [ ] On login screen: click "Inloggen met passkey"
- [ ] Browser passkey prompt appears
- [ ] Select your passkey (discoverable credential)
- [ ] After authentication: redirected to dashboard (not onboarding)
- [ ] e-Boekhouden is NOT connected (fresh session — need to reconnect)

## 12. Session Expiry

- [ ] Log in and connect to e-boekhouden
- [ ] Wait 30+ minutes (or manually delete the session from Redis)
- [ ] Try to navigate — should redirect to login screen
- [ ] Verify: no stale data visible

## 13. Security Checks

- [ ] Open browser DevTools → Application → Cookies
  - [ ] `speedy-session` cookie is HttpOnly: yes
  - [ ] Secure flag: matches COOKIE_SECURE setting
- [ ] Try accessing `/api/v1/bankstatements` without session cookie: returns 401
- [ ] Try accessing `/api/v1/bankstatements` with session but no e-boekhouden connection: returns 412
- [ ] Check CORS: `curl -H "Origin: https://evil.com" -I http://localhost:8080/api/v1/auth/me` — should NOT have evil.com in Allow-Origin
- [ ] Rate limiting: send 11 rapid requests to `/api/v1/auth/login/begin` — 11th should return 429

## 14. Accessibility Spot Checks

- [ ] Can complete full login flow using only keyboard (no mouse)
- [ ] Screen reader announces loading states (check aria-live regions)
- [ ] All page headings are h1 (check with browser devtools)
- [ ] Tab navigation has visible focus indicators
- [ ] Bank statement table rows are focusable and activatable with Enter

## 15. Landing Page

- [ ] http://localhost:3000 loads the landing page
- [ ] Hero section reflects full platform (not just hours)
- [ ] Features section shows all 4 main features
- [ ] Security page at /beveiliging reflects new architecture (passkeys, PostgreSQL, Redis, encryption)
- [ ] No "geen database" claims on the security page
- [ ] Disclaimer page loads at /disclaimer
- [ ] All navigation links work
- [ ] "Direct beginnen" / "Inloggen" links go to /app/

---

## Test Result Summary

| Area | Pass | Fail | Notes |
|------|------|------|-------|
| Infrastructure | | | |
| Registration | | | |
| Onboarding | | | |
| Dashboard | | | |
| Navigation | | | |
| e-Boekhouden Connect | | | |
| Hour Entry | | | |
| Bank Statements | | | |
| Invoice Processing | | | |
| Settings | | | |
| Session Management | | | |
| Security | | | |
| Accessibility | | | |
| Landing Page | | | |

Tester: _________________ Date: _________________
