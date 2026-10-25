package handler

import (
	"encoding/base64"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// A refused booking must not leave the uploaded file behind in the archive;
// when it is unknown whether the booking exists, the file stays. The order
// and payloads of the calls before the booking are unchanged.
func TestSubmitUploadRemovesStagedFileWhenBookingIsRefused(t *testing.T) {
	const envelope = `{"errorType":"validation","message":"Voorbeeldfout","code":"VAL_001"}`
	tests := []struct {
		name        string
		receipt     bool
		mutatieBody string
		hangup      bool
		wantErr     string
		wantRemoved bool
	}{
		{"invoice, error envelope", false, envelope, false, "De factuur is niet geboekt: e-Boekhouden: Voorbeeldfout", true},
		{"invoice, mutNr 0", false, `{"mutNr":0}`, false, "e-Boekhouden heeft de factuur niet geboekt en gaf geen reden. Controleer de gegevens en probeer het opnieuw.", true},
		{"invoice, unreadable answer", false, `<html>Fout</html>`, false, "e-Boekhouden gaf een onverwacht antwoord. Kijk in e-Boekhouden of de factuur is geboekt voordat je het opnieuw probeert.", false},
		{"invoice, no answer", false, "", true, "De factuur is niet geboekt: ", false},
		{"receipt, error envelope", true, envelope, false, "Het bonnetje is niet geboekt: e-Boekhouden: Voorbeeldfout", true},
		{"receipt, mutNr 0", true, `{"mutNr":0}`, false, "e-Boekhouden heeft het bonnetje niet geboekt en gaf geen reden. Controleer de gegevens en probeer het opnieuw.", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake, h, client := submitTestEnv(t)
			fake.mutatieBody = tc.mutatieBody
			fake.mutatieHangup = tc.hangup

			handler, body := h.SubmitFull, fullRequest(nil)
			if tc.receipt {
				handler, body = h.SubmitReceipt, receiptRequest(nil)
			}
			body["pdfBase64"] = base64.StdEncoding.EncodeToString(jpegBytes)
			body["folderId"] = 202

			status, resp := callSubmit(t, handler, client, body)
			if status != http.StatusBadGateway {
				t.Fatalf("status %d: %v", status, resp)
			}
			if got, _ := resp["error"].(string); !strings.HasPrefix(got, tc.wantErr) {
				t.Errorf("error = %q, want prefix %q", got, tc.wantErr)
			}

			wantWrites := []string{"POST /v1/api/folder/upload", "POST /v1/api/mutatie"}
			if tc.wantRemoved {
				wantWrites = append(wantWrites, "DELETE /v1/api/folder/701")
			}
			if got := fake.writes(); !reflect.DeepEqual(got, wantWrites) {
				t.Errorf("writes = %v, want %v", got, wantWrites)
			}
			// 202 held one file before; the upload added 701.
			if n := len(fake.files[202]); tc.wantRemoved && n != 1 || !tc.wantRemoved && n != 2 {
				t.Errorf("target folder holds %v", fake.files[202])
			}
		})
	}
}

func TestSubmitMailDocumentKeepsTheFileWhenBookingIsRefused(t *testing.T) {
	fake, h, client := submitTestEnv(t)
	fake.mutatieBody = `{"errorType":"validation","message":"Voorbeeldfout","code":"VAL_001"}`

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{"archiveFileId": 501, "targetFolderId": 202}))
	if status != http.StatusBadGateway {
		t.Fatalf("status %d: %v", status, resp)
	}
	// The mail-folder document was there before the booking: never deleted.
	if got := fake.writes(); !reflect.DeepEqual(got, []string{"POST /v1/api/mutatie"}) {
		t.Errorf("writes = %v", got)
	}
	if len(fake.files[100]) != 2 {
		t.Errorf("mail folder = %v", fake.files[100])
	}
}
