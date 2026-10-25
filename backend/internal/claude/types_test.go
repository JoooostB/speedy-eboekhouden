package claude

import (
	"encoding/json"
	"testing"
)

func TestNormalizeDocumentType(t *testing.T) {
	tests := []struct {
		name          string
		in            InvoiceData
		wantType      string
		wantIsReceipt bool
	}{
		// Each valid value is kept as-is and IsReceipt follows it.
		{"factuur", InvoiceData{DocumentType: "factuur"}, DocumentTypeFactuur, false},
		{"bonnetje", InvoiceData{DocumentType: "bonnetje"}, DocumentTypeBonnetje, true},
		{"creditnota", InvoiceData{DocumentType: "creditnota"}, DocumentTypeCreditnota, false},
		{"overig", InvoiceData{DocumentType: "overig"}, DocumentTypeOverig, false},

		// Mixed case and whitespace are tolerated.
		{"mixed case", InvoiceData{DocumentType: "CreditNota"}, DocumentTypeCreditnota, false},
		{"whitespace", InvoiceData{DocumentType: "  Bonnetje \n"}, DocumentTypeBonnetje, true},

		// Empty (legacy cache entries) falls back to IsReceipt.
		{"empty, not receipt", InvoiceData{}, DocumentTypeFactuur, false},
		{"empty, receipt", InvoiceData{IsReceipt: true}, DocumentTypeBonnetje, true},

		// Unknown values are treated like empty.
		{"unknown, not receipt", InvoiceData{DocumentType: "invoice"}, DocumentTypeFactuur, false},
		{"unknown, receipt", InvoiceData{DocumentType: "receipt", IsReceipt: true}, DocumentTypeBonnetje, true},

		// A valid DocumentType wins over a contradicting IsReceipt.
		{"factuur with stale isReceipt", InvoiceData{DocumentType: "factuur", IsReceipt: true}, DocumentTypeFactuur, false},
		{"overig with stale isReceipt", InvoiceData{DocumentType: "overig", IsReceipt: true}, DocumentTypeOverig, false},
		{"bonnetje without isReceipt", InvoiceData{DocumentType: "bonnetje", IsReceipt: false}, DocumentTypeBonnetje, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.in
			NormalizeDocumentType(&d)
			if d.DocumentType != tc.wantType {
				t.Errorf("DocumentType = %q, want %q", d.DocumentType, tc.wantType)
			}
			if d.IsReceipt != tc.wantIsReceipt {
				t.Errorf("IsReceipt = %v, want %v", d.IsReceipt, tc.wantIsReceipt)
			}
		})
	}
}

func TestNormalizeDocumentTypeTrimsReason(t *testing.T) {
	d := InvoiceData{DocumentType: "overig", DocumentTypeReason: "  Algemene voorwaarden \n"}
	NormalizeDocumentType(&d)
	if d.DocumentTypeReason != "Algemene voorwaarden" {
		t.Errorf("DocumentTypeReason = %q", d.DocumentTypeReason)
	}
}

func TestNormalizeDocumentTypeNil(t *testing.T) {
	// Must not panic; callers pass pointers straight from cache lookups.
	NormalizeDocumentType(nil)
}

// TestInvoiceDataLegacyCacheEntry replays an analysis cached before
// documentType existed: the JSON has no documentType key at all.
func TestInvoiceDataLegacyCacheEntry(t *testing.T) {
	legacy := `{"leverancier":"Voorbeeld B.V.","factuurnummer":"2026-001","bedragInclBtw":121,"isReceipt":true,"receiptReason":"Restaurant"}`
	var d InvoiceData
	if err := json.Unmarshal([]byte(legacy), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.DocumentType != "" {
		t.Fatalf("precondition: legacy entry should have empty DocumentType, got %q", d.DocumentType)
	}
	NormalizeDocumentType(&d)
	if d.DocumentType != DocumentTypeBonnetje || !d.IsReceipt {
		t.Errorf("legacy receipt normalised to %q/isReceipt=%v, want bonnetje/true", d.DocumentType, d.IsReceipt)
	}

	// And the wire format now always carries the field.
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal round-trip: %v", err)
	}
	if m["documentType"] != "bonnetje" {
		t.Errorf("documentType in JSON = %v, want bonnetje", m["documentType"])
	}
	if _, ok := m["documentTypeReason"]; !ok {
		t.Errorf("documentTypeReason missing from JSON")
	}
}
