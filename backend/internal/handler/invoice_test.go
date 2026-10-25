package handler

import (
	"strings"
	"testing"

	"github.com/joooostb/speedy-eboekhouden/internal/claude"
)

func TestDetectDocumentMediaType(t *testing.T) {
	pngSig := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	jpegSig := []byte{0xff, 0xd8, 0xff, 0xe0}
	tests := []struct {
		name     string
		data     []byte
		wantType string
		wantOK   bool
	}{
		{"pdf", []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n1 0 obj"), "application/pdf", true},
		{"png", append(pngSig, make([]byte, 16)...), "image/png", true},
		{"jpeg", append(jpegSig, make([]byte, 16)...), "image/jpeg", true},
		{"plain text", []byte("hello world"), "", false},
		{"html", []byte("<!DOCTYPE html><html></html>"), "", false},
		{"xml (UBL)", []byte(`<?xml version="1.0"?><Invoice/>`), "", false},
		{"gif", []byte("GIF89a\x01\x00\x01\x00"), "", false},
		{"empty", nil, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := detectDocumentMediaType(tc.data)
			if ok != tc.wantOK || got != tc.wantType {
				t.Errorf("got (%q, %v), want (%q, %v)", got, ok, tc.wantType, tc.wantOK)
			}
		})
	}
}

func TestDocumentExtension(t *testing.T) {
	tests := map[string]string{
		"application/pdf": "pdf",
		"image/jpeg":      "jpg",
		"image/png":       "png",
		"image/gif":       "",
		"text/plain":      "",
		"":                "",
	}
	for mt, want := range tests {
		if got := documentExtension(mt); got != want {
			t.Errorf("documentExtension(%q) = %q, want %q", mt, got, want)
		}
	}
}

func TestInvoiceAnalysisCacheKeyIsVersioned(t *testing.T) {
	data := []byte("%PDF-1.4 fake")
	hash := invoicePDFContentHash(data)
	accounts := []claude.LedgerAccountInfo{{Code: "4000"}, {Code: "1300"}}

	key := invoiceAnalysisCacheKey("user-1", data, accounts)

	// The version sits right after the prefix, before user and hash.
	wantPrefix := "invoice:analyze:v" + invoicePromptVersion + ":user-1:" + hash + ":"
	if invoicePromptVersion == "" {
		t.Fatal("invoicePromptVersion must not be empty")
	}
	if !strings.HasPrefix(key, wantPrefix) {
		t.Errorf("key = %q, want prefix %q", key, wantPrefix)
	}
	// Entries written by the unversioned (v1) layout must never be hit.
	if strings.HasPrefix(key, "invoice:analyze:user-1:") {
		t.Errorf("key %q still uses the unversioned layout", key)
	}

	// The accounts hash is order independent …
	reordered := invoiceAnalysisCacheKey("user-1", data, []claude.LedgerAccountInfo{{Code: "1300"}, {Code: "4000"}})
	if reordered != key {
		t.Errorf("account order changed the key:\n%s\n%s", key, reordered)
	}
	// … but a different account set, user or file gives a different key.
	if other := invoiceAnalysisCacheKey("user-1", data, accounts[:1]); other == key {
		t.Error("different account set produced the same key")
	}
	if other := invoiceAnalysisCacheKey("user-2", data, accounts); other == key {
		t.Error("different user produced the same key")
	}
	if other := invoiceAnalysisCacheKey("user-1", []byte("%PDF-1.4 other"), accounts); other == key {
		t.Error("different file produced the same key")
	}

	// The submitted marker is deliberately not versioned: it must keep
	// matching bookings made before a prompt bump.
	if got := invoiceSubmittedMarkerKey("user-1", hash); got != "invoice:submitted:user-1:"+hash {
		t.Errorf("marker key = %q", got)
	}
}
