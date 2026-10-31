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

- [ ] The dialog "e-Boekhouden koppelen" opens from the chip "Niet gekoppeld", the menu item "e-Boekhouden koppelen", the inbox alert button "e-Boekhouden koppelen" and the button "e-Boekhouden koppelen" under Instellingen
- [ ] Enter the e-boekhouden e-mail and password; Enter submits the form
- [ ] With MFA: "Verificatiecode invoeren" appears with the field "Verificatiecode" and the button "Verifieer"
- [ ] After a successful connection the dialog closes and the chip reads "Gekoppeld"
- [ ] "Annuleren" closes the dialog without connecting
- [ ] Live-only: a wrong password shows "Inloggen bij e-Boekhouden is mislukt. Controleer je e-mailadres en wachtwoord en probeer het opnieuw.", not the new-IP message
- [ ] Live-only: the first login from an IP address e-boekhouden does not know yet (a fresh deployment) shows "e-Boekhouden blokkeert deze aanmelding, omdat die van een onbekend IP-adres komt…". Speedy recognises the block page only by the word "IP" in its visible text (`loginBlockedForNewIP`); if the wrong-password message shows instead, note the block page's visible wording here, without personal data: ______

### 4a. Verbinding verbreken

The automated tests cover the handler against a fake e-boekhouden; what e-boekhouden really does with the logout can only be seen with a real account.

- [ ] The chip "Gekoppeld" and the menu item "Verbinding met e-Boekhouden verbreken" open the confirmation; Instellingen → "Verbinding met e-Boekhouden" has the button "Verbinding verbreken"
- [ ] The dialog "Verbinding met e-Boekhouden verbreken?" opens with focus on "Annuleren" and lists what happens: afmelden bij e-Boekhouden, token verwijderd, analyses, suggesties, duplicaatmarkeringen en negeerlijst verwijderd, geleerde boekingen blijven, opnieuw verbinden kan altijd
- [ ] Before confirming, fill Redis: open the inbox with an API key and at least one unprocessed bank line (`inbox:classify:<userId>`), analyse a document (`doc:analysis:*` and `invoice:analyze:v3:*`) and book an invoice (`invoice:submitted:*`)
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
- [ ] Click days to select them; shift+click selects a range (weekends and holidays included); "Alle werkdagen" adds Monday to Friday minus holidays; "Maand wissen" clears the month
- [ ] The submit button reads "Indienen ({n} regels)"; hours that already exist trigger "{n} dubbele urenboekingen gevonden" with "Selectie aanpassen" and "Toch indienen"
- [ ] All entries booked: "Uren ingediend" with "Nieuwe invoer". Some failed: "Gedeeltelijk ingediend" lists the failed entries with e-boekhouden's reason
- [ ] Not connected: the selectors stay empty and no error is shown (known gap)

## 6. Bank-line inbox (Afschriften)

- [ ] With an API key, opening the inbox classifies the unprocessed bank lines (placeholder rows while it loads)
- [ ] The filter tabs "Alles", "Auto", "Controleer", "Factuur" and "Handmatig" filter the list
- [ ] A row shows date, amount, description with an indicator and a category chip; expanding it shows "Soort", "Grootboekrekening", "Relatie", "BTW-code", "Omschrijving" and "Verwerken"
- [ ] "Verwerken" opens "Boeking bevestigen"; "Bevestigen" books the line, the snackbar says "Regel geboekt" and the line disappears
- [ ] Batch: tick a few rows (or "Selecteer alle zichtbare regels"), click "Goedkeuren", then "Bevestigen" in "{n} boekingen bevestigen"; the snackbar says "{n} regels geboekt"
- [ ] "Vernieuwen" classifies again instead of using the 1-hour cache
- [ ] Without an API key: every line is "Handmatig" and the alert "Geen boekingsvoorstellen zonder API-sleutel" offers "Instellen"
- [ ] With no bank lines and no documents left the inbox shows "Inbox zero!"

