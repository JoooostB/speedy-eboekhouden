package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/auth"
	"github.com/joooostb/speedy-eboekhouden/internal/auth/authtest"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/mail"
)

const (
	recoveryTestRPID   = "localhost"
	recoveryTestOrigin = "http://localhost:3000"
)

// The handler runs without a database: every refusal below happens before
// the account is looked up, and the nil *database.DB panics if one does not.
func newRecoveryTestHandler(t *testing.T) (*PasskeyHandler, *auth.WebAuthnService) {
	t.Helper()
	wa, err := auth.NewWebAuthnService(nil, recoveryTestRPID, "Speedy", recoveryTestOrigin)
	if err != nil {
		t.Fatalf("NewWebAuthnService: %v", err)
	}
	return &PasskeyHandler{webauthn: wa}, wa
}

func callRecoverFinish(t *testing.T, h *PasskeyHandler, headers map[string]string, body []byte) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/recover/finish", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	h.RecoverFinishComplete(c)
	var resp struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w.Code, resp.Error
}

func signedCreation(t *testing.T, options []byte) []byte {
	t.Helper()
	a, err := authtest.New(recoveryTestRPID, recoveryTestOrigin)
	if err != nil {
		t.Fatalf("authtest.New: %v", err)
	}
	body, err := a.Create(options)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return body
}

const victimUserID = "00000000-0000-4000-8000-000000000002"

// Before the fix the account came from X-User-ID: anyone could start a
// registration (no recovery link needed), create a passkey for that
// challenge and post it with someone else's user id, which stored the
// passkey on that account and logged the caller in as its owner.
func TestRecoverFinishRefusesAChallengeFromAnotherCeremony(t *testing.T) {
	h, wa := newRecoveryTestHandler(t)
	options, challengeID, err := wa.BeginRegistration(context.Background(), "ander@example.com", "Andere Gebruiker")
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}

	code, msg := callRecoverFinish(t, h, map[string]string{
		"X-Challenge-ID": challengeID,
		"X-User-ID":      victimUserID,
	}, signedCreation(t, options))
	if code != http.StatusBadRequest || msg != msgRecoveryInvalid {
		t.Fatalf("got %d %q, want 400 %q", code, msg, msgRecoveryInvalid)
	}
}

func TestRecoverFinishRefusesAnotherAccountsUserID(t *testing.T) {
	h, wa := newRecoveryTestHandler(t)
	owner := &database.User{
		ID:             "00000000-0000-4000-8000-000000000001",
		Email:          "voorbeeld@example.com",
		Name:           "Voorbeeld Gebruiker",
		WebAuthnHandle: []byte("voorbeeld-handle-0001"),
	}
	options, challengeID, err := wa.BeginRecoveryRegistration(context.Background(), owner)
	if err != nil {
		t.Fatalf("BeginRecoveryRegistration: %v", err)
	}

	code, msg := callRecoverFinish(t, h, map[string]string{
		"X-Challenge-ID": challengeID,
		"X-User-ID":      victimUserID,
	}, signedCreation(t, options))
	if code != http.StatusBadRequest || msg != msgRecoveryInvalid {
		t.Fatalf("got %d %q, want 400 %q", code, msg, msgRecoveryInvalid)
	}
}

func TestRecoverFinishAnswersInDutch(t *testing.T) {
	h, wa := newRecoveryTestHandler(t)

	if code, msg := callRecoverFinish(t, h, nil, []byte("{}")); code != http.StatusBadRequest || msg != msgRecoveryIncomplete {
		t.Fatalf("no challenge: got %d %q", code, msg)
	}
	if code, msg := callRecoverFinish(t, h, map[string]string{"X-Challenge-ID": "onbekend"}, []byte("{}")); code != http.StatusBadRequest || msg != msgRecoveryExpired {
		t.Fatalf("unknown challenge: got %d %q", code, msg)
	}

	_, challengeID, err := wa.BeginRecoveryRegistration(context.Background(), &database.User{ID: victimUserID, Email: "voorbeeld@example.com", Name: "Voorbeeld"})
	if err != nil {
		t.Fatalf("BeginRecoveryRegistration: %v", err)
	}
	if code, msg := callRecoverFinish(t, h, map[string]string{"X-Challenge-ID": challengeID}, []byte("{}")); code != http.StatusBadRequest || msg != msgRecoveryNotCreated {
		t.Fatalf("broken credential: got %d %q", code, msg)
	}
}

// The answer to a recovery request must not depend on the address: the
// lookup, the token and the mail run after the handler answered. The
// handler has no database here, so any lookup before the answer panics.
func TestRecoverRequestAnswersBeforeLookingUpTheAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var started []string
	h := &PasskeyHandler{
		mailer:        &mail.Mailer{},
		startRecovery: func(email string) { started = append(started, email) },
	}
	call := func(body string) (int, string) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/recover", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.RecoverRequest(c)
		return w.Code, w.Body.String()
	}

	code, body := call(`{"email":"iemand@voorbeeld.test"}`)
	if code != http.StatusOK || body != `{"status":"ok"}` {
		t.Fatalf("got %d %s, want 200 {\"status\":\"ok\"}", code, body)
	}
	if len(started) != 1 || started[0] != "iemand@voorbeeld.test" {
		t.Fatalf("background work started for %v, want the address once", started)
	}

	if code, _ := call(`{"email":"geen-adres"}`); code != http.StatusBadRequest {
		t.Errorf("invalid address: got %d, want 400", code)
	}
	if len(started) != 1 {
		t.Errorf("an invalid address started background work")
	}

	h.mailer = nil
	if code, _ := call(`{"email":"iemand@voorbeeld.test"}`); code != http.StatusServiceUnavailable {
		t.Errorf("without mailer: got %d, want 503", code)
	}
}
