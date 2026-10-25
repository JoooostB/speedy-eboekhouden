package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// submitResult is a submit call's answer plus what it left in the session
// and the request context.
type submitResult struct {
	status        int
	resp          map[string]any
	tokenCleared  bool
	clientDropped bool
}

// callSubmitWithSession runs a submit handler like callSubmit, with a session
// that carries an e-boekhouden token, and reports whether the handler cleared
// it. The handler has no session store or database, so clearing shows on the
// session object and the request context only.
func callSubmitWithSession(t *testing.T, handler gin.HandlerFunc, client *eboekhouden.Client, body map[string]any) submitResult {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	c.Request.Header.Set("Content-Type", "application/json")
	sess := &session.SessionData{UserID: "user-1", EBoekhoudenToken: "test-token"}
	c.Set(session.ContextKey, sess)
	c.Set(session.ClientContextKey, client)
	handler(c)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %s", w.Body.String())
	}
	v, _ := c.Get(session.ClientContextKey)
	return submitResult{
		status:        w.Code,
		resp:          resp,
		tokenCleared:  sess.EBoekhoudenToken == "",
		clientDropped: v == nil,
	}
}

// A session that expires after the booking leaves the booking standing: the
// answer is a 200 with a warning that says so, and the token is cleared like
// everywhere else a handler sees eboekhouden.ErrSessionExpired.
func TestSubmitSessionExpiresAfterBooking(t *testing.T) {
	upload := map[string]any{"pdfBase64": base64.StdEncoding.EncodeToString(jpegBytes), "folderId": 202}
	tests := []struct {
		name        string
		receipt     bool
		extra       map[string]any
		expireAt    string
		wantWarning string
		wantWrites  []string
		want        map[string]any
	}{
		{
			name:        "mail document, folder lookup",
			extra:       map[string]any{"archiveFileId": 501},
			expireAt:    "GET /v1/api/folder/directory",
			wantWarning: warnMailMoveSessionExpired,
			wantWrites:  []string{"POST /v1/api/mutatie", "POST /v1/api/folderkoppel/koppel/item"},
			want:        map[string]any{"linked": true, "renamed": false, "moved": false, "archiveName": "Voorbeeld-Bijlage.pdf"},
		},
		{
			name:        "mail document, rename",
			extra:       map[string]any{"archiveFileId": 501, "targetFolderId": 202},
			expireAt:    "PUT /v1/api/folder/filename",
			wantWarning: warnMailMoveSessionExpired,
			wantWrites:  []string{"POST /v1/api/mutatie", "POST /v1/api/folderkoppel/koppel/item", "PUT /v1/api/folder/filename"},
			want:        map[string]any{"linked": true, "renamed": false, "moved": false},
		},
		{
			name:        "mail document, move",
			extra:       map[string]any{"archiveFileId": 501, "targetFolderId": 202},
			expireAt:    "POST /v1/api/folder/bulk/update/202",
			wantWarning: warnMailMoveSessionExpired,
			wantWrites:  []string{"POST /v1/api/mutatie", "POST /v1/api/folderkoppel/koppel/item", "PUT /v1/api/folder/filename", "POST /v1/api/folder/bulk/update/202"},
			want:        map[string]any{"linked": true, "renamed": true, "moved": false},
		},
		{
			name:        "mail document, link",
			extra:       map[string]any{"archiveFileId": 501, "targetFolderId": 202},
			expireAt:    "POST /v1/api/folderkoppel/koppel/item",
			wantWarning: warnMailLinkSessionExpired,
			wantWrites:  []string{"POST /v1/api/mutatie", "POST /v1/api/folderkoppel/koppel/item"},
			want:        map[string]any{"archived": true, "linked": false, "moved": false},
		},
		{
			name:        "upload, link",
			extra:       upload,
			expireAt:    "POST /v1/api/folderkoppel/koppel/item",
			wantWarning: warnUploadLinkSessionExpired,
			wantWrites:  []string{"POST /v1/api/folder/upload", "POST /v1/api/mutatie", "POST /v1/api/folderkoppel/koppel/item"},
			want:        map[string]any{"archived": true, "linked": false},
		},
		{
			name:        "receipt from the mail folder, link",
			receipt:     true,
			extra:       map[string]any{"archiveFileId": 502, "targetFolderId": 202},
			expireAt:    "POST /v1/api/folderkoppel/koppel/item",
			wantWarning: warnMailLinkSessionExpired,
			wantWrites:  []string{"POST /v1/api/mutatie", "POST /v1/api/folderkoppel/koppel/item"},
			want:        map[string]any{"linked": false},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake, h, client := submitTestEnv(t)
			fake.expireAt = tc.expireAt
			handler, body := h.SubmitFull, fullRequest(tc.extra)
			if tc.receipt {
				handler, body = h.SubmitReceipt, receiptRequest(tc.extra)
			}

			got := callSubmitWithSession(t, handler, client, body)
			if got.status != http.StatusOK || got.resp["mutNr"] != 4711.0 {
				t.Fatalf("status %d: %v", got.status, got.resp)
			}
			if got.resp["archiveWarning"] != tc.wantWarning {
				t.Errorf("archiveWarning = %v, want %q", got.resp["archiveWarning"], tc.wantWarning)
			}
			for k, v := range tc.want {
				if got.resp[k] != v {
					t.Errorf("%s = %v, want %v", k, got.resp[k], v)
				}
			}
			if !got.tokenCleared || !got.clientDropped {
				t.Errorf("token cleared %v, client dropped %v: want both", got.tokenCleared, got.clientDropped)
			}
			if writes := fake.writes(); !reflect.DeepEqual(writes, tc.wantWrites) {
				t.Errorf("writes = %v, want %v", writes, tc.wantWrites)
			}
		})
	}
}