### 6a. Payments in parts (debiteuren, WKA)

Needs a sales invoice in e-Boekhouden that a customer paid in parts, for example € 12.000 excl + € 2.520 btw = € 14.520 received as € 10.800 on the bank account and € 1.200 + € 2.520 on the G-rekening, with the invoice number in the descriptions.

- [ ] The three lines are not loose rows but one row with the chip "Betaling in delen", "{relatie} · factuur {nr}" and "3 betalingen · € 14.520,00 van € 14.520,00 · factuur volledig betaald"; the sub-rows show date, amount, own account ("1022 G-rekening") and description. The hover text of the chip explains the group
- [ ] "Koppelen (3)" opens "Betaling in delen koppelen" with the invoice block (Relatie, Factuur, Factuurdatum, Factuurbedrag, Al betaald, Nog open) and the summary "Samen € 14.520,00 van € 14.520,00 open. Na koppelen is de factuur volledig betaald."; "3 betalingen koppelen" books three mutations in e-Boekhouden (Boekhouden → Overzichten → Mutaties: three factuurbetalingen on the invoice), the snackbar says "3 betalingen gekoppeld aan factuur {nr}. De factuur is volledig betaald." and the post is gone from Openstaande posten
- [ ] Untick one line in the dialog: the summary turns into "… Na koppelen blijft € 3.720,00 open." and the button reads "2 betalingen koppelen"; after booking the post remains with `teOntvangen` € 3.720,00, and the next classify shows the remaining line as "Betaalt open factuur" (single match) or as "Deelbetaling"
- [ ] Add a line that pays too much ("Betaling toevoegen"): the summary reads "Samen …, dat is … meer dan er openstaat. Haal een betaling weg of kies een andere factuur." and the primary button is disabled
- [ ] Compose by hand: tick two incoming lines, "Koppelen aan één factuur" in the batch bar, pick the invoice in "Openstaande verkoopfactuur"; with an outgoing line in the selection the button is disabled with "Alleen ontvangsten kun je samen op één verkoopfactuur koppelen."
- [ ] From a single row: expand an incoming line, "Hoort bij een openstaande factuur" opens the dialog with that line
- [ ] Two open invoices of the same customer with the same amount and no invoice number in the descriptions: chip "Kies factuur", no checkbox, "Kiezen" opens "Kies de factuur" with a radio per invoice ("Past precies" / "Past deels"), nothing preselected, the button disabled with "Kies eerst een factuur."
- [ ] Overpayment guard: book part of the invoice in e-Boekhouden by hand, then "Koppelen (3)" in Speedy without refreshing: every line answers "De factuur is in e-Boekhouden veranderd: er staat nu € … open. Controleer de betalingen en probeer het opnieuw." and nothing is booked (no new mutations in e-Boekhouden)
- [ ] Partial failure: with one of the three lines already processed in e-Boekhouden, the dialog stays open, the lines show "Geboekt" / "Niet geboekt: {reden}", the alert reads "2 van de 3 betalingen zijn geboekt. 1 niet: … De geboekte betalingen blijven staan." and the button reads "Opnieuw proberen (1)"
- [ ] On a phone the group row is collapsed by default and the dialog is full screen
- [ ] Logs: only line ids, mutNrs, statuses and error classes; no company names, amounts or descriptions

### 6b. Verification payments (Bijzondere baten)

Needs a card check in the bank statement: a web shop charged −€ 1,00 on the betaalpas and refunded +€ 1,00 under the same reference (`eCom, Betaalpas … P1234567890 …`), plus optionally a €0,01 IBAN check and a real card subscription of the same merchant.

