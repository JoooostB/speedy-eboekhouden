# Test plan

Manual acceptance checklist. Run it against a local stack (`cp .env.example .env`, then `docker compose up --build`). The Go and vitest suites cover the rules; this list covers what needs a browser or a real e-boekhouden administration.

## Prerequisites

- [ ] Docker with Compose v2 running
- [ ] An e-boekhouden.nl account with at least one employee, project and activity, and some unprocessed bank lines
- [ ] (Optional) An Anthropic API key for the AI checks
- [ ] A browser with passkey support (Chrome, Safari, Firefox)
- [ ] For section 14: the administration's Mail & herken address (e-Boekhouden → Digitaal archief → Mail & herken, e.g. `1234567@e-Boekhouden.nl`, the address without "herken"), a PDF invoice whose payment is an unprocessed bank line, a JPG receipt, and two junk attachments (a small logo PNG, a terms-and-conditions PDF)

> Port 3000 taken? Use a compose override instead of editing `docker-compose.yml`:
> `printf 'services:\n  landing:\n    ports: !override ["3100:80"]\n' > /tmp/compose.port.yml && docker compose -f docker-compose.yml -f /tmp/compose.port.yml up -d --build`, then open http://localhost:3100/app/ and run `./test/smoke.sh http://localhost:3100`.

## 1. Infrastructure

- [ ] `docker compose up --build` starts all 5 services without errors
- [ ] Landing page loads at http://localhost:3000
- [ ] App loads at http://localhost:3000/app/
- [ ] `curl http://localhost:8080/healthz` returns `{"status":"ok"}`
- [ ] PostgreSQL: `docker compose exec postgres psql -U speedy -c '\dt'` shows tables
- [ ] Redis: `docker compose exec redis redis-cli ping` returns PONG

## 2. Registration

- [ ] Open http://localhost:3000/app/: the login screen shows the heading "Speedy e-Boekhouden", "Welkom terug" and the button "Inloggen"
- [ ] Click "Maak er een aan": the form "Account aanmaken" with "E-mailadres" and "Naam" appears
- [ ] Fill in both and click "Registreren": the browser's passkey prompt appears (Touch ID / Face ID / security key)
- [ ] After the passkey is created you land in the inbox: greeting "Goedemorgen/Goedemiddag/Goedenavond, {voornaam}", the alert "Verbind met e-Boekhouden" with "Verbinden", and the chip "Niet gekoppeld"
- [ ] User and team exist: `docker compose exec postgres psql -U speedy -c 'SELECT email, name FROM users'`

## 3. Navigation

- [ ] The app bar shows "Speedy", the tabs "Inbox" and "Uren", the connection chip and a button with your initials
- [ ] Each tab navigates to its page and the active tab has an indicator; on /instellingen no tab is highlighted
- [ ] The chip reads "Gekoppeld" or "Niet gekoppeld"; "Niet gekoppeld" opens the connect dialog, "Gekoppeld" the disconnect confirmation
- [ ] The initials button opens a menu with your name and e-mail, "Instellingen", "e-Boekhouden koppelen" or "Verbinding met e-Boekhouden verbreken", and "Uitloggen"
- [ ] "Uitloggen" shows the login screen
- [ ] On a narrow viewport the tabs scroll horizontally when they don't fit

## 4. Connecting e-Boekhouden

- [ ] The dialog "Verbinden met e-Boekhouden" opens from the chip "Niet gekoppeld", the menu item "e-Boekhouden koppelen", the inbox alert button "Verbinden" and the button "e-Boekhouden koppelen" under Instellingen
- [ ] Enter the e-boekhouden e-mail and password; Enter submits the form
- [ ] With MFA: "Verificatiecode invoeren" appears with the field "Verificatiecode" and the button "Verifieer"
- [ ] After a successful connection the dialog closes and the chip reads "Gekoppeld"
- [ ] "Annuleren" closes the dialog without connecting

### 4a. Verbinding verbreken

The automated tests cover the handler against a fake e-boekhouden; what e-boekhouden really does with the logout can only be seen with a real account.