func TestSubmitFullSessionExpiresBeforePayment(t *testing.T) {
	for _, expireAt := range []string{"GET /v1/api/import/gridtable", "POST /v1/api/mutatie#2"} {
		t.Run(expireAt, func(t *testing.T) {
			fake, h, client := submitTestEnv(t)
			if expireAt == "POST /v1/api/mutatie#2" {
				// The bank line is found; the payment mutation runs into the
				// expired session.
				fake.expireOnMutatie = 2
			} else {
				fake.expireAt = expireAt
			}

			got := callSubmitWithSession(t, h.SubmitFull, client, fullRequest(map[string]any{"importId": 77}))
			if got.status != http.StatusOK || got.resp["mutNr"] != 4711.0 || got.resp["paymentMutNr"] != 0.0 {
				t.Fatalf("status %d: %v", got.status, got.resp)
			}
			if got.resp["paymentWarning"] != warnPaymentSessionExpired {
				t.Errorf("paymentWarning = %v", got.resp["paymentWarning"])
			}
			if !got.tokenCleared {
				t.Error("the token must be cleared")
			}
		})
	}
}

func TestSubmitSessionExpiredAtBookingAnswers412(t *testing.T) {
	for _, receipt := range []bool{false, true} {
		fake, h, client := submitTestEnv(t)
		fake.expireAt = "POST /v1/api/mutatie"
		handler, body := h.SubmitFull, fullRequest(nil)
		if receipt {
			handler, body = h.SubmitReceipt, receiptRequest(nil)
		}

		got := callSubmitWithSession(t, handler, client, body)
		if got.status != http.StatusPreconditionFailed || got.resp["error"] != "eboekhouden_session_expired" {
			t.Errorf("receipt=%v: status %d: %v", receipt, got.status, got.resp)
		}
		if !got.tokenCleared {
			t.Errorf("receipt=%v: the token must be cleared", receipt)
		}
	}
}

func TestSubmitUploadLinkFailureWarns(t *testing.T) {
	fake, h, client := submitTestEnv(t)
	fake.linkStatus = http.StatusInternalServerError

	got := callSubmitWithSession(t, h.SubmitFull, client, fullRequest(map[string]any{
		"pdfBase64": base64.StdEncoding.EncodeToString(jpegBytes), "folderId": 202,
	}))
	if got.status != http.StatusOK || got.resp["archived"] != true || got.resp["linked"] != false {
		t.Fatalf("status %d: %v", got.status, got.resp)
	}
	if got.resp["archiveWarning"] != "De boeking is gelukt en het bestand staat in je archief, maar het is er niet aan gekoppeld. Koppel het zelf aan de boeking in e-Boekhouden." {
		t.Errorf("archiveWarning = %v", got.resp["archiveWarning"])
	}
	if got.tokenCleared {
		t.Error("a link failure that is not an expired session must keep the token")
	}
}
