package claude

import "strings"

// Document types Claude assigns to an uploaded file. Everything the
// bookkeeping flow can book is a factuur, bonnetje or creditnota; "overig"
// marks files that are not purchase documents at all (terms and conditions,
// logos, newsletters, quotes) so the UI can park them instead of offering
// to book nonsense.
const (
	DocumentTypeFactuur    = "factuur"
	DocumentTypeBonnetje   = "bonnetje"
	DocumentTypeCreditnota = "creditnota"
	DocumentTypeOverig     = "overig"
)

// InvoiceData is the structured output from Claude's invoice reading.
type InvoiceData struct {
	Leverancier   string  `json:"leverancier"`
	Factuurnummer string  `json:"factuurnummer"`
	Datum         string  `json:"datum"`
	BedragExclBTW float64 `json:"bedragExclBtw"`
	BedragInclBTW float64 `json:"bedragInclBtw"`
	BTWBedrag     float64 `json:"btwBedrag"`
	BTWPercentage float64 `json:"btwPercentage"`
	// Currency is the ISO 4217 code of the invoice amounts (EUR, CHF, USD,
	// GBP, …). Defaults to "EUR" when the PDF doesn't state a currency.
	// Tesla Superchargers in Switzerland, hotels abroad, and foreign SaaS
	// occasionally invoice in non-EUR — we use this for fuzzy bank-line
	// matching via approximate FX conversion.
	Currency        string `json:"currency"`
	Omschrijving    string `json:"omschrijving"`
	Grootboekcode   string `json:"grootboekcode"`
	BTWCode         string `json:"btwCode"`
	IsReverseCharge bool   `json:"isReverseCharge"`
	// DocumentType is one of the DocumentType* constants once
	// NormalizeDocumentType has run (ReadInvoice does this for fresh results;
	// callers that load an older cached InvoiceData must call it themselves
	// because entries written before this field existed have it empty).
	DocumentType string `json:"documentType"`
	// DocumentTypeReason is a short Dutch explanation of the classification,
	// mainly filled for "overig" ("Algemene voorwaarden", "Logo/handtekening")
	// so the UI can show why a file was not treated as an invoice.
	DocumentTypeReason string `json:"documentTypeReason"`
	// IsReceipt is true when the document is a bonnetje/till slip (restaurant,
	// supermarket, gas station, etc.) rather than a formal invoice. Receipts
	// can be booked as "Geld uitgegeven" with the file attached, without
	// needing a crediteur relation. Kept for compatibility: it always equals
	// DocumentType == "bonnetje" after normalisation.
	IsReceipt       bool              `json:"isReceipt"`
	ReceiptReason   string            `json:"receiptReason"`
	Confidence      float64           `json:"confidence"`
	Redenering      string            `json:"redenering"`
	BelastingAdvies []BelastingAdvies `json:"belastingAdvies"`
}

// NormalizeDocumentType makes DocumentType and IsReceipt consistent. The
// type is lowercased and trimmed; an empty or unknown value is derived from
// the legacy IsReceipt flag ("bonnetje" when set, "factuur" otherwise), which
// is also how cached analyses from before DocumentType existed get a type.
// IsReceipt is then re-derived from the final DocumentType so the two can
// never disagree.
func NormalizeDocumentType(d *InvoiceData) {
	if d == nil {
		return
	}
	switch t := strings.ToLower(strings.TrimSpace(d.DocumentType)); t {
	case DocumentTypeFactuur, DocumentTypeBonnetje, DocumentTypeCreditnota, DocumentTypeOverig:
		d.DocumentType = t
	default:
		if d.IsReceipt {
			d.DocumentType = DocumentTypeBonnetje
		} else {
			d.DocumentType = DocumentTypeFactuur
		}
	}
	d.IsReceipt = d.DocumentType == DocumentTypeBonnetje
	d.DocumentTypeReason = strings.TrimSpace(d.DocumentTypeReason)
}

// BelastingAdvies is a tax tip from Claude.
type BelastingAdvies struct {
	Type  string `json:"type"`  // kia, vaste_activa, gemengd_gebruik, representatie, geen_btw
	Tekst string `json:"tekst"` // Dutch explanation
}

// ClassifyRequest is the input for transaction classification.
type ClassifyRequest struct {
	Omschrijving  string  `json:"omschrijving"`
	Bedrag        float64 `json:"bedrag"`
	Tegenrekening string  `json:"tegenrekening,omitempty"`
	Datum         string  `json:"datum"`
}

// ClassifyResult is the structured output from Claude's transaction classification.
type ClassifyResult struct {
	Grootboekcode string  `json:"grootboekcode"`
	BTWCode       string  `json:"btwCode"`
	Soort         string  `json:"soort"`
	Omschrijving  string  `json:"omschrijving"`
	Confidence    float64 `json:"confidence"`
}
