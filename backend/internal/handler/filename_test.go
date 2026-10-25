package handler

import (
	"regexp"
	"strings"
	"testing"
)

var archiveNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func TestArchiveFileName(t *testing.T) {
	tests := []struct {
		name                                            string
		leverancier, kenmerk, datum, docType, mediaType string
		want                                            string
	}{
		{
			name: "plain invoice", leverancier: "Anthropic", kenmerk: "INV-0042", datum: "2026-10-12", docType: "factuur", mediaType: "application/pdf",
			want: "2026-10-12_Anthropic_INV-0042.pdf",
		},
		{
			name: "receipt without number, jpeg becomes jpg", leverancier: "Albert Heijn", kenmerk: "", datum: "2026-10-03", docType: "bonnetje", mediaType: "image/jpeg",
			want: "2026-10-03_Albert-Heijn_bonnetje.jpg",
		},
		{
			name: "slash in invoice number", leverancier: "Coolblue", kenmerk: "2026/0042", datum: "2026-09-30", docType: "factuur", mediaType: "application/pdf",
			want: "2026-09-30_Coolblue_2026-0042.pdf",
		},
		{
			name: "underscore in invoice number is not a separator", leverancier: "Acme", kenmerk: "F_2026_17", datum: "2026-01-05", docType: "factuur", mediaType: "application/pdf",
			want: "2026-01-05_Acme_F-2026-17.pdf",
		},
		{
			name: "diacritics transliterated", leverancier: "Café Ångström & Søn", kenmerk: "Nº 7", datum: "2026-02-01", docType: "factuur", mediaType: "application/pdf",
			want: "2026-02-01_Cafe-Angstrom-Son_No-7.pdf",
		},
		{
			name: "sharp s and ligature", leverancier: "Straße ﬁrma", kenmerk: "1", datum: "2026-02-01", docType: "factuur", mediaType: "image/png",
			want: "2026-02-01_Strasse-firma_1.png",
		},
		{
			name: "legal form B.V. stripped", leverancier: "Coolblue B.V.", kenmerk: "1", datum: "2026-02-01", docType: "factuur", mediaType: "application/pdf",
			want: "2026-02-01_Coolblue_1.pdf",
		},
		{
			name: "legal form with comma stripped", leverancier: "Anthropic, PBC", kenmerk: "1", datum: "2026-02-01", docType: "factuur", mediaType: "application/pdf",
			want: "2026-02-01_Anthropic_1.pdf",
		},
		{
			name: "two-token legal form stripped", leverancier: "Amazon EU S.à r.l.", kenmerk: "1", datum: "2026-02-01", docType: "factuur", mediaType: "application/pdf",
			want: "2026-02-01_Amazon-EU_1.pdf",
		},
		{
			name: "stacked legal forms stripped", leverancier: "Foo Holding Inc. Ltd", kenmerk: "1", datum: "2026-02-01", docType: "factuur", mediaType: "application/pdf",
			want: "2026-02-01_Foo-Holding_1.pdf",
		},
		{
			name: "legal form in the middle stays", leverancier: "BV Holland Reizen", kenmerk: "1", datum: "2026-02-01", docType: "factuur", mediaType: "application/pdf",
			want: "2026-02-01_BV-Holland-Reizen_1.pdf",
		},
		{
			name: "case preserved", leverancier: "KPN", kenmerk: "abc", datum: "2026-02-01", docType: "factuur", mediaType: "application/pdf",
			want: "2026-02-01_KPN_abc.pdf",
		},
		{
			name: "empty supplier", leverancier: "", kenmerk: "1", datum: "2026-02-01", docType: "factuur", mediaType: "application/pdf",
			want: "2026-02-01_onbekend_1.pdf",
		},
		{
			name: "supplier of only punctuation", leverancier: "???", kenmerk: "1", datum: "2026-02-01", docType: "factuur", mediaType: "application/pdf",
			want: "2026-02-01_onbekend_1.pdf",
		},
		{
			name: "supplier of only a legal form", leverancier: "B.V.", kenmerk: "1", datum: "2026-02-01", docType: "factuur", mediaType: "application/pdf",
			want: "2026-02-01_onbekend_1.pdf",
		},
		{
			name: "invoice without number", leverancier: "Acme", kenmerk: "", datum: "2026-02-01", docType: "factuur", mediaType: "application/pdf",
			want: "2026-02-01_Acme_document.pdf",
		},
		{
			name: "credit note without number", leverancier: "Acme", kenmerk: "", datum: "2026-02-01", docType: "creditnota", mediaType: "application/pdf",
			want: "2026-02-01_Acme_creditnota.pdf",
		},
		{
			name: "unknown type without number", leverancier: "Acme", kenmerk: " ", datum: "2026-02-01", docType: "", mediaType: "application/pdf",
			want: "2026-02-01_Acme_document.pdf",
		},
		{
			name: "date with time part", leverancier: "Acme", kenmerk: "1", datum: "2026-10-24T15:43:35.67", docType: "factuur", mediaType: "application/pdf",
			want: "2026-10-24_Acme_1.pdf",
		},
		{
			name: "unparseable date", leverancier: "Acme", kenmerk: "1", datum: "gisteren", docType: "factuur", mediaType: "application/pdf",
			want: "0000-00-00_Acme_1.pdf",
		},
		{
			name: "bare extension kept for unsupported type", leverancier: "Acme", kenmerk: "1", datum: "2026-02-01", docType: "", mediaType: "xml",
			want: "2026-02-01_Acme_1.xml",
		},
		{
			name: "bare jpeg extension", leverancier: "Acme", kenmerk: "1", datum: "2026-02-01", docType: "", mediaType: "JPEG",
			want: "2026-02-01_Acme_1.jpg",
		},
		{
			name: "unknown media type uses its subtype", leverancier: "Acme", kenmerk: "1", datum: "2026-02-01", docType: "", mediaType: "image/gif",
			want: "2026-02-01_Acme_1.gif",
		},
		{
			name: "no media type at all", leverancier: "Acme", kenmerk: "1", datum: "2026-02-01", docType: "", mediaType: "",
			want: "2026-02-01_Acme_1.bin",
		},
		{
			name: "leading and trailing separators trimmed", leverancier: "  --Acme-- ", kenmerk: "#001#", datum: "2026-02-01", docType: "factuur", mediaType: "application/pdf",
			want: "2026-02-01_Acme_001.pdf",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := archiveFileName(tc.leverancier, tc.kenmerk, tc.datum, tc.docType, tc.mediaType)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if !archiveNameRe.MatchString(got) {
				t.Errorf("%q contains characters outside [A-Za-z0-9._-]", got)
			}
			if again := archiveFileName(tc.leverancier, tc.kenmerk, tc.datum, tc.docType, tc.mediaType); again != got {
				t.Errorf("not deterministic: %q then %q", got, again)
			}
		})
	}
}