- [ ] The chip "Gekoppeld" and the menu item "Verbinding met e-Boekhouden verbreken" open the confirmation; Instellingen → "Verbinding met e-Boekhouden" has the button "Verbinding verbreken"
- [ ] The dialog "Verbinding met e-Boekhouden verbreken?" opens with focus on "Annuleren" and lists what happens: afmelden bij e-Boekhouden, token verwijderd, analyses, suggesties, duplicaatmarkeringen en negeerlijst verwijderd, geleerde boekingen blijven, opnieuw verbinden kan altijd
- [ ] Before confirming, fill Redis: open the inbox with an API key and at least one unprocessed bank line (`inbox:classify:<userId>`), analyse a document (`doc:analysis:*` and `invoice:analyze:v2:*`) and book an invoice (`invoice:submitted:*`)
- [ ] Click "Verbinding verbreken". `POST /api/v1/eboekhouden/disconnect` answers `{"status":"ok","ebLoggedOut":…,"purgedKeys":n}` with `n` > 0 (devtools). Note the value of `ebLoggedOut`: ______
- [ ] Snackbar: "Verbinding verbroken en afgemeld bij e-Boekhouden" when `ebLoggedOut` is true, otherwise "Verbinding in Speedy verbroken. e-Boekhouden laat de sessie zelf verlopen."
- [ ] The chip reads "Niet gekoppeld", the inbox shows the connect prompt, `GET /api/v1/documents` answers 412
- [ ] Redis: `docker compose exec redis redis-cli --scan --pattern '*<userId>*'` prints nothing (session keys carry the session id, not the user id). Other users' keys are untouched
- [ ] PostgreSQL: `user_settings.eb_token_enc` is NULL for this user
- [ ] Live-only: open secure.e-boekhouden.nl in the browser where you were logged in before the disconnect. Record whether that session is logged out too (expected when e-boekhouden sessions are per account, not when they are per cookie): ______
- [ ] A second Speedy session of the same user, opened before the disconnect: with `ebLoggedOut: true` its first bookkeeping call gets 412 and the reconnect prompt; with `ebLoggedOut: false` it keeps working until e-boekhouden ends the session. A new Speedy login after the disconnect shows "Niet gekoppeld"
- [ ] Reconnect: the documents show "Nieuw" again and nothing is analysed until you click "Analyseren"; the backend log has no "Invoice analyze cache HIT" line for them
- [ ] The backend log for the whole flow has at most `Disconnect: …` lines with error classes (a successful disconnect logs nothing); never a token, a name or an e-mail address

## 5. Hours (Uren)

- [ ] Open the tab "Uren"
- [ ] "Medewerkers", "Project" (search works) and "Activiteit" load from e-boekhouden
- [ ] The calendar shows the current month with Dutch day names and marks Dutch public holidays
- [ ] Click days to select them; shift+click selects a range (weekends and holidays included); "Alle werkdagen" adds Monday to Friday minus holidays; "Wis maand" clears the month
- [ ] The submit button reads "Indienen ({n} items)"; hours that already exist trigger "{n} dubbele uren-boekingen gevonden" with "Terug om aan te passen" and "Toch indienen"
- [ ] All entries booked: "Uren ingediend!" with "Nieuwe invoer". Some failed: "Gedeeltelijk ingediend" lists the failed entries with e-boekhouden's reason
- [ ] Not connected: the selectors stay empty and no error is shown (known gap)

## 6. Bank-line inbox (Afschriften)

- [ ] With an API key, opening the inbox classifies the unprocessed bank lines ("Afschriften worden geclassificeerd met AI — dit duurt even bij een nieuwe verbinding...")
- [ ] The filter tabs "Alles", "Auto", "Controleer", "Factuur" and "Handmatig" filter the list
- [ ] A row shows date, amount, description with an indicator and a category chip; expanding it shows "Soort", "Grootboekrekening", "Relatie", "BTW-code", "Omschrijving" and "Verwerken"
- [ ] "Verwerken" opens "Boeking bevestigen"; "Bevestigen" books the line, the snackbar says "Regel geboekt" and the line disappears
- [ ] Batch: tick a few rows (or "Selecteer alle zichtbare items"), click "Goedkeuren", then "Bevestigen" in "{n} boekingen bevestigen"; the snackbar says "{n} regels geboekt"
- [ ] "Vernieuwen" classifies again instead of using the 1-hour cache
- [ ] Without an API key: every line is "Handmatig" and the alert "AI-classificatie is niet actief" offers "Instellen"
- [ ] With no bank lines and no documents left the inbox shows "Inbox zero!"

## 7. Uploading invoices and receipts

