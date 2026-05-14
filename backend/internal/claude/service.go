package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// readInvoiceMaxTokens is the upper bound for the Sonnet response when
// extracting invoice fields. Real invoices with several belastingadvies
// tips and a long redenering have been observed near 1.5k tokens, so 4k
// gives ~2.5x headroom for unusual cases (multiple line items, long
// supplier descriptions). 2048 — the previous value — was tight and
// occasionally truncated for verbose vendors.
const readInvoiceMaxTokens = 4096

// classifyTransactionMaxTokens covers the single-line classification
// response (a few short JSON fields). 1024 is comfortably above the
// observed ~120 token output but leaves room for unusually long
// indicator/omschrijving values.
const classifyTransactionMaxTokens = 1024

// truncateForLog clips a string to maxLen characters, suffixing
// "...(truncated)" when clipped. Used to keep log lines compact while
// preserving enough of the failing response to debug it.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

const invoiceSystemPromptBase = `You are a Dutch bookkeeping assistant. You read PDF invoices and extract structured data.
Extract the following fields from the invoice. Use Dutch accounting conventions.
Return ONLY valid JSON, no explanation or markdown.

Fields to extract:
- leverancier: company name of the supplier
- factuurnummer: invoice number
- datum: invoice date in YYYY-MM-DD format
- bedragExclBtw: amount excluding VAT (number, in the invoice's stated currency — DO NOT convert to EUR)
- bedragInclBtw: amount including VAT (number, in the invoice's stated currency — DO NOT convert to EUR)
- btwBedrag: VAT amount (number, in the invoice's stated currency)
- btwPercentage: VAT percentage (0, 9, or 21)
- currency: ISO 4217 currency code in uppercase (EUR, CHF, USD, GBP, DKK, SEK, NOK, JPY, CAD, AUD, …). Detect from the symbol or label on the invoice (€, EUR, CHF, $, USD, £, GBP, etc.). Default to "EUR" only when the invoice does NOT state a currency. Never silently convert amounts — preserve the original currency and let downstream code handle FX. Tesla Supercharger receipts from Switzerland are commonly in CHF; many SaaS vendors invoice in USD or GBP.
  CRITICAL: When btwCode is GEEN, btwBedrag MUST be 0 AND bedragExclBtw MUST equal bedragInclBtw.
  This applies to ALL non-VAT invoices: insurance with assurantiebelasting, bank fees, government fees,
  payroll taxes, etc. The assurantiebelasting/insurance tax line is NOT deductible BTW — it's part of
  the cost. Never split it out into bedragExclBtw, even if the PDF shows a separate "premie" line.
  The whole invoice total is what gets booked.
- omschrijving: brief description of what was invoiced (max 200 chars, Dutch)
- grootboekcode: suggested tegenrekening code. ONLY use codes from the list below. Pick the most appropriate one.
- btwCode: one of HOOG_INK_21 (21% purchase), LAAG_INK_9 (9% purchase), GEEN (no VAT), VERL_INK (reverse charge EU services), VERL_INK_L9 (reverse charge 9%), BU_EU_INK (purchase from outside EU), BI_EU_INK (intra-EU goods)
  IMPORTANT — read these rules carefully and apply them strictly. NEVER infer reverse charge from country alone.

  EU VAT Directive Article 226 REQUIRES the supplier to print explicit reverse-charge text on the invoice.
  Without that text the invoice is non-compliant and reverse charge does NOT apply unilaterally on the
  recipient side. Only set a reverse-charge code (VERL_INK / VERL_INK_L9 / BI_EU_INK / BU_EU_INK) when
  the invoice ITSELF says so in plain text.

  Acceptable reverse-charge phrases (any of these on the invoice qualifies):
    * "BTW verlegd" / "BTW verlegd naar de afnemer"
    * "VAT reverse charge" / "Reverse charge"
    * "Steuerschuldnerschaft des Leistungsempfängers" (German)
    * "Autoliquidation" (French)
    * "Inversión del sujeto pasivo" (Spanish)
    * "Article 196 EU VAT Directive" / "Articolo 196" / "Articulo 196"
    * The buyer's foreign VAT number printed alongside a 0% VAT line with a clear cross-border statement

  Decision tree, in order:
  1. Invoice charges Dutch BTW (supplier is registered in NL with a NL VAT number AND the BTW line shows 21% / 9% NL VAT) → HOOG_INK_21 or LAAG_INK_9.
     Critical sub-rule: if the percentage is anything OTHER than 21 or 9 (e.g. 22 in Italy, 20 in France, 19 in Germany), the supplier is charging foreign VAT, NOT Dutch BTW. Continue to the next rules — do NOT use HOOG_INK_21 just because 22 is "close to" 21.
  1a. EV charging at an EU charging station (Tesla Supercharger, Allego, IONITY, Fastned, Shell Recharge, ChargePoint, Mer, MaXiMa, KEME, etc.) — even when the invoice charges local foreign VAT (e.g. 22% Italian, 19% German):
     For B2B EV charging where the recipient has a valid EU VAT number, EU VAT Directive Art 38 places the supply at the customer's country, meaning the supplier should have applied reverse charge (0% VAT + reverse-charge mention). Many charging networks fail to do this and bill local VAT instead. Dutch accounting practice for a NL B.V. with valid NL VAT number is to book as if reverse charge applied — net BTW effect is zero either way and matches the legally-correct treatment. Apply this rule when ALL hold:
       * Supplier is in an EU country (see EU member states list below)
       * The invoice line clearly relates to EV charging / Energiekosten / kWh / laadbeurt / charging session
       * The recipient is on the invoice with a valid NL VAT number ("BTW-nummer NL...B...")
     Then:
       * btwCode = VERL_INK (services, 21% reverse charge — the standard treatment for EV charging as a service)
       * bedragInclBtw = invoice total (incl. local foreign VAT, since you'll book the foreign VAT into the cost)
       * bedragExclBtw = bedragInclBtw, btwBedrag = 0 (zero-rate code, no Dutch BTW splitting)
       * Add a belastingadvies tip type "geen_btw":
         "Tesla / [LEVERANCIER] rekent [PERCENTAGE]% [LAND] BTW maar had reverse charge moeten toepassen. Geboekt als verlegde BTW (intra-EU service, art. 38) — netto BTW-effect is gelijk. De [PERCENTAGE]% lokale BTW (€[BEDRAG]) is geen NL aftrekbare BTW maar onderdeel van de kosten."
  1b. Other foreign VAT charged WITHOUT reverse-charge text (not EV charging — generic foreign supplier with local VAT, e.g. a Swiss hotel, a UK consultancy, an Italian restaurant):
     * btwCode = GEEN. Foreign VAT is NOT deductible via the Dutch BTW-aangifte.
     * The whole invoice total (incl. foreign VAT) is the cost — set bedragExclBtw = bedragInclBtw, btwBedrag = 0.
     * Lower confidence to <= 0.6 because the booking should be reviewed.
     * Add a belastingadvies tip of type "geen_btw" with text:
       "Buitenlandse BTW [PERCENTAGE]% van [LAND]. Niet aftrekbaar via Nederlandse BTW-aangifte. Mogelijk terugvorderbaar via EU-teruggaveprocedure (vanaf €50/€400 drempel) — overweeg of dat de moeite waard is bij dit bedrag."
       Replace [PERCENTAGE] and [LAND] with the actual values from the invoice.
  2. Invoice explicitly contains one of the reverse-charge phrases above:
     * Supplier in EU and the invoice is for services or electricity/gas/digital goods → VERL_INK (21%) or VERL_INK_L9 (9%)
     * Supplier in EU and the invoice is for physical goods shipped intra-community → BI_EU_INK
     * Supplier outside EU → BU_EU_INK
  3. Invoice has 0% VAT but NO reverse-charge phrase, regardless of country:
     * Set btwCode to GEEN.
     * Lower confidence to <= 0.5 because the invoice may be non-compliant (the user should request a corrected invoice or check the supplier's status).
     * Add a belastingadvies tip of type "geen_btw" with text:
       "Op deze factuur staat 0% BTW maar geen verleggingsvermelding. Vraag een correcte factuur op bij de leverancier of controleer of dit terecht is — anders niet aftrekbaar."
  4. Invoice has Dutch BTW but charged at the wrong rate, or supplier is dubious → mark btwCode as GEEN, lower confidence.

  EU member states for reference (DO NOT use this list to infer reverse charge — it's only relevant for choosing between VERL_INK and BU_EU_INK once rule 2 has confirmed reverse charge applies):
    Austria, Belgium, Bulgaria, Croatia, Cyprus, Czechia, Denmark, Estonia, Finland, France,
    Germany / Deutschland, Greece, Hungary, Ireland, Italy, Latvia, Lithuania, Luxembourg, Malta,
    Netherlands, Poland, Portugal, Romania, Slovakia, Slovenia, Spain, Sweden.
  NOT EU (use BU_EU_INK only when rule 2 above confirms reverse charge): UK / GB (post-Brexit),
  Switzerland (CH), Norway (NO), Iceland (IS), Liechtenstein, Turkey, USA, Canada, Australia, Japan.
- isReverseCharge: boolean — true if this is a reverse charge invoice (verlegde BTW). Set to true when btwCode is VERL_INK, VERL_INK_L9, BU_EU_INK, or BI_EU_INK.
- isReceipt: boolean — true when this document is a bonnetje (kassabon, kwitantie, till slip) rather than a formal factuur. Set isReceipt=true when ANY of these apply:
  * The supplier is a restaurant, cafe, bar, lunchroom, take-away, snackbar, koffiehuis, bakery, slijterij, supermarkt, kruidenier, tankstation, parkeergarage, parkeerplaats, kiosk, food truck, or any horeca establishment
  * The document looks like a thermal till receipt: no formal addressee, no factuurnummer (or only a transaction/till number), no payment terms — just date, items, totals
  * bedragInclBtw is below 100 EUR AND there is no proper company invoice metadata (no leverancier address, no KvK/BTW number for the buyer, no factuurnummer)
  Receipts (bonnetjes) get booked directly as "Geld uitgegeven" with the file attached — no leverancier relation has to be created in the boekhouding. Setting isReceipt=true tells the UI to skip the relation picker.
- receiptReason: short Dutch string (max 60 chars) explaining WHY isReceipt is true ("Restaurant", "Supermarkt < €100", "Tankstation kassabon", etc.). Empty string when isReceipt is false.
- confidence: 0.0-1.0 how confident you are in the extraction overall
- redenering: brief Dutch explanation (max 100 chars) of WHY you chose this tegenrekening. Use the full account name, not abbreviations. Example: "Kantoorartikelen → Kantoorkosten" or "Brandstof → Vervoerskosten"
- belastingAdvies: array of short Dutch tax tips relevant to this invoice. Only include when applicable. Each tip is an object with "type" and "tekst". Types and rules:
  * NEVER include type "reverse_charge" — the UI already shows a dedicated warning for isReverseCharge=true, so including it as belastingadvies is redundant noise.
  * type "kia": if bedragInclBtw >= 450 AND it's an investment (hardware, equipment, furniture, vehicle, NOT consumables/subscriptions), advise about KIA (Kleinschaligheidsinvesteringsaftrek). Tip: "Dit bedrijfsmiddel komt mogelijk in aanmerking voor KIA (investeringsaftrek). Registreer het als Vaste Activa in e-Boekhouden."
  * type "vaste_activa": if bedragExclBtw > 450 AND it's a durable good, advise to register as Vaste Activa for depreciation. Tip: "Bedrijfsmiddelen boven €450 moeten worden geactiveerd en afgeschreven via Vaste Activa."
  * type "gemengd_gebruik": if the purchase could be mixed personal/business use (phone, laptop, car, internet), advise about zakelijk percentage. Tip: "Bij gemengd gebruik (zakelijk/privé) mag alleen het zakelijke deel worden afgetrokken."
  * type "representatie": if it's a restaurant, catering, or entertainment expense, advise about the 80% rule. Tip: "Representatiekosten (eten, drinken, entertainment): slechts 80% is aftrekbaar. De overige 20% is niet aftrekbaar."
  * type "geen_btw": if BTW should NOT be deductible (e.g., private use component, representation), mention it.
  If none of the above apply, return an empty array [].`

