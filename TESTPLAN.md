# Test Plan — Speedy e-Boekhouden

Manual acceptance test plan covering all features. Run through this after `docker-compose up --build`.

## Prerequisites

- [ ] Docker + Docker Compose running
- [ ] Access to an e-boekhouden.nl account with at least one employee, project, and activity
- [ ] (Optional) Anthropic API key for AI features
- [ ] Modern browser with passkey support (Chrome, Safari, Firefox)
- [ ] For section 16: the administration's Mail & herken address (e-Boekhouden → Digitaal archief → Mail & herken, e.g. `1234567@e-Boekhouden.nl`, the address **without** "herken"), a PDF invoice whose payment is an unprocessed bank line, a JPG receipt, and two junk attachments (a small logo PNG, a terms-and-conditions PDF)

> **Port 3000 taken?** Use a compose override instead of editing `docker-compose.yml`:
> `printf 'services:\n  landing:\n    ports: !override ["3100:80"]\n' > /tmp/compose.port.yml && docker compose -f docker-compose.yml -f /tmp/compose.port.yml up -d --build`, then read the app at http://localhost:3100/app/ and run `./test/smoke.sh http://localhost:3100`.

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

> **Outdated:** the dashboard is no longer routed (`App.tsx` serves the bookkeeping inbox at `/`). `Dashboard.tsx` still exists but cannot be reached from the UI. Kept for reference; skip these checks.

- [ ] Dashboard shows 4 cards: Afschriften, Facturen, Uren, API-sleutel
- [ ] If e-boekhouden connected: afschriften card shows unprocessed count (number)
- [ ] If not connected: warning banner shows "Verbind met e-Boekhouden"
- [ ] If no API key: info banner shows "Stel een Anthropic API-sleutel in"
- [ ] Clicking each card navigates to the correct page
- [ ] API key card shows "Geconfigureerd" or "Niet ingesteld" correctly

## 5. Navigation

> **Partly outdated:** the app bar now has the tabs "Inbox" (`/`) and "Uren" (`/uren`) only; the Dashboard, Afschriften and Facturen tabs no longer exist. Bank lines and documents live in the inbox tabs "Afschriften" and "Documenten" (section 16). The remaining checks still apply.

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

### 6a. Verbinding verbreken (disconnect)

Live-only: the automated tests cover the handler against a fake e-boekhouden; what e-boekhouden really does with the logout can only be seen with a real account.

- [ ] While connected, the chip "Gekoppeld" and the user menu offer "Verbinding met e-Boekhouden verbreken"; Instellingen shows the section "Verbinding met e-Boekhouden" with the same button
- [ ] The confirmation dialog opens with focus on "Annuleren" and lists: afmelden bij e-Boekhouden, token verwijderd, tijdelijk opgeslagen analyses/suggesties/duplicaatmarkeringen/negeerlijst verwijderd, geleerde boekingen blijven, opnieuw verbinden kan altijd
- [ ] Before confirming: analyse at least one document and classify the inbox, so Redis holds `doc:analysis:*`, `inbox:classify:*` and (after a booking) `invoice:submitted:*` keys for your user id
- [ ] Confirm. `POST /api/v1/eboekhouden/disconnect` answers `{"status":"ok","ebLoggedOut":…,"purgedKeys":n}` with `n` > 0 (devtools). Note the value of `ebLoggedOut` here: ______
- [ ] Snackbar: "Verbinding verbroken en afgemeld bij e-Boekhouden" when `ebLoggedOut` is true, otherwise "Verbinding in Speedy verbroken. e-Boekhouden laat de sessie zelf verlopen."
- [ ] The chip reads "Niet gekoppeld", the inbox shows the connect prompt, `GET /api/v1/documents` answers 412
- [ ] Redis: `docker compose exec redis redis-cli --scan --pattern '*<userId>*'` lists only `session:*` (if any); no `inbox:classify`, `invoice:*` or `doc:*` keys for this user. Other users' keys are untouched
- [ ] PostgreSQL: `user_settings.eb_token_enc` is NULL for this user
- [ ] **Live-only:** open secure.e-boekhouden.nl in the browser where you were logged in before the disconnect. Record whether that session is also logged out (expected when e-boekhouden sessions are per account; not when they are per cookie): ______
- [ ] A Speedy login in another browser (second session of the same user) no longer has a working e-boekhouden connection: the first bookkeeping call gets 412 and the reconnect prompt
- [ ] Reconnect: the document list loads again and is analysed afresh (no cache hits in the backend log)
- [ ] Backend log for the whole flow shows only `Disconnect: …` lines with error classes at most; never a token, a name or an e-mail address

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