- [ ] "Facturen uploaden" accepts PDF, JPG and PNG, several at once; an expanded "Factuur" row has a drop zone too
- [ ] Without an API key the upload fails with "Fout bij {bestand}: Stel eerst een Anthropic API-sleutel in"
- [ ] The dialog "Facturen controleren" shows the toggle "Factuur"/"Bonnetje", the fields "Leverancier", "Relatie (crediteur)", "Factuurnummer", "Datum", the amounts, "BTW-code", "Tegenrekening", "Archiefmap" and "Omschrijving", and a confidence badge; all fields are editable
- [ ] A bank line that matches shows as "Gevonden afschriftregel:" with "Koppelen"
- [ ] "Boeken" books the invoice; the dialog shows "1 van 1 facturen geboekt" and "Sluiten"; after closing, both lists reload
- [ ] With an "Archiefmap" picked, the file is in that folder in e-boekhouden, named `JJJJ-MM-DD_Leverancier_Factuurnummer.pdf` and linked to the mutation. Without a folder and without R2 (the local stack) nothing is archived
- [ ] Upload the same PDF again: the dialog warns "Mogelijk dubbele factuur"

## 8. Settings (Instellingen)

- [ ] Open Instellingen from the account menu; the page heading is "Instellingen"
- [ ] Sections: "Anthropic API-sleutel", "Verbinding met e-Boekhouden", "Type onderneming", "SOAP API-gegevens", "REST API-token", "Account", "Team" (when the session has one), "Passkeys", "Geleerde boekingen"
- [ ] API key:
  - [ ] The chip "Ingesteld" shows when a key exists
  - [ ] A valid key (sk-ant-...) saves with "API-sleutel opgeslagen" and the chip appears
  - [ ] A key that doesn't start with sk-ant- is refused: "Ongeldige sleutel. Een Anthropic API-sleutel begint met sk-ant-"
  - [ ] A well-formed key that Anthropic rejects is not saved: "Ongeldige API-sleutel. Controleer of je de juiste sleutel hebt gekopieerd."
  - [ ] "Verwijderen" removes the key ("API-sleutel verwijderd")
- [ ] Account shows name, e-mail and "Authenticatie: Passkey"; Team shows "Teamnaam"

## 9. Returning login

- [ ] Log out with "Uitloggen"
- [ ] On the login screen click "Inloggen"; the browser's passkey prompt appears
- [ ] Pick your passkey (discoverable credential)
- [ ] You land in the inbox
- [ ] e-Boekhouden is still connected (chip "Gekoppeld"): the stored token is restored, unless you disconnected. If e-boekhouden ended that token meanwhile, the first bookkeeping call gets 412 and the connect prompt

## 10. Session expiry

- [ ] Log in and connect e-boekhouden
- [ ] Stay active for more than 30 minutes (click around every few minutes): you stay logged in, because every API call renews both the Redis TTL and the cookie
- [ ] Then leave the app idle for 30+ minutes, or delete the `session:*` key from Redis
- [ ] The next API call shows the login screen
- [ ] No stale data is visible

## 11. Security checks

- [ ] DevTools → Application → Cookies:
  - [ ] `speedy-session` is HttpOnly and SameSite=Lax
  - [ ] Secure matches the `COOKIE_SECURE` setting
- [ ] `/api/v1/bankstatements` without a session cookie returns 401
- [ ] `/api/v1/bankstatements` with a session but no e-boekhouden connection returns 412
- [ ] CORS: `curl -H "Origin: https://evil.com" -I http://localhost:8080/api/v1/auth/me` has no evil.com in Allow-Origin
- [ ] Rate limiting (only with `COOKIE_SECURE=true`; `.env.example` sets `false`, which turns it off): 21 rapid requests to `/api/v1/auth/login/begin`, the 21st returns 429. `RATE_LIMIT_TEST=1 ./test/smoke.sh` does the same

## 12. Accessibility spot checks

- [ ] The whole login flow works with the keyboard only
- [ ] Screen readers announce loading states (aria-live regions)
- [ ] The inbox, Instellingen and the login screen each have one h1 (the Uren page has none: known gap)
- [ ] Tab navigation has visible focus indicators
- [ ] In the bank-line inbox, a row's description area is focusable and Enter or Space expands it

## 13. Landing pages

