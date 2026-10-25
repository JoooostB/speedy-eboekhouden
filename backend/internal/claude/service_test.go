package claude

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// contentBlockJSON mirrors the wire shape of a document or image block so a
// test can check what the SDK would actually send, independent of the
// SDK's internal union representation.
type contentBlockJSON struct {
	Type   string `json:"type"`
	Source struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

func TestBuildInvoiceContentBlock(t *testing.T) {
	const data = "QUJD" // "ABC"

	tests := []struct {
		name          string
		mediaType     string
		wantBlockType string // "document" or "image"
		wantMediaType string
	}{
		{"pdf", "application/pdf", "document", "application/pdf"},
		{"jpeg", "image/jpeg", "image", "image/jpeg"},
		{"png", "image/png", "image", "image/png"},
		{"jpg alias", "image/jpg", "image", "image/jpeg"},
		{"uppercase with parameters", "IMAGE/PNG; charset=binary", "image", "image/png"},
		{"pdf with parameters and whitespace", " Application/PDF ;name=x ", "document", "application/pdf"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block, err := buildInvoiceContentBlock(tc.mediaType, data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// The union must carry exactly the expected variant.
			switch tc.wantBlockType {
			case "document":
				if block.OfDocument == nil || block.OfImage != nil {
					t.Fatalf("want OfDocument set and OfImage nil, got OfDocument=%v OfImage=%v", block.OfDocument != nil, block.OfImage != nil)
				}
				if block.OfDocument.Source.OfBase64 == nil || block.OfDocument.Source.OfBase64.Data != data {
					t.Fatalf("document base64 source not populated with the data")
				}
			case "image":
				if block.OfImage == nil || block.OfDocument != nil {
					t.Fatalf("want OfImage set and OfDocument nil, got OfImage=%v OfDocument=%v", block.OfImage != nil, block.OfDocument != nil)
				}
				if block.OfImage.Source.OfBase64 == nil || block.OfImage.Source.OfBase64.Data != data {
					t.Fatalf("image base64 source not populated with the data")
				}
			}

			// And the JSON the SDK would send must carry the right type/media_type.
			raw, err := json.Marshal(block)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got contentBlockJSON
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal %s: %v", raw, err)
			}
			if got.Type != tc.wantBlockType {
				t.Errorf("type = %q, want %q (json: %s)", got.Type, tc.wantBlockType, raw)
			}
			if got.Source.Type != "base64" {
				t.Errorf("source.type = %q, want base64 (json: %s)", got.Source.Type, raw)
			}
			if got.Source.MediaType != tc.wantMediaType {
				t.Errorf("source.media_type = %q, want %q (json: %s)", got.Source.MediaType, tc.wantMediaType, raw)
			}
			if got.Source.Data != data {
				t.Errorf("source.data = %q, want %q", got.Source.Data, data)
			}
		})
	}
}

func TestBuildInvoiceContentBlockUnsupported(t *testing.T) {
	for _, mediaType := range []string{
		"application/xml",
		"",
		"   ",
		"image/gif", // Claude supports GIF, but the mail folder flow does not
		"text/plain",
		"application/pdf-ish",
		"; charset=binary",
	} {
		name := mediaType
		if strings.TrimSpace(name) == "" {
			name = "blank"
		}
		t.Run(name, func(t *testing.T) {
			block, err := buildInvoiceContentBlock(mediaType, "QUJD")
			if !errors.Is(err, ErrUnsupportedMediaType) {
				t.Fatalf("mediaType %q: err = %v, want ErrUnsupportedMediaType", mediaType, err)
			}
			if block.OfDocument != nil || block.OfImage != nil {
				t.Errorf("mediaType %q: expected an empty block on error", mediaType)
			}
			// The user-facing text is Dutch and must not echo the input.
			if err.Error() != "Speedy kan alleen PDF-, JPG- en PNG-bestanden lezen." {
				t.Errorf("error text = %q", err.Error())
			}
		})
	}
}

func TestNormalizeMediaType(t *testing.T) {
	tests := map[string]string{
		"application/pdf":           "application/pdf",
		"APPLICATION/PDF":           "application/pdf",
		"image/jpg":                 "image/jpeg",
		"IMAGE/JPG":                 "image/jpeg",
		"image/jpeg":                "image/jpeg",
		"IMAGE/PNG; charset=binary": "image/png",
		"  image/png ; q=1 ":        "image/png",
		"":                          "",
		";":                         "",
	}
	for in, want := range tests {
		if got := normalizeMediaType(in); got != want {
			t.Errorf("normalizeMediaType(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestInvoicePromptDescribesDocumentType guards the JSON contract between
// the prompt and InvoiceData: every documentType value the normaliser
// accepts must be spelled out for Claude, with and without a ledger list.
func TestInvoicePromptDescribesDocumentType(t *testing.T) {
	prompts := map[string]string{
		"default accounts": buildInvoicePrompt(nil),
		"custom accounts":  buildInvoicePrompt([]LedgerAccountInfo{{Code: "4100", Omschrijving: "Kantoorkosten"}}),
	}
	wantTerms := []string{
		"documentType",
		"documentTypeReason",
		`"` + DocumentTypeFactuur + `"`,
		`"` + DocumentTypeBonnetje + `"`,
		`"` + DocumentTypeCreditnota + `"`,
		`"` + DocumentTypeOverig + `"`,
		"isReceipt",
	}
	for name, prompt := range prompts {
		for _, term := range wantTerms {
			if !strings.Contains(prompt, term) {
				t.Errorf("%s prompt does not mention %s", name, term)
			}
		}
		if strings.Contains(prompt, "You read PDF invoices") {
			t.Errorf("%s prompt still claims it only reads PDFs", name)
		}
	}
	if !strings.Contains(prompts["custom accounts"], "4100 = Kantoorkosten") {
		t.Errorf("custom accounts prompt does not list the supplied ledger account")
	}
}