> **Outdated:** `/afschriften` is no longer routed. Unprocessed bank lines are handled in the inbox tab "Afschriften" at `/` (classification, batch approval, "Betaalt open factuur" → "Koppelen", "Document gevonden" → "Factuur koppelen en boeken"; see section 16). Kept for reference; skip these checks.

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

> **Outdated:** `/facturen` is no longer routed. Invoices are uploaded from the inbox ("Afschriften" tab, upload zone on an invoice line or the upload button) or arrive through the mail folder (section 16); both open the same review dialog, which now also accepts JPG and PNG. Kept for reference; skip these checks.

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
- [ ] No "geen database" claims on the security page or the disclaimer
- [ ] Security page describes the Redis caches (AI analyses with TTL), the stored e-boekhouden token, learned bookings, and the Mail & herken data handling; no "niets gecachet" claim
- [ ] Rate-limit numbers on the security page match `cmd/server/main.go` (20 passkey logins / 5 min, 5 registrations or recoveries / 15 min, 10 e-boekhouden logins / 5 min)
- [ ] Disclaimer page loads at /disclaimer
- [ ] All navigation links work
- [ ] "Direct beginnen" / "Inloggen" links go to /app/

## 16. Documents (Mail & herken)

End-to-end with a real administration. Automated tests (`go test`, vitest) cover the status rules, naming, matching and the handlers against a fake e-boekhouden; everything below that touches e-boekhouden's own behaviour (rename conflicts, bulk move, folder header values) can only be confirmed live. Watch the backend log throughout: `docker compose logs -f backend`.

### 16a. Setup and listing

- [ ] Send the PDF invoice, the JPG receipt and the two junk attachments to the Mail & herken address **without** "herken" (e.g. `1234567@e-Boekhouden.nl`). Confirm in e-Boekhouden → Digitaal archief that they landed in the mail folder
- [ ] Log in, connect e-boekhouden. The inbox opens; the tab "Documenten" shows a count pill with the number of files
- [ ] Each document shows the file name as title (underscores and dashes turned into spaces) and "Ontvangen 12 okt 14:32 · PDF · 84 kB" as subtitle, chip "Nieuw", button "Analyseer"
- [ ] Backend log contains **no** Claude call for the listing (no "Invoice analyze", no "Claude" lines) — listing never costs tokens
- [ ] The small logo PNG (< 15 kB) shows chip "Geen factuur?" without having been analysed and is skipped by "Alles analyseren"
- [ ] A file with another extension (e.g. `.xml`) shows chip "Niet ondersteund" and no "Analyseer" button
- [ ] `GET /api/v1/inbox/summary` returns `documentsCount` equal to the number of documents that are not ignored, not linked and readable
- [ ] **Live-only — no mail folder:** on an administration that never set Mail & herken, the tab shows the card "Facturen automatisch binnenhalen" with "Mailmap instellen" and "Bestaande map kiezen". Check with the browser devtools what `GET /v1/api/folder/0/header` really returns for `menhFolderId` (the code treats `0`, missing and negative as "not configured"); note the real value here: ______
- [ ] Click "Mailmap instellen": a root folder "Inbox" is created and set. Verify in e-Boekhouden → Digitaal archief → Mail & herken that the folder is selected. Clicking it twice (via the API) answers 409 "Er is al een mailmap ingesteld"
- [ ] "Bestaande map kiezen" lets you pick any existing folder ("Deze map gebruiken"); the picker helper reads "Kies de map waarin Mail & herken je bijlagen zet."
- [ ] With a mail folder but no files: card "Je mailmap "Inbox" is leeg" with the hint where the mail address lives and to use the address without 'herken'

### 16b. Analysis