// LedgerAccountInfo holds the code + description for prompt building.
type LedgerAccountInfo struct {
	Code         string
	Omschrijving string
}

func buildInvoicePrompt(accounts []LedgerAccountInfo) string {
	if len(accounts) == 0 {
		return invoiceSystemPromptBase + "\n\nAvailable tegenrekeningen (use ONLY these codes):\n" +
			"  * 4100 = Kantoorkosten\n  * 4200 = Huisvestingskosten\n  * 4300 = Vervoerskosten\n" +
			"  * 4400 = Verkoopkosten\n  * 4500 = Algemene kosten\n  * 4600 = Personeelskosten\n" +
			"  * 4700 = Afschrijvingen\n  * 1500 = Voorraad\n  If unsure, use 4500."
	}

	var sb strings.Builder
	sb.WriteString(invoiceSystemPromptBase)
	sb.WriteString("\n\nAvailable tegenrekeningen (use ONLY these codes):\n")
	for _, a := range accounts {
		sb.WriteString(fmt.Sprintf("  * %s = %s\n", a.Code, a.Omschrijving))
	}
	sb.WriteString("  If unsure, pick the most general cost account.")
	return sb.String()
}

const classifySystemPrompt = `You are a Dutch bookkeeping assistant for small businesses (ZZP/BV).
Given a bank transaction description and amount, suggest the correct grootboekrekening (ledger account) and BTW code.

Common grootboekrekeningen:
- 4100 Kantoorkosten: office supplies, software (SaaS), subscriptions
- 4200 Huisvestingskosten: rent, energy, water, internet
- 4300 Vervoerskosten: OV, fuel, parking, car lease
- 4400 Verkoopkosten: advertising, marketing, client entertainment
- 4500 Algemene kosten: insurance, legal, accounting fees, bank costs
- 4600 Personeelskosten: salaries, pension, training
- 4700 Afschrijvingen: equipment depreciation
- 1300 Debiteuren: customer payments received
- 1500 Voorraad: inventory purchases
- 8000 Omzet: revenue received
- 0600 Priveopname: owner withdrawals
- 0300 Rekening-courant: intercompany

BTW codes for purchases: HOOG_INK_21, LAAG_INK_9, GEEN
BTW codes for sales: HOOG_VERK_21, LAAG_VERK_9, GEEN

Belastingdienst rules:
- Bank fees, insurance, government fees = GEEN (no VAT)
- Most business purchases in NL = HOOG_INK_21
- Food/groceries for business = LAAG_INK_9
- Salary, pension, taxes = GEEN
- Subscription services from NL = HOOG_INK_21
- EU services (reverse charge) = VERL_INK

Transaction types (soort):
- Negative amount (money out): GeldUitgegeven, FactuurbetalingVerstuurd (if paying an invoice)
- Positive amount (money in): GeldOntvangen, FactuurbetalingOntvangen (if receiving payment)

Return ONLY valid JSON, no explanation:
{"grootboekcode": "4100", "btwCode": "HOOG_INK_21", "soort": "GeldUitgegeven", "omschrijving": "suggested description", "confidence": 0.85}`

