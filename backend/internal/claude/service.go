package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const invoiceSystemPromptBase = `You are a Dutch bookkeeping assistant. You read PDF invoices and extract structured data.
Extract the following fields from the invoice. Use Dutch accounting conventions.
Return ONLY valid JSON, no explanation or markdown.

Fields to extract:
- leverancier: company name of the supplier
- factuurnummer: invoice number
- datum: invoice date in YYYY-MM-DD format
- bedragExclBtw: amount excluding VAT (number)
- bedragInclBtw: amount including VAT (number)
- btwBedrag: VAT amount (number)
- btwPercentage: VAT percentage (0, 9, or 21)
  CRITICAL: When btwCode is GEEN, btwBedrag MUST be 0 AND bedragExclBtw MUST equal bedragInclBtw.
  This applies to ALL non-VAT invoices: insurance with assurantiebelasting, bank fees, government fees,
  payroll taxes, etc. The assurantiebelasting/insurance tax line is NOT deductible BTW — it's part of
  the cost. Never split it out into bedragExclBtw, even if the PDF shows a separate "premie" line.
  The whole invoice total is what gets booked.
- omschrijving: brief description of what was invoiced (max 200 chars, Dutch)
- grootboekcode: suggested tegenrekening code. ONLY use codes from the list below. Pick the most appropriate one.
- btwCode: one of HOOG_INK_21 (21% purchase), LAAG_INK_9 (9% purchase), GEEN (no VAT), VERL_INK (reverse charge EU services), VERL_INK_L9 (reverse charge 9%), BU_EU_INK (purchase from outside EU), BI_EU_INK (intra-EU goods)
  IMPORTANT reverse charge detection rules:
  * If the supplier is from another EU country AND no Dutch BTW (21%/9%) is charged on the invoice → use VERL_INK (21%) or VERL_INK_L9 (9%)
  * Signs of reverse charge: invoice says "VAT reverse charge", "BTW verlegd", "Article 196 EU VAT Directive", foreign EU VAT number, 0% VAT with EU supplier
  * If supplier is from outside the EU (US, UK, etc.) → use BU_EU_INK
  * If the invoice explicitly charges Dutch BTW → use HOOG_INK_21 or LAAG_INK_9 as normal
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
  * type "reverse_charge": if isReverseCharge is true, ALWAYS include this tip. Tip: "Let op: dit is een reverse charge factuur (verlegde BTW). De BTW moet je zelf aangeven en afdragen in je BTW-aangifte. De BTW-code [code] zorgt hiervoor automatisch in e-Boekhouden." Replace [code] with the actual btwCode.
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
func (s *Service) ReadInvoice(ctx context.Context, apiKey string, pdfBase64 string, accounts []LedgerAccountInfo) (*InvoiceData, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	prompt := buildInvoicePrompt(accounts)

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_5,
		MaxTokens: 2048,
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

	text := extractText(msg)
	if text == "" {
		return nil, fmt.Errorf("no text in Claude response")
	}

	var invoice InvoiceData
	if err := json.Unmarshal([]byte(cleanJSON(text)), &invoice); err != nil {
		return nil, fmt.Errorf("parsing Claude response: %w (response: %s)", err, text)
	}

	return &invoice, nil
}

// ClassifyTransaction sends a bank transaction description to Claude for classification.
func (s *Service) ClassifyTransaction(ctx context.Context, apiKey string, req ClassifyRequest) (*ClassifyResult, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	userMsg := fmt.Sprintf(
		"Description: %q, Amount: %.2f, Counter account: %q, Date: %s",
		req.Omschrijving, req.Bedrag, req.Tegenrekening, req.Datum,
	)

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 512,
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

	text := extractText(msg)
	if text == "" {
		return nil, fmt.Errorf("no text in Claude response")
	}

	var result ClassifyResult
	if err := json.Unmarshal([]byte(cleanJSON(text)), &result); err != nil {
		return nil, fmt.Errorf("parsing Claude response: %w (response: %s)", err, text)
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
