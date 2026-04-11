package claude

// InvoiceData is the structured output from Claude's invoice reading.
type InvoiceData struct {
	Leverancier     string            `json:"leverancier"`
	Factuurnummer   string            `json:"factuurnummer"`
	Datum           string            `json:"datum"`
	BedragExclBTW   float64           `json:"bedragExclBtw"`
	BedragInclBTW   float64           `json:"bedragInclBtw"`
	BTWBedrag       float64           `json:"btwBedrag"`
	BTWPercentage   float64           `json:"btwPercentage"`
	Omschrijving    string            `json:"omschrijving"`
	Grootboekcode   string            `json:"grootboekcode"`
	BTWCode         string            `json:"btwCode"`
	IsReverseCharge bool              `json:"isReverseCharge"`
	Confidence      float64           `json:"confidence"`
	Redenering      string            `json:"redenering"`
	BelastingAdvies []BelastingAdvies `json:"belastingAdvies"`
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