- [ ] The −€ 1,00 and +€ 1,00 are not loose rows but one row with the chip "Kaartcontrole", the description, "−€ 1,00 en +€ 1,00 heffen elkaar op · boeken op 9300 Bijzondere baten" (the account's real name) and "€ 0,00"; the sub-rows show date, amount and description. The chip's hover text ("Een webwinkel schrijft een klein bedrag af …") is reachable with Tab (focus ring, read out as the chip's description). On a phone the "Boeken (2)" button wraps under the description. The pair also shows under the "Auto" filter tab with both legs
- [ ] "Boeken (2)" opens "2 boekingen bevestigen" with both lines on tegenrekening "9300 - Bijzondere baten", BTW "GEEN" and the line "Controlebetaling: geen factuur nodig."; no "Mogelijke terugbetaling" hint for the +€ 1,00; "Bevestigen" books two mutations (Geld uitgegeven and Geld ontvangen on 9300), the snackbar says "2 regels geboekt" and both lines disappear
- [ ] A single leg (only the −€ 1,00 or only the +€ 1,00 in the inbox) is a loose row with the chip "Kaartcontrole", the caption "Controlebetaling zonder terugboeking in de inbox · boeken op 9300 Bijzondere baten", category "Auto" and a "Boeken" button in the header; "Boeken" → "Bevestigen" books it
- [ ] A €0,01 line shows "Rekeningcontrole" with "Controlebetaling · boeken op 9300 Bijzondere baten"; a €0,50 line shows the same chip but category "Controleren"; the −€ 43,38 subscription of the same merchant has no chip and is classified by Claude or the learned memory
- [ ] Without an account named "Bijzondere baten" and without code 9300 the row is "Controleren" with "Controlebetaling. Geen rekening Bijzondere baten gevonden: kies zelf een grootboekrekening.", expands with the grootboek picker and BTW "GEEN", and has no header button
- [ ] Batch: ticking the pair row selects both lines ("2 geselecteerd"); "Selecteer alle zichtbare regels" includes them; "Goedkeuren" books them with the rest; after a batch with one failed row the other rows stay selected only if they were not booked
- [ ] Partial failure (book one leg in e-Boekhouden by hand first): the dialog stays open with the failed leg only, the alert reads "1 van de 2 controlebetalingen geboekt, 1 mislukt: …", a tegenrekening picked by hand is still there, and the primary button reads "Opnieuw proberen (1)"; the booked leg has left the list and the other leg is a loose "Kaartcontrole" row
- [ ] After booking, Instellingen → Geleerde boekingen shows no new entry for the merchant (a learned "merchant → 9300" would swallow its real charges). Delete any such entry made before this feature
- [ ] Logs: only line ids, mutNrs, statuses and error classes; no merchant names, amounts or descriptions

## 7. Uploading invoices and receipts

- [ ] "Facturen uploaden" accepts PDF, JPG and PNG, several at once; an expanded "Factuur" row has a drop zone too
- [ ] Without an API key the upload fails with "Fout bij {bestand}: Vul eerst je Anthropic API-sleutel in bij Instellingen."
- [ ] The dialog "Facturen controleren" shows the toggle "Factuur"/"Bonnetje", the fields "Leverancier", "Relatie (crediteur)", "Factuurnummer", "Datum", the amounts, "BTW-code", "Tegenrekening", "Archiefmap" and "Omschrijving", and a confidence badge; all fields are editable
- [ ] A bank line that matches shows as "Gevonden afschriftregel:" with "Koppelen"
- [ ] Amounts show with a decimal comma ("12,50"). Type "12,50", "1.234,56" and "12.50" in "Bedrag incl. BTW" in turn: "BTW-bedrag" and "Bedrag excl. BTW" follow each time. Type "12,505" or "twaalf": the field turns red with "Vul een bedrag in, bijvoorbeeld 12,50" and "Boeken" skips the row with "Bedrag ontbreekt of klopt niet"
- [ ] Book an invoice of € 12,50 typed as "12,50", and a receipt with a tip line typed as "2,50": e-Boekhouden shows exactly 12,50 and 2,50 (the old parser booked 12,00 and 2,00)
- [ ] "Boeken" books the invoice; the dialog shows "1 van 1 facturen geboekt" and "Sluiten"; after closing, both lists reload
- [ ] With an "Archiefmap" picked, the file is in that folder in e-boekhouden, named `JJJJ-MM-DD_Leverancier_Factuurnummer.pdf` and linked to the mutation. Without a folder and without R2 (the local stack) nothing is archived
- [ ] Upload the same PDF again: the dialog warns "Mogelijk dubbele factuur"

### 7a. Split bookings (gesplitste boekingen)

- [ ] Upload an invoice of a gift with shipping (e.g. € 39,75: € 35,80 products and € 3,95 shipping, both 9 %). Click "Splitsen over meerdere regels": the amount fields are replaced by "Regels" with the invoice as "Regel 1" (tegenrekening and BTW code kept, the full € 39,75), an empty "Regel 2" with the same BTW code and "Totaal incl. BTW" € 39,75. The status reads "Verdeeld" (regel 2 counts as 0), the empty amount of regel 2 is marked red, and "Boeken" skips the row with "Regel 2: vul een bedrag in"
- [ ] Change regel 1 to "35,80" (status "Nog te verdelen: € 3,95"), fill regel 2 with "3,95" on another ledger (e.g. verzendkosten): the status turns "Verdeeld", the BTW under the amounts reads "BTW € 2,96" and "BTW € 0,33". "Regel toevoegen" pre-fills a third line with the remainder; "Regel 3 verwijderen" removes it again
- [ ] Type "40,00" in regel 1: the status reads "€ 4,20 te veel" and "Boeken" skips the row ("€ 4,20 te veel" in the hint, "1 met regels die nog niet kloppen")
- [ ] Pick another relation with a default ledger: regel 1 follows it (unless you picked regel 1's ledger by hand). "Niet splitsen" brings the single fields back with regel 1's tegenrekening and BTW code and the total unchanged
- [ ] Book it. In e-Boekhouden the FactuurOntvangen has 2 regels on two ledgers with BTW 2,96 and 0,33 (3,29 in total, one cent above the invoice's 3,28: e-boekhouden rounds per regel); the payment is still one line and the bank line is processed. Check in devtools that the request carried `regels` (2 items, `tegenRekeningId` each) and no `extraLines`: ______
- [ ] Restaurant receipt against its bank line (e.g. € 121,00 on the statement): "Bonnetje", then "Splitsen over meerdere regels". The note reads "De regels moeten optellen tot het bedrag op het afschrift: € 121,00." and there is no total field. Three lines: drinks 21 % (HOOG_INK_21), food 9 %, tip "GEEN" on the same ledger; "Boeken" only once the status reads "Verdeeld". e-Boekhouden shows one Geld uitgegeven with 3 regels, each with its own BTW, and the bank line is processed
- [ ] A receipt € 5,00 below its bank line still shows "Splits in fooi"; clicking it opens the editor with the receipt as regel 1 and "Fooi" € 5,00 without BTW as regel 2, status "Verdeeld"
- [ ] A creditnota offers no "Splitsen over meerdere regels"
- [ ] Proposed split: upload the gift + shipping invoice of the fictitious "Voorbeeld Cadeauwinkel" (a staff gift at 9 % plus shipping). The dialog opens already split, with the info alert "Speedy stelt voor deze factuur te splitsen: Het cadeau valt onder de WKR, de verzendkosten niet." (or a similar sentence), the gift on the WKR / personeelsgeschenken ledger, the shipping on verzendkosten or algemene kosten, status "Verdeeld". Nothing is booked until you click "Boeken"
- [ ] While the proposal is untouched the only way back is "Ongedaan maken" in that note (no "Niet splitsen"): it returns to one line with the values of the analysis (ledger, BTW code, amounts). Change any line: the note disappears and "Niet splitsen" appears, which keeps regel 1. Pick another relation with a default ledger while split: the lines keep their ledgers; after "Niet splitsen" the default applies again
- [ ] A restaurant receipt with 21 % drinks, 9 % food and a tip opens split into three lines (reason "btw_tarieven" or "fooi"), the tip "GEEN" without BTW. A plain invoice with one BTW rate (a subscription) opens as one line, without an alert
- [ ] Check in devtools that the analyze response of the gift invoice carries `regels` (2 items) and `splitsAdvies`, and that the subscription's response has neither; the Redis key is `invoice:analyze:v3:…`
- [ ] Afschrift split: expand a "Controleren" line of −€ 121,00 (a restaurant) in "Afschriften" and click "Splitsen…" next to "Verwerken". The dialog "Afschriftregel splitsen" shows the date, the amount with "Geld uitgegeven", the bank text and the booking description, then the lines (focus starts in the amount of regel 2) with the row's ledger and BTW code as regel 1 and the status "… · totaal afschrift € 121,00". Split into drinks 21 %, food 9 % and a tip "GEEN" on another ledger; "Boeken (3 regels)" is enabled only at "Verdeeld". After booking the line leaves the list; e-Boekhouden shows one Geld uitgegeven with 3 regels and the bank line is processed. Instellingen → Geleerde boekingen has no new entry for it
- [ ] "Splitsen…" is absent on a "Betaalt open factuur" row, a payment-group row and when the soort is a factuurbetaling. Pick a relation in the panel before "Splitsen…": the booked mutation carries it. After "Boeken" the dialog closes and focus is on the row's header; after an error the message sits above the buttons and is read out
- [ ] Through the API (`POST /inbox/process-batch`): regels that do not add up to the bank line answer the per-item error "De regels tellen op tot € 119,00, het totaal is € 121,00." and nothing is booked; a soort 4 item with regels answers "Een factuurbetaling kan niet gesplitst worden."
- [ ] Through the API: regels that add up to € 2,00 less than the total answer 400 "De regels tellen op tot € 37,75, het totaal is € 39,75." and nothing is staged or booked; a regel without `tegenRekeningId` answers "Regel 2: kies een grootboekrekening."

## 8. Settings (Instellingen)

- [ ] Open Instellingen from the account menu; the page heading is "Instellingen"
- [ ] Sections: "Anthropic API-sleutel", "Verbinding met e-Boekhouden", "Type onderneming", "SOAP API-gegevens", "REST API-token", "Account", "Team" (when the session has one), "Passkeys", "Geleerde boekingen"
- [ ] API key:
  - [ ] The chip "Ingesteld" shows when a key exists
  - [ ] A valid key (sk-ant-...) saves with "API-sleutel opgeslagen" and the chip appears
  - [ ] A key that doesn't start with sk-ant- is refused: "Dit is geen Anthropic API-sleutel. Die begint met sk-ant-."
  - [ ] A well-formed key that Anthropic rejects is not saved: "Anthropic accepteert deze API-sleutel niet. Controleer of je de hele sleutel hebt gekopieerd."
  - [ ] "Verwijderen" removes the key ("API-sleutel verwijderd")
  - [ ] Opening the inbox twice within 15 minutes checks the key with Anthropic only once: Redis has `apikey:status:<userId>` with a TTL of about 900 s (`TTL <key>`) holding a 64-character hash, never the key. Saving or deleting the key removes the entry
  - [ ] With an exhausted Anthropic credit balance the inbox shows the credit alert on every visit (a refusal is not cached), and it disappears on the first visit after topping up
- [ ] Account shows name, e-mail and "Authenticatie: Passkey"; Team shows "Teamnaam"

## 9. Returning login

- [ ] Log out with "Uitloggen"
- [ ] On the login screen click "Inloggen"; the browser's passkey prompt appears
- [ ] Pick your passkey (discoverable credential)
- [ ] You land in the inbox
- [ ] e-Boekhouden is still connected (chip "Gekoppeld"): the stored token is restored, unless you disconnected. If e-boekhouden ended that token meanwhile, the first bookkeeping call gets 412 and the connect prompt
- [ ] Recovery with a broken SMTP setup (for example `SMTP_PORT` pointing at a closed port): "Passkey kwijt?" with your own address shows "Als er een account bestaat voor …", exactly like an unknown address does, and just as fast (the lookup and the mail run after the answer); the backend log has `Recovery: mail for user <id> not sent: smtp_network` and no e-mail address. With `SMTP_HOST` pointing at a host that accepts but never answers, the log line (`smtp_timeout`) appears after at most 40 seconds
- [ ] Recovery end to end: "Passkey kwijt?", open the link, create the new passkey; you land in the inbox. Log out and log in with the recovered passkey: it works (before the fix the backend refused it as `passkey_mismatch`, because recovery gave the passkey a new user handle)
- [ ] Recovery link opened twice: the second time shows "Deze herstellink is ongeldig, verlopen of al gebruikt. Vraag een nieuwe aan." and the request form
- [ ] Recovery cancelled in the passkey prompt: "Passkey-registratie geannuleerd of verlopen. Vraag hieronder een nieuwe herstellink aan." above the request form (the link is used up)
- [ ] Login whose challenge is gone: click "Inloggen", restart the backend while the passkey prompt is open (challenges live in memory), then confirm the passkey: "Het inloggen duurde te lang. Probeer het opnieuw." (not "Speedy kent deze passkey niet meer" and no English)

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
- [ ] "Bestaande map kiezen" lets you pick any existing folder ("Deze map gebruiken"); the picker's helper reads "Mail & herken zet je bijlagen voortaan in deze map."
- [ ] With a mail folder but no files: the card "Je mailmap "Inbox" is leeg", with where the mail address lives and the advice to use the address without 'herken'

### 14b. Analysis

- [ ] Click "Alles analyseren ({n})": the progress text "Analyseren {n} van {total}…" and "Stoppen" appear; rows show "In wachtrij" / "Analyseren…"; at most three analyses run at once. With more than 20 documents a confirmation with the estimated cost comes first
- [ ] After the run a snackbar sums it up (e.g. "4 geanalyseerd · 1 met betaling · 2 zonder · 1 geen factuur"); titles read "Leverancier · 12 okt 2026 · € 1.234,56", a receipt "Bonnetje Leverancier · …"
- [ ] The invoice whose payment is an unprocessed bank line moves to "Klaar om te boeken" with the chip "Betaling gevonden", the second line "Betaald 13 okt · € -21,78 · {omschrijving}" and the button "Boeken"
- [ ] The receipt without a bank line shows the chip "Wacht op afschrift"; its "Boeken" is disabled with the tooltip "Bonnetjes boek je samen met de betaling"
- [ ] An invoice without a bank line shows the chip "Geen betaling gevonden", the second line "Wordt geboekt als openstaande factuur" and "Boeken" (open booking, 14d)
- [ ] A credit note cannot be booked: "Boeken" is disabled with the tooltip "Een creditnota boek je voorlopig zelf in e-Boekhouden"
- [ ] The terms-and-conditions PDF is analysed as "overig": chip "Geen factuur?", the title stays the file name, the second line is Claude's reason and the file name, the button is "Negeren"
- [ ] ⋮ → "Opnieuw analyseren" calls Claude again (no "Invoice analyze cache HIT" line for it) and replaces the cached result
- [ ] A document analysed in another session comes from the cache: no Claude call, same title
- [ ] A non-EUR invoice (e.g. CHF): the title shows "CHF 32,80"; in the review dialog the linked bank line carries "(geschatte koers, controleer het bedrag)"; the booking uses the bank line's real EUR amount
- [ ] In "Afschriften" the paying bank line shows the chip "Document gevonden" and the indicator "Factuur {titel} staat in je mailmap"; expanding the row shows "Factuur koppelen en boeken". Both tabs point at the same pair (the assignment is global)
- [ ] Redis: `docker compose exec redis redis-cli --scan --pattern 'doc:*'` lists `doc:analysis:v3:<user>:<file>` with a TTL of about 30 days (`TTL <key>`) and no leftover `doc:analyzing:*`. Each analysis also writes `invoice:analyze:v3:<user>:<hash>:<accounts>` (7 days)

### 14c. Booking a matched document (rename and move)

- [ ] Click "Boeken" on the matched invoice. The review dialog opens with the fields filled in, the bank line under "Gekoppeld aan afschriftregel:" and the picker "Verplaatsen naar". Without a supplier folder the helper reads "Het bestand krijgt een archiefnaam en verlaat je mailmap Inbox. Kies je geen map, dan komt het in Facturen / 2026 / 10 Oktober." (with your own folder name)
- [ ] Click "Boeken". The dialog shows "1 van 1 facturen geboekt" and a line ending in "Geboekt · verplaatst naar Facturen / 2026 / 10 Oktober"
- [ ] In e-Boekhouden: a FactuurOntvangen and a FactuurbetalingVerstuurd mutation exist, the bank line is processed, the file is linked to the invoice mutation, renamed to `JJJJ-MM-DD_Leverancier_Factuurnummer.pdf`, sits in the target folder and is gone from the mail folder
- [ ] Naming contract on a real file: legal form stripped ("Voorbeeld Leverancier B.V." → `Voorbeeld-Leverancier`), diacritics transliterated, `/` in the invoice number → `-`, only `[A-Za-z0-9._-]`, lowercase `pdf`/`jpg`/`png`
- [ ] Live-only, name conflict: first put a file with exactly the expected archive name in the target folder, then book. The booked file becomes `…_2.pdf` (counter before the extension) and nothing is overwritten
- [ ] Live-only, explicit target: pick a folder in "Verplaatsen naar"; the file lands there with the same id as before (links intact). The picker never offers the mail folder or its subfolders; through the API such a target is refused with 400 ("De mailmap kan geen doelmap zijn" / "Een submap van de mailmap kan geen doelmap zijn")
- [ ] Live-only, supplier folder: when the supplier already has a folder in the archive, the picker preselects it
- [ ] Live-only, deleted month folder: delete `Facturen / 2026 / 10 Oktober` in e-Boekhouden, then book a document without picking a folder. Speedy creates a new "10 Oktober" under "2026" and files the document there, never in the deleted folder. Check in devtools that `GET /v1/api/folder/directory` still lists the deleted folder with `isDeleted: true`: ______
- [ ] Book a receipt (JPG) against its bank line: one "Geld uitgegeven" mutation; the file is renamed `JJJJ-MM-DD_Leverancier_bonnetje.jpg` and moved
- [ ] Book a mail-folder invoice split over two ledgers (section 7a): the mutation has two regels, and the file is still linked, renamed and moved

### 14d. Booking without a match, then linking the payment

- [ ] Click "Boeken" on an invoice with "Geen betaling gevonden". The dialog shows the alert "Nog geen betaling gevonden, dus Speedy boekt dit als openstaande factuur. Staat het bedrag later op je afschrift, dan stelt Speedy voor om de betaling te koppelen." and the button reads "Boeken als openstaand"
- [ ] Result line "Geboekt als openstaande factuur · verplaatst naar …"; in e-Boekhouden only a FactuurOntvangen exists; the file is renamed and moved
- [ ] Expand "Wacht op betaling" (open posts load on the first expand, once per page load): it lists the invoice from the open-post grid (soort Crediteur) with amount, relation and "Factuur … · vervalt op …", or "vervallen op …" when overdue
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
- [ ] Start two analyses of the same file at once (two tabs): the second is refused with "Dit document wordt al geanalyseerd." and the UI tries three times in total, 1.5 s apart. If the first analysis finishes in that time the row shows its result; otherwise the chip "Analyse mislukt" and "Opnieuw"

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