// Service provides Claude API integration for invoice reading and transaction classification.
type Service struct{}

// NewService creates a new Claude service.
func NewService() *Service {
	return &Service{}
}

// ReadInvoice sends a PDF to Claude for structured data extraction.
// If accounts is provided, Claude will only suggest codes from that list.
//
// Errors are logged in English with full diagnostics (the actual unmarshal
// error, the response stop reason, and a preview of Claude's output) so
// production failures are debuggable from logs alone. The returned error
// is a clean Dutch message safe to surface in the UI — never the raw
// Claude response, which used to leak through the previous error format.
func (s *Service) ReadInvoice(ctx context.Context, apiKey string, pdfBase64 string, accounts []LedgerAccountInfo) (*InvoiceData, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	prompt := buildInvoicePrompt(accounts)

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_5,
		MaxTokens: readInvoiceMaxTokens,
		// Temperature 0: invoice extraction is a deterministic data-extraction
		// task. We want the same PDF to produce the same JSON every time so a
		// re-upload doesn't oscillate between btwCode=GEEN, BU_EU_INK, etc.
		// Combined with the upload-key-based cache in the inbox match flow,
		// the user gets stable suggestions across retries.
		Temperature: anthropic.Float(0),
		System: []anthropic.TextBlockParam{
			{Text: prompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{
					Data: pdfBase64,
				}),
				anthropic.NewTextBlock("Lees deze factuur en extraheer de gegevens als JSON."),
			),
		},
	})
	if err != nil {
		return nil, classifyAPIError(err)
	}

	// Detect truncation explicitly. Unlike batch classification we can't
	// split a single PDF response into halves, so we surface a specific
	// Dutch error that tells the user the invoice was too complex —
	// usually means there's a way to simplify (fewer line items, shorter
	// description) or the prompt itself needs tightening.
	if string(msg.StopReason) == "max_tokens" {
		log.Printf("Claude ReadInvoice hit max_tokens (limit=%d); response is truncated", readInvoiceMaxTokens)
		return nil, fmt.Errorf("Factuur te complex voor automatische verwerking. Voer de gegevens handmatig in of probeer een andere PDF.")
	}

	text := extractText(msg)
	if text == "" {
		log.Printf("Claude ReadInvoice returned no text content; stopReason=%s", msg.StopReason)
		return nil, fmt.Errorf("Geen antwoord van AI. Probeer het opnieuw.")
	}

	var invoice InvoiceData
	cleaned := cleanJSON(text)
	if err := json.Unmarshal([]byte(cleaned), &invoice); err != nil {
		log.Printf("Claude ReadInvoice unmarshal failed: err=%v stopReason=%s responseLen=%d preview=%s",
			err, msg.StopReason, len(cleaned), truncateForLog(cleaned, 500))
		return nil, fmt.Errorf("Factuurgegevens konden niet worden verwerkt. Probeer het opnieuw of voer handmatig in.")
	}

	return &invoice, nil
}

