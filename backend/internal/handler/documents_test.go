package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
)

func TestDocumentStatus(t *testing.T) {
	factuur := &claude.InvoiceData{DocumentType: "factuur", BedragInclBTW: 10}
	bonnetje := &claude.InvoiceData{DocumentType: "bonnetje", BedragInclBTW: 10}
	overig := &claude.InvoiceData{DocumentType: "overig"}
	const pdf, jpg = "application/pdf", "image/jpeg"

	tests := []struct {
		name string
		in   docStatusInput
		want string
	}{
		{"new", docStatusInput{MediaType: pdf, Size: 50000}, docStatusNew},
		{"unmatched", docStatusInput{MediaType: pdf, Size: 50000, Analysis: factuur}, docStatusUnmatched},
		{"matched", docStatusInput{MediaType: pdf, Size: 50000, Analysis: factuur, HasMatch: true}, docStatusMatched},
		{"receipt waiting", docStatusInput{MediaType: jpg, Size: 50000, Analysis: bonnetje}, docStatusReceiptWaiting},
		{"receipt with match is matched", docStatusInput{MediaType: jpg, Size: 50000, Analysis: bonnetje, HasMatch: true}, docStatusMatched},
		{"not invoice", docStatusInput{MediaType: pdf, Size: 50000, Analysis: overig}, docStatusNotInvoice},
		{"not invoice beats match", docStatusInput{MediaType: pdf, Size: 50000, Analysis: overig, HasMatch: true}, docStatusNotInvoice},
		{"duplicate", docStatusInput{MediaType: pdf, Size: 50000, Analysis: factuur, AlreadySubmitted: true}, docStatusDuplicate},
		{"duplicate beats match", docStatusInput{MediaType: pdf, Size: 50000, Analysis: factuur, AlreadySubmitted: true, HasMatch: true}, docStatusDuplicate},
		{"duplicate beats not invoice", docStatusInput{MediaType: pdf, Size: 50000, Analysis: overig, AlreadySubmitted: true}, docStatusDuplicate},
		{"junk image", docStatusInput{MediaType: jpg, Size: 14 * 1024}, docStatusJunk},
		{"junk beats unanalysed duplicate", docStatusInput{MediaType: jpg, Size: 1000, AlreadySubmitted: true}, docStatusJunk},
		{"analysed small image follows its analysis", docStatusInput{MediaType: "image/png", Size: 1000, Analysis: factuur, HasMatch: true}, docStatusMatched},
		{"analysed small receipt waits for a bank line", docStatusInput{MediaType: "image/png", Size: 1000, Analysis: bonnetje}, docStatusReceiptWaiting},
		{"analysed small image can be not invoice", docStatusInput{MediaType: jpg, Size: 1000, Analysis: overig}, docStatusNotInvoice},
		{"analysed small image can be duplicate", docStatusInput{MediaType: jpg, Size: 1000, Analysis: factuur, AlreadySubmitted: true}, docStatusDuplicate},
		{"small pdf is not junk", docStatusInput{MediaType: pdf, Size: 1000}, docStatusNew},
		{"image at the threshold is not junk", docStatusInput{MediaType: jpg, Size: junkImageMaxSize}, docStatusNew},
		{"unsupported", docStatusInput{MediaType: "", Size: 50000}, docStatusUnsupported},
		{"unsupported beats junk size", docStatusInput{MediaType: "", Size: 10}, docStatusUnsupported},
		{"unsupported beats analysis", docStatusInput{MediaType: "", Size: 50000, Analysis: factuur, HasMatch: true}, docStatusUnsupported},
		{"ignored", docStatusInput{MediaType: pdf, Size: 50000, Ignored: true}, docStatusIgnored},
		{"ignored beats unsupported", docStatusInput{MediaType: "", Ignored: true}, docStatusIgnored},
		{"ignored beats match", docStatusInput{MediaType: pdf, Size: 50000, Ignored: true, Analysis: factuur, HasMatch: true}, docStatusIgnored},
		{"linked", docStatusInput{MediaType: pdf, Size: 50000, KoppelCount: 1}, docStatusLinked},
		{"linked beats everything", docStatusInput{MediaType: "", KoppelCount: 2, Ignored: true, Analysis: overig, AlreadySubmitted: true}, docStatusLinked},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := documentStatus(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMediaTypeFromExtension(t *testing.T) {
	tests := map[string]string{
		"factuur.pdf":        "application/pdf",
		"FACTUUR.PDF":        "application/pdf",
		"bon.jpg":            "image/jpeg",
		"bon.JPEG":           "image/jpeg",
		"scan.png":           "image/png",
		"ubl.xml":            "",
		"archive.zip":        "",
		"noext":              "",
		".pdf":               "", // hidden file without a base name
		"my.invoice.v2.pdf":  "application/pdf",
		"trailingdot.":       "",
		"":                   "",
		"document.pdf.exe":   "",
		"weird name (1).PnG": "image/png",
	}
	for name, want := range tests {
		if got := mediaTypeFromExtension(name); got != want {
			t.Errorf("mediaTypeFromExtension(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestDocumentTitleBeforeAnalysis(t *testing.T) {
	tests := []struct {
		name          string
		file          eboekhouden.ArchiveFile
		wantTitle     string
		wantSubtitle  string
		wantSubPrefix string
	}{
		{
			name:         "pdf with underscores",
			file:         eboekhouden.ArchiveFile{Naam: "invoice_8821.pdf", Datum: "2026-10-12T14:32:05.67", Size: 86016},
			wantTitle:    "invoice 8821",
			wantSubtitle: "Ontvangen 12 okt 14:32 · PDF · 84 kB",
		},
		{
			name:         "jpg with dashes and double separators",
			file:         eboekhouden.ArchiveFile{Naam: "bon--albert_heijn.JPG", Datum: "2026-03-01T09:05:00", Size: 51200},
			wantTitle:    "bon albert heijn",
			wantSubtitle: "Ontvangen 1 mrt 09:05 · JPG · 50 kB",
		},
		{
			name:         "megabytes with a decimal comma",
			file:         eboekhouden.ArchiveFile{Naam: "scan.png", Datum: "2026-12-24T23:59:59", Size: 2 * 1024 * 1024},
			wantTitle:    "scan",
			wantSubtitle: "Ontvangen 24 dec 23:59 · PNG · 2,0 MB",
		},
		{
			name:         "tiny file is at least 1 kB",
			file:         eboekhouden.ArchiveFile{Naam: "pixel.png", Datum: "2026-01-02T00:00:00", Size: 120},
			wantTitle:    "pixel",
			wantSubtitle: "Ontvangen 2 jan 00:00 · PNG · 1 kB",
		},
		{
			name:         "unsupported extension shows uppercase extension",
			file:         eboekhouden.ArchiveFile{Naam: "ubl-invoice.xml", Datum: "2026-05-06T10:00:00", Size: 4096},
			wantTitle:    "ubl invoice",
			wantSubtitle: "Ontvangen 6 mei 10:00 · XML · 4 kB",
		},
		{
			name:         "unreadable date leaves the received part out",
			file:         eboekhouden.ArchiveFile{Naam: "x.pdf", Datum: "", Size: 1024},
			wantTitle:    "x",
			wantSubtitle: "PDF · 1 kB",
		},
		{
			name:         "name without extension",
			file:         eboekhouden.ArchiveFile{Naam: "README", Datum: "2026-05-06T10:00:00", Size: 1024},
			wantTitle:    "README",
			wantSubtitle: "Ontvangen 6 mei 10:00 · Bestand · 1 kB",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			title, subtitle := documentTitle(tc.file, nil)
			if title != tc.wantTitle {
				t.Errorf("title = %q, want %q", title, tc.wantTitle)
			}
			if subtitle != tc.wantSubtitle {
				t.Errorf("subtitle = %q, want %q", subtitle, tc.wantSubtitle)
			}
		})
	}
}

func TestDocumentTitleAfterAnalysis(t *testing.T) {
	file := eboekhouden.ArchiveFile{Naam: "invoice_8821.pdf", Datum: "2026-10-12T14:32:05", Size: 86016}
	tests := []struct {
		name         string
		inv          *claude.InvoiceData
		wantTitle    string
		wantSubtitle string
	}{
		{
			name:         "invoice with thousands",
			inv:          &claude.InvoiceData{DocumentType: "factuur", Leverancier: "Anthropic", Factuurnummer: "INV-8821", Datum: "2026-10-12", BedragInclBTW: 1234.56, Currency: "EUR"},
			wantTitle:    "Anthropic · 12 okt 2026 · € 1.234,56",
			wantSubtitle: "Factuur INV-8821 · invoice_8821.pdf",
		},
		{
			name:         "currency defaults to EUR",
			inv:          &claude.InvoiceData{DocumentType: "factuur", Leverancier: "KPN", Factuurnummer: "7", Datum: "2026-01-01", BedragInclBTW: 21.78},
			wantTitle:    "KPN · 1 jan 2026 · € 21,78",
			wantSubtitle: "Factuur 7 · invoice_8821.pdf",
		},
		{
			name:         "millions",
			inv:          &claude.InvoiceData{DocumentType: "factuur", Leverancier: "Bouw", Factuurnummer: "1", Datum: "2026-01-01", BedragInclBTW: 1234567.891},
			wantTitle:    "Bouw · 1 jan 2026 · € 1.234.567,89",
			wantSubtitle: "Factuur 1 · invoice_8821.pdf",
		},
		{
			name:         "non-EUR shows the original currency and an estimate",
			inv:          &claude.InvoiceData{DocumentType: "factuur", Leverancier: "Tesla", Factuurnummer: "4711", Datum: "2026-10-01", BedragInclBTW: 32.80, Currency: "CHF"},
			wantTitle:    "Tesla · 1 okt 2026 · CHF 32,80",
			wantSubtitle: "Factuur 4711 · invoice_8821.pdf · ≈ € 34,44",
		},
		{
			name:         "unknown currency gets no estimate",
			inv:          &claude.InvoiceData{DocumentType: "factuur", Leverancier: "X", Factuurnummer: "1", Datum: "2026-10-01", BedragInclBTW: 10, Currency: "XYZ"},
			wantTitle:    "X · 1 okt 2026 · XYZ 10,00",
			wantSubtitle: "Factuur 1 · invoice_8821.pdf",
		},
		{
			name:         "receipt",
			inv:          &claude.InvoiceData{DocumentType: "bonnetje", Leverancier: "Albert Heijn", Datum: "2026-10-03", BedragInclBTW: 12.5},
			wantTitle:    "Bonnetje Albert Heijn · 3 okt 2026 · € 12,50",
			wantSubtitle: "Bonnetje · invoice_8821.pdf",
		},
		{
			name:         "credit note with number",
			inv:          &claude.InvoiceData{DocumentType: "creditnota", Leverancier: "Coolblue", Factuurnummer: "CN-12", Datum: "2026-09-30", BedragInclBTW: 99.99},
			wantTitle:    "Coolblue · 30 sep 2026 · € 99,99",
			wantSubtitle: "Creditnota CN-12 · invoice_8821.pdf",
		},
		{
			name:         "credit note without number",
			inv:          &claude.InvoiceData{DocumentType: "creditnota", Leverancier: "Coolblue", Datum: "2026-09-30", BedragInclBTW: 99.99},
			wantTitle:    "Coolblue · 30 sep 2026 · € 99,99",
			wantSubtitle: "Creditnota · invoice_8821.pdf",
		},
		{
			name:         "unknown supplier",
			inv:          &claude.InvoiceData{DocumentType: "factuur", Leverancier: "  ", Factuurnummer: "1", Datum: "2026-02-28", BedragInclBTW: 5},
			wantTitle:    "Onbekende leverancier · 28 feb 2026 · € 5,00",
			wantSubtitle: "Factuur 1 · invoice_8821.pdf",
		},
		{
			name:         "invoice without number",
			inv:          &claude.InvoiceData{DocumentType: "factuur", Leverancier: "Acme", Datum: "2026-02-28", BedragInclBTW: 5},
			wantTitle:    "Acme · 28 feb 2026 · € 5,00",
			wantSubtitle: "Factuur · invoice_8821.pdf",
		},
		{
			name:         "missing date and amount are left out",
			inv:          &claude.InvoiceData{DocumentType: "factuur", Leverancier: "Acme", Factuurnummer: "1"},
			wantTitle:    "Acme",
			wantSubtitle: "Factuur 1 · invoice_8821.pdf",
		},
		{
			name:         "overig keeps the file title and shows the reason",
			inv:          &claude.InvoiceData{DocumentType: "overig", DocumentTypeReason: "Algemene voorwaarden"},
			wantTitle:    "invoice 8821",
			wantSubtitle: "Algemene voorwaarden · invoice_8821.pdf",
		},
		{
			name:         "overig without reason",
			inv:          &claude.InvoiceData{DocumentType: "overig"},
			wantTitle:    "invoice 8821",
			wantSubtitle: "Geen factuur · invoice_8821.pdf",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			title, subtitle := documentTitle(file, tc.inv)
			if title != tc.wantTitle {
				t.Errorf("title = %q, want %q", title, tc.wantTitle)
			}
			if subtitle != tc.wantSubtitle {
				t.Errorf("subtitle = %q, want %q", subtitle, tc.wantSubtitle)
			}
		})
	}
}

func TestFormatNLAmount(t *testing.T) {
	tests := map[float64]string{
		0:          "0,00",
		0.5:        "0,50",
		21.78:      "21,78",
		999.999:    "1.000,00",
		1234.56:    "1.234,56",
		1000000:    "1.000.000,00",
		-1234.5:    "-1.234,50",
		123456789:  "123.456.789,00",
		0.005:      "0,01",
		100:        "100,00",
		1000:       "1.000,00",
		12345.678:  "12.345,68",
		-0.004:     "-0,00",
		9999999.99: "9.999.999,99",
	}
	for in, want := range tests {
		if got := formatNLAmount(in); got != want {
			t.Errorf("formatNLAmount(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatFileSize(t *testing.T) {
	tests := map[int64]string{
		0:                 "1 kB",
		1:                 "1 kB",
		1535:              "1 kB",
		1536:              "2 kB",
		86016:             "84 kB",
		228310:            "223 kB",
		1024*1024 - 1:     "1024 kB",
		1024 * 1024:       "1,0 MB",
		1024*1024 + 51200: "1,0 MB",
		1572864:           "1,5 MB",
		10 * 1024 * 1024:  "10,0 MB",
	}
	for in, want := range tests {
		if got := formatFileSize(in); got != want {
			t.Errorf("formatFileSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestParseEBoekhoudenTimeKeepsWallClock(t *testing.T) {
	tests := map[string]string{
		"2026-10-24T15:43:35.67": "2026-10-24 15:43:35",
		"2026-10-24T15:43:35":    "2026-10-24 15:43:35",
		"2026-10-24":             "2026-10-24 00:00:00",
		" 2026-01-02T03:04:05 ":  "2026-01-02 03:04:05",
	}
	for in, want := range tests {
		got, ok := parseEBoekhoudenTime(in)
		if !ok {
			t.Errorf("parseEBoekhoudenTime(%q) failed", in)
			continue
		}
		if s := got.Format("2006-01-02 15:04:05"); s != want {
			t.Errorf("parseEBoekhoudenTime(%q) = %s, want %s", in, s, want)
		}
	}
	for _, bad := range []string{"", "gisteren", "24-10-2026", "2026-10-24T15:43"} {
		if _, ok := parseEBoekhoudenTime(bad); ok {
			t.Errorf("parseEBoekhoudenTime(%q) unexpectedly succeeded", bad)
		}
	}
}

func TestAssignDocumentsToBankLines(t *testing.T) {
	cand := func(id int, score float64) BankLineCandidate {
		return BankLineCandidate{ID: id, Score: score}
	}

	t.Run("higher score wins, loser falls back to its second candidate", func(t *testing.T) {
		docs := map[int][]BankLineCandidate{
			1: {cand(100, 110), cand(101, 95)},
			2: {cand(100, 120), cand(102, 90)},
		}
		got := assignDocumentsToBankLines(docs)
		if got[2].ID != 100 {
			t.Errorf("doc 2 got line %d, want 100", got[2].ID)
		}
		if got[1].ID != 101 {
			t.Errorf("doc 1 got line %d, want 101 (fallback)", got[1].ID)
		}
	})

	t.Run("fallback line can itself be taken", func(t *testing.T) {
		docs := map[int][]BankLineCandidate{
			1: {cand(100, 110), cand(101, 95)},
			2: {cand(100, 120)},
			3: {cand(101, 100)},
		}
		got := assignDocumentsToBankLines(docs)
		if got[2].ID != 100 || got[3].ID != 101 {
			t.Errorf("unexpected assignment: %v", got)
		}
		if _, ok := got[1]; ok {
			t.Errorf("doc 1 should be left without a line, got %v", got[1])
		}
	})

	t.Run("ties go to the lower document id, then the lower line id", func(t *testing.T) {
		docs := map[int][]BankLineCandidate{
			7: {cand(300, 100), cand(200, 100)},
			3: {cand(300, 100)},
		}
		for i := 0; i < 20; i++ { // map iteration order must not leak into the result
			got := assignDocumentsToBankLines(docs)
			if got[3].ID != 300 {
				t.Fatalf("doc 3 got line %d, want 300", got[3].ID)
			}
			if got[7].ID != 200 {
				t.Fatalf("doc 7 got line %d, want 200", got[7].ID)
			}
		}
		// Within one document equal scores prefer the lower line id.
		got := assignDocumentsToBankLines(map[int][]BankLineCandidate{1: {cand(9, 50), cand(4, 50)}})
		if got[1].ID != 4 {
			t.Errorf("doc 1 got line %d, want 4", got[1].ID)
		}
	})

	t.Run("one document per line and one line per document", func(t *testing.T) {
		docs := map[int][]BankLineCandidate{
			1: {cand(100, 100), cand(101, 100), cand(102, 100)},
			2: {cand(100, 100), cand(101, 100), cand(102, 100)},
			3: {cand(100, 100), cand(101, 100), cand(102, 100)},
		}
		got := assignDocumentsToBankLines(docs)
		if len(got) != 3 {
			t.Fatalf("expected 3 assignments, got %d", len(got))
		}
		seen := map[int]bool{}
		for _, c := range got {
			if seen[c.ID] {
				t.Errorf("line %d assigned twice", c.ID)
			}
			seen[c.ID] = true
		}
	})

	t.Run("empty and nil input", func(t *testing.T) {
		if got := assignDocumentsToBankLines(nil); len(got) != 0 {
			t.Errorf("nil input produced %v", got)
		}
		if got := assignDocumentsToBankLines(map[int][]BankLineCandidate{1: nil, 2: {}}); len(got) != 0 {
			t.Errorf("documents without candidates produced %v", got)
		}
	})
}

func TestParseDocumentID(t *testing.T) {
	ok := map[string]int{"1": 1, "12340002": 12340002, " 7 ": 7}
	for in, want := range ok {
		got, valid := parseDocumentID(in)
		if !valid || got != want {
			t.Errorf("parseDocumentID(%q) = (%d, %v), want (%d, true)", in, got, valid, want)
		}
	}
	for _, in := range []string{"", "0", "-1", "abc", "1.5", "1e3", "9999999999999999999999"} {
		if got, valid := parseDocumentID(in); valid {
			t.Errorf("parseDocumentID(%q) = %d, want invalid", in, got)
		}
	}
}

func TestValidateFileIDs(t *testing.T) {
	if _, err := validateFileIDs(nil); err == nil || !strings.Contains(err.Error(), "Kies minstens één document") {
		t.Errorf("empty list: err = %v", err)
	}
	tooMany := make([]int, maxArchiveFileIDs+1)
	for i := range tooMany {
		tooMany[i] = i + 1
	}
	if _, err := validateFileIDs(tooMany); err == nil || !strings.Contains(err.Error(), "Maximaal 100") {
		t.Errorf("101 ids: err = %v", err)
	}
	if got, err := validateFileIDs(tooMany[:maxArchiveFileIDs]); err != nil || len(got) != maxArchiveFileIDs {
		t.Errorf("100 ids: got %d ids, err = %v", len(got), err)
	}
	for _, bad := range [][]int{{0}, {-5}, {1, 0}} {
		if _, err := validateFileIDs(bad); err == nil || !strings.Contains(err.Error(), "Ongeldig") {
			t.Errorf("%v: err = %v", bad, err)
		}
	}
	got, err := validateFileIDs([]int{3, 1, 3, 2, 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != 3 || got[1] != 1 || got[2] != 2 {
		t.Errorf("dedupe kept order wrong: %v", got)
	}
}

func TestArchiveNameFor(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	file := eboekhouden.ArchiveFile{ID: 1, Naam: "Voorbeeld-Bijlage.pdf", Datum: "2026-10-24T15:43:35.67"}

	t.Run("without analysis: received date and base name", func(t *testing.T) {
		datum, name := archiveNameFor(file, nil, now)
		if name != "2026-10-24_Voorbeeld-Bijlage_document.pdf" {
			t.Errorf("name = %q", name)
		}
		if datum.Format("2006-01-02") != "2026-10-24" {
			t.Errorf("datum = %s", datum)
		}
	})
	t.Run("with analysis: invoice fields and date", func(t *testing.T) {
		inv := &claude.InvoiceData{DocumentType: "factuur", Leverancier: "Anthropic, PBC", Factuurnummer: "INV-0042", Datum: "2026-10-12"}
		datum, name := archiveNameFor(file, inv, now)
		if name != "2026-10-12_Anthropic_INV-0042.pdf" {
			t.Errorf("name = %q", name)
		}
		if datum.Format("2006-01-02") != "2026-10-12" {
			t.Errorf("datum = %s", datum)
		}
	})
	t.Run("analysis without date falls back to the received date", func(t *testing.T) {
		inv := &claude.InvoiceData{DocumentType: "bonnetje", Leverancier: "Albert Heijn"}
		_, name := archiveNameFor(eboekhouden.ArchiveFile{Naam: "IMG_0001.JPEG", Datum: "2026-10-03T09:00:00"}, inv, now)
		if name != "2026-10-03_Albert-Heijn_bonnetje.jpg" {
			t.Errorf("name = %q", name)
		}
	})
	t.Run("no dates at all falls back to now", func(t *testing.T) {
		datum, name := archiveNameFor(eboekhouden.ArchiveFile{Naam: "x.pdf"}, nil, now)
		if name != "2026-10-24_x_document.pdf" || !datum.Equal(now) {
			t.Errorf("name = %q, datum = %s", name, datum)
		}
	})
	t.Run("unsupported extension is kept", func(t *testing.T) {
		_, name := archiveNameFor(eboekhouden.ArchiveFile{Naam: "ubl.xml", Datum: "2026-10-03T09:00:00"}, nil, now)
		if name != "2026-10-03_ubl_document.xml" {
			t.Errorf("name = %q", name)
		}
	})
}

func TestInlineContentDisposition(t *testing.T) {
	tests := map[string]string{
		"plain.pdf":            `inline; filename=plain.pdf`,
		"with space.pdf":       `inline; filename="with space.pdf"`,
		"Café factuur.pdf":     `inline; filename*=utf-8''Caf%C3%A9%20factuur.pdf`,
		`a"b.pdf`:              `inline; filename="a\"b.pdf"`,
		"../../etc/passwd.pdf": `inline; filename=passwd.pdf`,
		`C:\temp\scan.png`:     `inline; filename=scan.png`,
		"ctrl\x01char.pdf":     `inline; filename=ctrlchar.pdf`,
		"":                     `inline; filename=document.pdf`,
		"..":                   `inline; filename=document.pdf`,
	}
	for name, want := range tests {
		if got := inlineContentDisposition(name, "application/pdf"); got != want {
			t.Errorf("inlineContentDisposition(%q) = %q, want %q", name, got, want)
		}
	}
	if got := inlineContentDisposition("", "image/png"); got != `inline; filename=document.png` {
		t.Errorf("png fallback = %q", got)
	}
}

func TestWithFreshMatches(t *testing.T) {
	cached := json.RawMessage(`{"invoice":{"leverancier":"Acme"},"matchedBankLine":{"id":1},"bankLineCandidates":[{"id":1}],"filename":"a.pdf","uploadKey":""}`)
	fresh := BankLineCandidate{ID: 42, Datum: "2026-10-13T00:00:00", Bedrag: -21.78, Omschrijving: "ACME"}

	out := withFreshMatches(cached, &fresh, []BankLineCandidate{fresh})
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if m, _ := got["matchedBankLine"].(map[string]any); m["id"] != float64(42) || m["omschrijving"] != "ACME" {
		t.Errorf("matchedBankLine = %v", got["matchedBankLine"])
	}
	if _, hasScore := got["matchedBankLine"].(map[string]any)["score"]; hasScore {
		t.Error("matchedBankLine must use the legacy shape without score")
	}
	if c, _ := got["bankLineCandidates"].([]any); len(c) != 1 {
		t.Errorf("bankLineCandidates = %v", got["bankLineCandidates"])
	}
	if inv, _ := got["invoice"].(map[string]any); inv["leverancier"] != "Acme" {
		t.Error("other fields must pass through untouched")
	}
	if got["filename"] != "a.pdf" {
		t.Error("filename must pass through untouched")
	}

	// No match: null and an empty array, never missing.
	out = withFreshMatches(cached, nil, nil)
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["matchedBankLine"] != nil {
		t.Errorf("matchedBankLine = %v, want null", got["matchedBankLine"])
	}
	if c, ok := got["bankLineCandidates"].([]any); !ok || len(c) != 0 {
		t.Errorf("bankLineCandidates = %v, want []", got["bankLineCandidates"])
	}

	// Garbage in, garbage out unchanged.
	if out := withFreshMatches(json.RawMessage(`not json`), nil, nil); string(out) != "not json" {
		t.Errorf("unparseable response was altered: %s", out)
	}
}

func TestDocumentAnalysisEntryInvoice(t *testing.T) {
	entry := &documentAnalysisEntry{Response: json.RawMessage(`{"invoice":{"leverancier":"Acme","isReceipt":true}}`)}
	inv := entry.invoice()
	if inv == nil || inv.Leverancier != "Acme" {
		t.Fatalf("invoice = %+v", inv)
	}
	if inv.DocumentType != claude.DocumentTypeBonnetje {
		t.Errorf("legacy isReceipt entry should normalise to bonnetje, got %q", inv.DocumentType)
	}
	if (&documentAnalysisEntry{Response: json.RawMessage(`{}`)}).invoice() != nil {
		t.Error("entry without invoice must yield nil")
	}
	var nilEntry *documentAnalysisEntry
	if nilEntry.invoice() != nil {
		t.Error("nil entry must yield nil")
	}
}

func TestDocumentRedisKeys(t *testing.T) {
	if got := documentAnalysisKey("u1", 42); got != "doc:analysis:v"+invoicePromptVersion+":u1:42" {
		t.Errorf("analysis key = %q", got)
	}
	if got := documentAnalyzingKey("u1", 42); got != "doc:analyzing:u1:42" {
		t.Errorf("lock key = %q", got)
	}
	if got := documentIgnoredKey("u1"); got != "doc:ignored:u1" {
		t.Errorf("ignored key = %q", got)
	}
}