- [ ] http://localhost:3000 loads the landing page
- [ ] The hero and the feature cards describe what the app does today: the inbox for bank lines and Mail & herken documents, invoices, hours. No card for something that is not routed or built
- [ ] The security page at /beveiliging covers passkeys, PostgreSQL, Redis and encryption
- [ ] No "geen database" claim on the security page or the disclaimer
- [ ] The security page describes the Redis caches (AI analyses with their TTLs), the stored e-boekhouden token, learned bookings and the Mail & herken data handling; no "niets gecachet" claim
- [ ] The security page says that unprocessed bank lines go to Claude when the inbox loads, and documents only when the user starts an analysis
- [ ] The rate-limit numbers on the security page match `cmd/server/main.go` (20 passkey-login requests / 5 min, 5 registration or recovery requests / 15 min, 10 e-boekhouden login requests / 5 min)
- [ ] The disclaimer loads at /disclaimer
- [ ] All navigation links work
- [ ] "Gratis aan de slag" (home), "Direct beginnen" (security page) and "Inloggen" go to /app/

## 14. Documents (Mail & herken)

End-to-end with a real administration. The Go and vitest suites cover the status rules, naming, matching and the handlers against a fake e-boekhouden; what depends on e-boekhouden's own behaviour (rename conflicts, bulk move, folder header values) can only be confirmed live. Watch the backend log throughout: `docker compose logs -f backend`.

### 14a. Setup and listing

- [ ] Send the PDF invoice, the JPG receipt and the two junk attachments to the Mail & herken address without "herken" (e.g. `1234567@e-Boekhouden.nl`). Check in e-Boekhouden → Digitaal archief that they landed in the mail folder
- [ ] Log in and connect e-boekhouden. The inbox opens on "Afschriften" (on "Documenten" when there are no bank lines); the tab "Documenten" shows the number of files still to handle (not linked, not ignored, not unsupported)
- [ ] Each document shows the file name as title (underscores and dashes turned into spaces), "Ontvangen 12 okt 14:32 · PDF · 84 kB" as subtitle, the chip "Nieuw" and the button "Analyseren"
- [ ] Listing analyses nothing: the rows stay "Nieuw" and no `doc:analysis:*` key appears in Redis
- [ ] The small logo PNG (< 15 kB) shows the chip "Geen factuur?" without being analysed, and "Alles analyseren" skips it
- [ ] A file with another extension (e.g. `.xml`) shows the chip "Niet ondersteund" and no "Analyseren" button
- [ ] `GET /api/v1/inbox/summary` returns `documentsCount` equal to the number of documents that are not ignored, not linked and readable
- [ ] Live-only, no mail folder: on an administration that never set up Mail & herken, the tab shows the card "Facturen automatisch binnenhalen" with "Mailmap instellen" and "Bestaande map kiezen". Check in the browser devtools what `GET /v1/api/folder/0/header` really returns for `menhFolderId` (the code treats `0`, missing and negative as "not configured"); note the value: ______
- [ ] Click "Mailmap instellen": a root folder "Inbox" is created and set. Check in e-Boekhouden → Digitaal archief → Mail & herken that the folder is selected. A second `POST /documents/setup` answers 409 "Er is al een mailmap ingesteld"
- [ ] "Bestaande map kiezen" lets you pick any existing folder ("Deze map gebruiken"); the picker's helper reads "Kies de map waarin Mail & herken je bijlagen zet."
- [ ] With a mail folder but no files: the card "Je mailmap "Inbox" is leeg", with where the mail address lives and the advice to use the address without 'herken'

### 14b. Analysis

- [ ] Click "Alles analyseren ({n})": the progress text "Analyseren {n} van {total}…" and "Stoppen" appear; rows show "In wachtrij" / "Analyseren…"; at most three analyses run at once. With more than 20 documents a confirmation with the estimated cost comes first
- [ ] After the run a snackbar sums it up (e.g. "4 geanalyseerd · 1 met betaling · 2 zonder · 1 geen factuur"); titles read "Leverancier · 12 okt 2026 · € 1.234,56", a receipt "Bonnetje Leverancier · …"
- [ ] The invoice whose payment is an unprocessed bank line moves to "Klaar om te boeken" with the chip "Betaling gevonden", the second line "Betaald 13 okt · € -21,78 · {omschrijving}" and the button "Boeken"
- [ ] The receipt without a bank line shows the chip "Wacht op afschrift"; its "Boeken" is disabled with the tooltip "Bonnetjes boek je samen met de betaling"
- [ ] An invoice without a bank line shows the chip "Geen betaling gevonden", the second line "Wordt geboekt als openstaande factuur" and "Boeken" (open booking, 14d)
- [ ] A credit note cannot be booked: "Boeken" is disabled with the tooltip "Creditnota's boek je voorlopig in e-Boekhouden zelf"
- [ ] The terms-and-conditions PDF is analysed as "overig": chip "Geen factuur?", the title stays the file name, the second line is Claude's reason and the file name, the button is "Negeren"
- [ ] ⋮ → "Opnieuw analyseren" calls Claude again (no "Invoice analyze cache HIT" line for it) and replaces the cached result
- [ ] A document analysed in another session comes from the cache: no Claude call, same title
- [ ] A non-EUR invoice (e.g. CHF): the title shows "CHF 32,80"; in the review dialog the linked bank line carries "(geschatte koers — controleer bedrag)"; the booking uses the bank line's real EUR amount
- [ ] In "Afschriften" the paying bank line shows the chip "Document gevonden" and the indicator "Factuur {titel} staat in je mailmap"; expanding the row shows "Factuur koppelen en boeken". Both tabs point at the same pair (the assignment is global)
- [ ] Redis: `docker compose exec redis redis-cli --scan --pattern 'doc:*'` lists `doc:analysis:v2:<user>:<file>` with a TTL of about 30 days (`TTL <key>`) and no leftover `doc:analyzing:*`. Each analysis also writes `invoice:analyze:v2:<user>:<hash>:<accounts>` (7 days)