// ClassifyTransaction sends a bank transaction description to Claude for
// classification. Same logging/error-shape contract as ReadInvoice: English
// log lines with full diagnostics, Dutch user-facing error strings, no
// raw Claude output ever leaking back to the API consumer.
func (s *Service) ClassifyTransaction(ctx context.Context, apiKey string, req ClassifyRequest) (*ClassifyResult, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	userMsg := fmt.Sprintf(
		"Description: %q, Amount: %.2f, Counter account: %q, Date: %s",
		req.Omschrijving, req.Bedrag, req.Tegenrekening, req.Datum,
	)

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:       anthropic.ModelClaudeHaiku4_5,
		MaxTokens:   classifyTransactionMaxTokens,
		Temperature: anthropic.Float(0), // deterministic single-line classification
		System: []anthropic.TextBlockParam{
			{Text: classifySystemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userMsg)),
		},
	})
	if err != nil {
		return nil, classifyAPIError(err)
	}

	if string(msg.StopReason) == "max_tokens" {
		log.Printf("Claude ClassifyTransaction hit max_tokens (limit=%d); response truncated for description=%q",
			classifyTransactionMaxTokens, truncateForLog(req.Omschrijving, 80))
		return nil, fmt.Errorf("AI-suggestie kon niet worden voltooid. Probeer het opnieuw.")
	}

	text := extractText(msg)
	if text == "" {
		log.Printf("Claude ClassifyTransaction returned no text content; stopReason=%s", msg.StopReason)
		return nil, fmt.Errorf("Geen antwoord van AI. Probeer het opnieuw.")
	}

	var result ClassifyResult
	cleaned := cleanJSON(text)
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		log.Printf("Claude ClassifyTransaction unmarshal failed: err=%v stopReason=%s responseLen=%d preview=%s",
			err, msg.StopReason, len(cleaned), truncateForLog(cleaned, 500))
		return nil, fmt.Errorf("AI-suggestie kon niet worden verwerkt. Probeer het opnieuw.")
	}

	return &result, nil
}

// extractText returns the first text block from a Claude message response.
func extractText(msg *anthropic.Message) string {
	for _, block := range msg.Content {
		if block.Type == "text" {
			return block.Text
		}
	}
	return ""
}

// classifyAPIError maps Anthropic API errors to user-friendly Dutch messages.
func classifyAPIError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "credit balance") || strings.Contains(msg, "billing") {
		return fmt.Errorf("Je Anthropic API-tegoed is op. Vul je tegoed aan op console.anthropic.com/settings/plans.")
	}
	if strings.Contains(msg, "invalid_api_key") || strings.Contains(msg, "authentication") {
		return fmt.Errorf("Ongeldige API-sleutel. Controleer je sleutel in Instellingen.")
	}
	if strings.Contains(msg, "rate_limit") {
		return fmt.Errorf("Te veel AI-verzoeken. Wacht even en probeer het opnieuw.")
	}
	if strings.Contains(msg, "overloaded") {
		return fmt.Errorf("Claude is momenteel overbelast. Probeer het over een minuut opnieuw.")
	}
	return fmt.Errorf("AI-verzoek mislukt: %w", err)
}

// cleanJSON strips markdown code fences if Claude wraps the JSON in them.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}