- [ ] Click "Alles analyseren": a progress bar "Voortgang van de analyse" with "Stoppen" appears; rows show "In wachtrij" / "Analyseren…"; at most three analyses run at once; with more than 20 documents a confirmation with the estimated cost appears first
- [ ] After the run: a closing summary snackbar; analysed titles read "Leverancier · 12 okt 2026 · € 1.234,56", subtitles "Factuur INV-0042 · bestand.pdf"; a receipt reads "Bonnetje Leverancier · …" / "Bonnetje · bon.jpg"
- [ ] The invoice whose payment is an unprocessed bank line moves to "Klaar om te boeken" with chip "Betaling gevonden" and button "Boek"
- [ ] The receipt without a bank line shows chip "Wacht op afschrift" (status `receipt_waiting`)
- [ ] An invoice without a bank line shows chip "Geen betaling gevonden" and button "Boek" (open booking, 16d)
- [ ] The terms-and-conditions PDF is analysed as "overig": chip "Geen factuur?", title stays the file name, subtitle is Claude's reason
- [ ] "Analyseer opnieuw" (⋮) really calls Claude again (log shows a fresh analysis, no cache HIT) and replaces the cached result
- [ ] A document analysed in another session is served from cache: no Claude call, same title
- [ ] A non-EUR invoice (e.g. CHF): title shows "CHF 32,80", subtitle adds "≈ € 34,44"; the matched bank line is marked as currency-converted in the dialog; the booking uses the bank line's real EUR amount
- [ ] Switch to "Afschriften": the paying bank line shows the chip "Document gevonden" and, instead of the upload zone, "Factuur koppelen en boeken" with the document title. Both tabs point at the same pair (the assignment is global)
- [ ] Redis: `docker compose exec redis redis-cli --scan --pattern 'doc:*'` lists `doc:analysis:v2:<user>:<file>` with a TTL of about 30 days (`TTL <key>`), no leftover `doc:analyzing:*` after the run, and no `invoice:analyze:*` upload key for mail-folder files when R2 is configured (no `uploads/` object was created)

### 16c. Booking a matched document (rename + move)

- [ ] Click "Boek" on the matched invoice. The review dialog opens with the fields prefilled, the bank line preselected, a "Verplaatsen naar" picker with helper text "Geen map gekozen: Facturen/2026/10 Oktober. Het bestand gaat uit je mailmap Inbox." (your folder name)
- [ ] Click "Boeken". Result line: "Geboekt · verplaatst naar Facturen / 2026 / 10 Oktober"
- [ ] Verify in e-Boekhouden: a FactuurOntvangen and a FactuurbetalingVerstuurd mutation exist, the bank line is processed, the file is linked to the invoice mutation, renamed to `JJJJ-MM-DD_Leverancier_Factuurnummer.pdf` and sits in the target folder; it is gone from the mail folder
- [ ] Naming contract on a real file: legal form stripped ("Voorbeeld Leverancier B.V." → `Voorbeeld-Leverancier`), diacritics transliterated, `/` in the invoice number → `-`, only `[A-Za-z0-9._-]`, lowercase `pdf`/`jpg`/`png`
- [ ] **Live-only — name conflict:** first put a file with exactly the expected archive name in the target folder, then book. The booked file becomes `…_2.pdf` (counter before the extension), nothing is overwritten
- [ ] **Live-only — explicit target:** pick a folder in "Verplaatsen naar"; the file lands there with the same id as before (links intact). Picking the mail folder or one of its subfolders is refused ("De mailmap kan geen doelmap zijn" / "Een submap van de mailmap kan geen doelmap zijn")
- [ ] **Live-only — supplier folder default:** when the supplier already has a folder in the archive, the picker defaults to it
- [ ] Book a receipt (JPG) against its bank line: one "Geld uitgegeven" mutation, the file is renamed `JJJJ-MM-DD_Leverancier_bonnetje.jpg` and moved

### 16d. Booking without a match, then linking the payment later