### 14c. Booking a matched document (rename and move)

- [ ] Click "Boeken" on the matched invoice. The review dialog opens with the fields filled in, the bank line under "Gekoppeld aan afschriftregel:" and the picker "Verplaatsen naar". Without a supplier folder the helper reads "Geen map gekozen: Facturen/2026/10 Oktober. Het bestand gaat uit je mailmap Inbox." (your folder name)
- [ ] Click "Boeken". The dialog shows "1 van 1 facturen geboekt" and a line ending in "Geboekt · verplaatst naar Facturen / 2026 / 10 Oktober"
- [ ] In e-Boekhouden: a FactuurOntvangen and a FactuurbetalingVerstuurd mutation exist, the bank line is processed, the file is linked to the invoice mutation, renamed to `JJJJ-MM-DD_Leverancier_Factuurnummer.pdf`, sits in the target folder and is gone from the mail folder
- [ ] Naming contract on a real file: legal form stripped ("Voorbeeld Leverancier B.V." → `Voorbeeld-Leverancier`), diacritics transliterated, `/` in the invoice number → `-`, only `[A-Za-z0-9._-]`, lowercase `pdf`/`jpg`/`png`
- [ ] Live-only, name conflict: first put a file with exactly the expected archive name in the target folder, then book. The booked file becomes `…_2.pdf` (counter before the extension) and nothing is overwritten
- [ ] Live-only, explicit target: pick a folder in "Verplaatsen naar"; the file lands there with the same id as before (links intact). The picker never offers the mail folder or its subfolders; through the API such a target is refused with 400 ("De mailmap kan geen doelmap zijn" / "Een submap van de mailmap kan geen doelmap zijn")
- [ ] Live-only, supplier folder: when the supplier already has a folder in the archive, the picker preselects it
- [ ] Book a receipt (JPG) against its bank line: one "Geld uitgegeven" mutation; the file is renamed `JJJJ-MM-DD_Leverancier_bonnetje.jpg` and moved

### 14d. Booking without a match, then linking the payment

- [ ] Click "Boeken" on an invoice with "Geen betaling gevonden". The dialog shows the alert "Nog geen betaling gevonden. We boeken dit als openstaande factuur. Zodra de betaling op je afschrift staat, stelt Speedy voor om hem te koppelen." and the button reads "Boeken als openstaand"
- [ ] Result line "Geboekt als openstaande factuur · verplaatst naar …"; in e-Boekhouden only a FactuurOntvangen exists; the file is renamed and moved
- [ ] Expand "Wacht op betaling" (open posts load on the first expand, once per page load): it lists the invoice from the open-post grid (soort Crediteur) with amount, relation and "Factuur … · vervalt …", or "vervallen op …" when overdue
- [ ] Once the payment is on the bank statement, the line in "Afschriften" shows "Betaalt open factuur" with the indicator "Leverancier · INV-0042 · geboekt 12 okt" and a "Koppelen" button
- [ ] Click "Koppelen", then "Bevestigen" in "Boeking bevestigen": the snackbar says "Regel geboekt", the open post is settled (FactuurbetalingVerstuurd with relatieId + factuur, no tegenrekening) and the bank line is processed. After a page reload the invoice is gone from "Wacht op betaling"
- [ ] An incoming payment of an outstanding sales invoice (Debiteur, created in e-Boekhouden) gets the same suggestion and is booked as FactuurbetalingOntvangen ("Wacht op betaling" lists Crediteur posts only)
- [ ] Two open posts with the same amount and no supplier name in the description: no suggestion (the matcher refuses ambiguous matches)

