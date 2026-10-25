package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// failingReader yields some bytes, then a read error, like an aborted upload.
type failingReader struct{ sent bool }

func (r *failingReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, "%PDF-1.7"), nil
	}
	return 0, errors.New("unexpected EOF")
}

func TestReadUploadedFile(t *testing.T) {
	data, err := readUploadedFile(bytes.NewReader(make([]byte, maxInvoiceFileSize)))
	if err != nil || len(data) != maxInvoiceFileSize {
		t.Errorf("a file of exactly the limit: %d bytes, err %v", len(data), err)
	}

	if _, err := readUploadedFile(bytes.NewReader(make([]byte, maxInvoiceFileSize+1))); !errors.Is(err, errFileTooLarge) {
		t.Errorf("a file above the limit: err = %v, want errFileTooLarge", err)
	}

	_, err = readUploadedFile(&failingReader{})
	if err == nil || errors.Is(err, errFileTooLarge) {
		t.Errorf("a broken upload: err = %v, want the read error", err)
	}
}

func TestWriteUploadReadError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantError  string
	}{
		{"too large", errFileTooLarge, http.StatusRequestEntityTooLarge, "Het bestand is te groot. Speedy leest bestanden tot 10 MB."},
		{"read error", io.ErrUnexpectedEOF, http.StatusBadRequest, "Het bestand kon niet worden gelezen. Probeer het opnieuw."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			writeUploadReadError(c, tc.err)
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.wantStatus || body["error"] != tc.wantError {
				t.Errorf("got %d %q, want %d %q", w.Code, body["error"], tc.wantStatus, tc.wantError)
			}
		})
	}
}