func TestArchiveFileNameLimits(t *testing.T) {
	long := strings.Repeat("Abcdefghij", 10) // 100 characters
	got := archiveFileName(long, long, "2026-02-01", "factuur", "application/pdf")
	parts := strings.Split(strings.TrimSuffix(got, ".pdf"), "_")
	if len(parts) != 3 {
		t.Fatalf("expected 3 fields, got %q", got)
	}
	if len(parts[1]) != archiveNameMaxSupplier {
		t.Errorf("supplier length %d, want %d: %q", len(parts[1]), archiveNameMaxSupplier, parts[1])
	}
	if len(parts[2]) != archiveNameMaxReference {
		t.Errorf("reference length %d, want %d: %q", len(parts[2]), archiveNameMaxReference, parts[2])
	}
	if len(got) > archiveNameMaxTotal {
		t.Errorf("total length %d exceeds %d", len(got), archiveNameMaxTotal)
	}

	// A cut must not leave a trailing separator behind.
	supplier := strings.Repeat("ab ", 20) // "ab ab ab …" → cut lands on a dash
	got = archiveFileName(supplier, "1", "2026-02-01", "factuur", "application/pdf")
	field := strings.Split(got, "_")[1]
	if strings.HasSuffix(field, "-") || strings.HasPrefix(field, "-") {
		t.Errorf("field %q starts or ends with a dash", field)
	}

	// The extension is capped as well, and the whole name stays within the limit.
	got = archiveFileName(long, long, "2026-02-01", "factuur", strings.Repeat("x", 50))
	if len(got) > archiveNameMaxTotal {
		t.Errorf("total length %d exceeds %d: %q", len(got), archiveNameMaxTotal, got)
	}
	if !archiveNameRe.MatchString(got) {
		t.Errorf("%q contains characters outside the allowed set", got)
	}
}

