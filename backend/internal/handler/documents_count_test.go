package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
)

// analysisEntryFor builds the cached analysis of a document the way
// AnalyzeDocument stores it.
func analysisEntryFor(t *testing.T, inv claude.InvoiceData) *documentAnalysisEntry {
	t.Helper()
	body, err := json.Marshal(map[string]any{"invoice": inv})
	if err != nil {
		t.Fatal(err)
	}
	return &documentAnalysisEntry{PdfHash: "hash", AnalyzedAt: time.Now(), Response: body}
}

func TestCountOpenDocuments(t *testing.T) {
	files := []eboekhouden.ArchiveFile{
		{ID: 1, Naam: "factuur.pdf", Size: 80_000},                 // new: counted
		{ID: 2, Naam: "geboekt.pdf", Size: 80_000, KoppelCount: 1}, // linked: not counted
		{ID: 3, Naam: "genegeerd.pdf", Size: 80_000},               // ignored: not counted
		{ID: 4, Naam: "ubl.xml", Size: 80_000},                     // unsupported: not counted
		{ID: 5, Naam: "logo.png", Size: 2_000},                     // junk (small, unanalysed): not counted
		{ID: 6, Naam: "bon.png", Size: 2_000},                      // small but analysed as bonnetje: counted
		{ID: 7, Naam: "voorwaarden.pdf", Size: 80_000},             // analysed as overig: still open (user ignores it)
		{ID: 8, Naam: "scan.jpg", Size: 300_000},                   // new image above the junk size: counted
	}
	ignored := map[int]bool{3: true}
	analyses := map[int]*documentAnalysisEntry{
		6: analysisEntryFor(t, claude.InvoiceData{DocumentType: "bonnetje", BedragInclBTW: 12.5}),
		7: analysisEntryFor(t, claude.InvoiceData{DocumentType: "overig"}),
	}

	if got := countOpenDocuments(files, ignored, analyses); got != 4 {
		t.Errorf("countOpenDocuments = %d, want 4 (ids 1, 6, 7, 8)", got)
	}

	// Without the analyses the small receipt image counts as junk.
	if got := countOpenDocuments(files, ignored, nil); got != 3 {
		t.Errorf("countOpenDocuments without analyses = %d, want 3 (ids 1, 7, 8)", got)
	}

	if got := countOpenDocuments(nil, nil, nil); got != 0 {
		t.Errorf("countOpenDocuments(empty) = %d, want 0", got)
	}
}