- [ ] Click "Boek" on an invoice with "Geen betaling gevonden". The dialog shows the alert "Nog geen betaling gevonden. We boeken dit als openstaande factuur. Zodra de betaling op je afschrift staat, stelt Speedy voor om hem te koppelen." and the button reads "Boeken als openstaand"
- [ ] Result line "Geboekt als openstaande factuur · verplaatst naar …"; in e-Boekhouden only a FactuurOntvangen exists; the file is renamed and moved
- [ ] The section "Wacht op betaling" lists the invoice (from the open-post grid, soort Crediteur) with amount, "Factuur …" and "vervalt …"/"vervallen op …"
- [ ] Once the payment is on the bank statement: in "Afschriften" the line shows "Betaalt open factuur" with the indicator "Leverancier · INV-0042 · geboekt 12 okt" and a "Koppelen" button
- [ ] Click "Koppelen": the open post is settled (FactuurbetalingVerstuurd with relatieId + factuur, no tegenrekening) and the bank line is processed. The invoice disappears from "Wacht op betaling"
- [ ] An incoming payment on an outstanding sales invoice (Debiteur) behaves the same with FactuurbetalingOntvangen
- [ ] Two open posts with the same amount and no supplier name in the description: no suggestion (the matcher refuses ambiguous matches)

### 16e. Ignore, undo, delete

- [ ] ⋮ → "Negeren" on a junk attachment: snackbar "Genegeerd" with "Ongedaan maken"; the row disappears
- [ ] "Ongedaan maken": the row is back; ignore it again
- [ ] "Toon genegeerd" (bottom of the list) reveals the section "Genegeerde documenten" with "Niet meer negeren"
- [ ] Nothing changed in e-Boekhouden; Redis `SMEMBERS doc:ignored:<userId>` holds only the file id (no name, no content)
- [ ] ⋮ → "Verwijderen uit e-Boekhouden" on the second junk attachment: a confirmation dialog, then "Verwijderen"; snackbar "Verwijderd uit e-Boekhouden"; the file is gone from the archive in e-Boekhouden
- [ ] Deleting a file that is linked to a booking is refused (409 "Dit document is gekoppeld aan een boeking")

### 16f. Already processed in e-Boekhouden

- [ ] Link a mail-folder file to a booking in e-Boekhouden itself. In Speedy it appears under "Al verwerkt in e-Boekhouden" with chip "Al gekoppeld"; "Alles analyseren" skips it
- [ ] "Alles opruimen" (or ⋮ → "Ruim op") renames and moves it. The link is intact (same file id, still attached to the mutation)
- [ ] ⋮ → "Verplaatsen naar…" opens the dialog with "Doelmap" and the helper "Het bestand krijgt een archiefnaam en verlaat je mailmap."; an unanalysed file gets `ontvangstdatum_bestandsnaam_document.pdf`

### 16g. Duplicates, errors, session expiry

- [ ] Re-send an already booked PDF to the mail address. After analysis: chip "Mogelijk dubbel", listed under "Te doen", "Toch boeken" in ⋮
- [ ] Without an API key: the alert "Geen API-sleutel ingesteld" and disabled "Analyseer" buttons; the list itself still loads
- [ ] Let the e-boekhouden session expire (or invalidate it in e-Boekhouden) and click "Analyseer": the reconnect prompt appears (412 `eboekhouden_session_expired`), not a bare 502
- [ ] Start two analyses of the same file at once (two tabs): the second is refused with "Wordt al geanalyseerd" and the UI retries up to three times

### 16h. Custom mail folder name and nested path

- [ ] **Live-only:** in e-Boekhouden set Mail & herken to a nested folder with a custom name (e.g. `Administratie / Post / Binnenkomend`). Speedy shows "Je mailmap "Binnenkomend" is leeg" when empty and lists its files otherwise; analysing, booking, moving and cleaning up work the same; the folder and its subfolders are never offered as target
- [ ] **Live-only:** delete the configured mail folder in e-Boekhouden. Speedy shows the card with the warning "Je mailmap bestaat niet meer. Stel een nieuwe mailmap in om verder te gaan."; "Mailmap instellen" works even though `menhFolderId` is still non-zero
- [ ] **Live-only:** a subfolder inside the mail folder (soort "D") is not listed as a document

### 16i. Logs and data handling

- [ ] After the run, grep the backend log for the supplier name, the file name and an amount: `docker compose logs backend | grep -i -e "<leverancier>" -e "<bestandsnaam>" -e "<bedrag>"` — no hits
- [ ] Only ids and error classes are logged for rename/move failures
- [ ] When R2 is configured: no new object under `uploads/` for mail-folder analyses (only `/invoices/analyze` uploads create them)

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
| Documents (Mail & herken) | | | |

Tester: _________________ Date: _________________