func TestArchiveFileNameNeverStartsWithSeparator(t *testing.T) {
	for _, in := range []string{"-x", ".hidden", "_u", " ", "CON"} {
		got := archiveFileName(in, in, in, "", "application/pdf")
		if strings.HasPrefix(got, "-") || strings.HasPrefix(got, ".") || strings.HasPrefix(got, "_") {
			t.Errorf("archiveFileName(%q, …) = %q starts with a separator", in, got)
		}
		if !archiveNameRe.MatchString(got) {
			t.Errorf("%q contains characters outside the allowed set", got)
		}
	}
}

func TestStripLegalForms(t *testing.T) {
	tests := map[string]string{
		"Coolblue B.V.":             "Coolblue",
		"Coolblue BV":               "Coolblue",
		"Shell Nederland N.V.":      "Shell Nederland",
		"Bakker & Zn V.O.F.":        "Bakker & Zn",
		"Apple Inc.":                "Apple",
		"Acme Ltd":                  "Acme",
		"OpenAI, LLC":               "OpenAI,",
		"Siemens GmbH":              "Siemens",
		"Anthropic PBC":             "Anthropic",
		"Total S.A.":                "Total",
		"Hetzner Online GmbH & Co.": "Hetzner Online",
		"Tiffany & Co.":             "Tiffany",
		"Albert Heijn":              "Albert Heijn",
		"":                          "",
		"B.V.":                      "",
	}
	for in, want := range tests {
		if got := stripLegalForms(in); got != want {
			t.Errorf("stripLegalForms(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestArchiveMediaType(t *testing.T) {
	tests := map[string]string{
		"factuur.pdf":    "application/pdf",
		"bon.JPG":        "image/jpeg",
		"scan.jpeg":      "image/jpeg",
		"scan.png":       "image/png",
		"ubl.xml":        "xml", // unsupported: the bare extension is kept
		"archive.ZIP":    "ZIP",
		"noext":          "",
		".hidden":        "",
		"my.invoice.pdf": "application/pdf",
	}
	for name, want := range tests {
		if got := archiveMediaType(name); got != want {
			t.Errorf("archiveMediaType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestUniqueName(t *testing.T) {
	existing := map[string]bool{
		"2026-09-30_coolblue_2026-0042.pdf":   true,
		"2026-09-30_coolblue_2026-0042_2.pdf": true,
		"readme":                              true,
	}
	tests := map[string]string{
		"2026-09-30_Coolblue_2026-0042.pdf": "2026-09-30_Coolblue_2026-0042_3.pdf", // _2 is taken as well
		"2026-09-30_Coolblue_2026-0043.pdf": "2026-09-30_Coolblue_2026-0043.pdf",
		"README":                            "README_2", // no extension, case-insensitive
		"":                                  "",
	}
	for in, want := range tests {
		if got := uniqueName(in, existing); got != want {
			t.Errorf("uniqueName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := uniqueName("a.pdf", nil); got != "a.pdf" {
		t.Errorf("uniqueName with nil set = %q", got)
	}
	// Names that are already in the set under their exact spelling are found too.
	if got := uniqueName("Mixed.PDF", map[string]bool{"Mixed.PDF": true}); got != "Mixed_2.PDF" {
		t.Errorf("uniqueName exact spelling = %q", got)
	}
}