### 14e. Ignore, undo, delete

- [ ] ⋮ → "Negeren" on a junk attachment: snackbar "Genegeerd" with "Ongedaan maken"; the row disappears
- [ ] "Ongedaan maken" brings the row back; ignore it again
- [ ] "Toon genegeerd ({n})" at the bottom of the list shows the section "Genegeerd ({n})" with "Niet meer negeren"
- [ ] Nothing changed in e-Boekhouden; Redis `SMEMBERS doc:ignored:<userId>` holds only the file id (no name, no content)
- [ ] ⋮ → "Verwijderen uit e-Boekhouden" on the second junk attachment: a confirmation dialog, then "Verwijderen"; snackbar "Verwijderd uit e-Boekhouden"; the file is gone from the archive in e-Boekhouden
- [ ] A file linked to a booking has no "Verwijderen uit e-Boekhouden" in ⋮; `DELETE /api/v1/documents/:id` on it answers 409 "Dit document is gekoppeld aan een boeking"

### 14f. Already processed in e-Boekhouden

- [ ] Link a mail-folder file to a booking in e-Boekhouden itself. In Speedy it appears under "Al verwerkt in e-Boekhouden" with the chip "Al gekoppeld"; "Alles analyseren" skips it
- [ ] "Alles opruimen" (or the row's "Opruimen") renames and moves it; snackbar "1 document opgeruimd". The link is intact (same file id, still attached to the mutation). Without a target the file goes to `Facturen/<jaar>/<maand>` for the invoice date, or the received date when it was not analysed
- [ ] ⋮ → "Verplaatsen naar…" opens the dialog with "Doelmap" and the helper "Het bestand krijgt een archiefnaam en verlaat je mailmap."; an unanalysed file gets `<ontvangstdatum>_<bestandsnaam>_document.<ext>`

### 14g. Duplicates, errors, session expiry

- [ ] Re-send an already booked PDF invoice to the mail address. After analysis: chip "Mogelijk dubbel", listed under "Te doen", "Toch boeken" in ⋮. (Receipts get no duplicate marker.)
- [ ] Without an API key: the alert "Geen API-sleutel ingesteld" and disabled "Analyseren" and "Alles analyseren" buttons; the list itself still loads
- [ ] Let the e-boekhouden session expire (or end it in e-Boekhouden) and click "Analyseren": the reconnect prompt appears (412 `eboekhouden_session_expired`), not a bare 502
- [ ] Start two analyses of the same file at once (two tabs): the second is refused with "Wordt al geanalyseerd" and the UI tries three times in total, 1.5 s apart. If the first analysis finishes in that time the row shows its result; otherwise the chip "Analyse mislukt" and "Opnieuw"

### 14h. Custom mail folder name and nested path

- [ ] Live-only: in e-Boekhouden set Mail & herken to a nested folder with a custom name (e.g. `Administratie / Post / Binnenkomend`). Speedy shows "Je mailmap "Binnenkomend" is leeg" when it is empty and lists its files otherwise; analysing, booking, moving and cleaning up work the same; the folder and its subfolders are never offered as a target
- [ ] Live-only: delete the configured mail folder in e-Boekhouden. Speedy shows the card with the warning "Je mailmap bestaat niet meer. Stel een nieuwe mailmap in om verder te gaan."; "Mailmap instellen" works even though `menhFolderId` is still non-zero
- [ ] Live-only: a subfolder inside the mail folder (soort "D") is not listed as a document

### 14i. Logs and data handling

- [ ] After the run, grep the backend log for the supplier name, the file name and an amount: `docker compose logs backend | grep -i -e "<leverancier>" -e "<bestandsnaam>" -e "<bedrag>"` gives no hits
- [ ] Rename and move failures log only ids and error classes
- [ ] With R2 configured: no objects other than avatars appear in the bucket

## Test result summary

| Area | Pass | Fail | Notes |
|------|------|------|-------|
| Infrastructure | | | |
| Registration | | | |
| Navigation | | | |
| Connecting e-Boekhouden | | | |
| Hours | | | |
| Bank-line inbox | | | |
| Uploading invoices | | | |
| Settings | | | |
| Returning login | | | |
| Session expiry | | | |
| Security | | | |
| Accessibility | | | |
| Landing pages | | | |
| Documents (Mail & herken) | | | |

Tester: _________________ Date: _________________
