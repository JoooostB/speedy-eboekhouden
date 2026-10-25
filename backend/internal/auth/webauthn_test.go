package auth

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/joooostb/speedy-eboekhouden/internal/auth/authtest"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
)

const (
	testRPID   = "localhost"
	testOrigin = "http://localhost:3000"
)

// The service runs without a database: every path under test returns
// before one is needed, and a nil *database.DB panics if that changes.
func newTestService(t *testing.T) *WebAuthnService {
	t.Helper()
	s, err := NewWebAuthnService(nil, testRPID, "Speedy", testOrigin)
	if err != nil {
		t.Fatalf("NewWebAuthnService: %v", err)
	}
	return s
}

func newAuthenticator(t *testing.T) *authtest.Authenticator {
	t.Helper()
	a, err := authtest.New(testRPID, testOrigin)
	if err != nil {
		t.Fatalf("authtest.New: %v", err)
	}
	return a
}

func finishRequest(body []byte) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/v1/auth/recover/finish", bytes.NewReader(body))
}

// A fictitious account with a stored WebAuthn handle.
func exampleUser() *database.User {
	return &database.User{
		ID:             "00000000-0000-4000-8000-000000000001",
		Email:          "voorbeeld@example.com",
		Name:           "Voorbeeld Gebruiker",
		WebAuthnHandle: []byte("voorbeeld-handle-0001"),
	}
}

// The passkey a recovery registers must carry the account's own handle:
// discoverable login refuses a passkey whose userHandle differs from it.
// Before the fix recovery minted a fresh handle, so a recovered passkey
// could never log in.
func TestRecoveredPasskeyCarriesTheAccountHandleAndLogsIn(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	user := exampleUser()
	a := newAuthenticator(t)

	options, challengeID, err := s.BeginRecoveryRegistration(ctx, user)
	if err != nil {
		t.Fatalf("BeginRecoveryRegistration: %v", err)
	}
	body, err := a.Create(options)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !bytes.Equal(a.UserHandle, user.WebAuthnHandle) {
		t.Fatalf("options carry user handle %q, want the account's %q", a.UserHandle, user.WebAuthnHandle)
	}

	cred, boundUserID, err := s.FinishRecoveryRegistration(ctx, challengeID, finishRequest(body))
	if err != nil {
		t.Fatalf("FinishRecoveryRegistration: %v", err)
	}
	if boundUserID != user.ID {
		t.Fatalf("bound user = %q, want %q", boundUserID, user.ID)
	}

	stored := &database.PasskeyCredential{
		ID:              cred.ID,
		UserID:          user.ID,
		PublicKey:       cred.PublicKey,
		AttestationType: cred.AttestationType,
		AAGUID:          cred.Authenticator.AAGUID,
		SignCount:       cred.Authenticator.SignCount,
		BackupEligible:  cred.Flags.BackupEligible,
		BackupState:     cred.Flags.BackupState,
		UserPresent:     cred.Flags.UserPresent,
		UserVerified:    cred.Flags.UserVerified,
	}

	// Discoverable login as FinishLogin runs it, with the database lookups
	// replaced by the stored credential.
	login := func(t *testing.T) error {
		t.Helper()
		loginOptions, loginID, err := s.BeginLogin(ctx)
		if err != nil {
			t.Fatalf("BeginLogin: %v", err)
		}
		assertion, err := a.Get(loginOptions)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		entry, err := s.takeChallenge(loginID, purposeLogin)
		if err != nil {
			t.Fatalf("takeChallenge: %v", err)
		}
		parsed, err := protocol.ParseCredentialRequestResponseBytes(assertion)
		if err != nil {
			t.Fatalf("parse assertion: %v", err)
		}
		lookup := func(rawID, userHandle []byte) (gowebauthn.User, error) {
			return s.toWebAuthnUser(user, []*database.PasskeyCredential{stored}), nil
		}
		_, err = s.webauthn.ValidateDiscoverableLogin(lookup, *entry.SessionData, parsed)
		return err
	}

	if err := login(t); err != nil {
		t.Fatalf("login with the recovered passkey: %v", err)
	}

	// Control: the same passkey under another handle (what the old recovery
	// produced) is refused, which is why the handle matters.
	a.UserHandle = []byte("een-nieuwe-handle-0002")
	if err := login(t); err == nil || !strings.Contains(err.Error(), "User handle and User ID do not match") {
		t.Fatalf("login with a passkey under a foreign handle: %v, want the user-handle mismatch", err)
	}
}

func TestBeginRecoveryRegistrationFallsBackToTheAccountID(t *testing.T) {
	s := newTestService(t)
	user := exampleUser()
	user.WebAuthnHandle = nil // created before the handle was stored
	a := newAuthenticator(t)

	options, _, err := s.BeginRecoveryRegistration(context.Background(), user)
	if err != nil {
		t.Fatalf("BeginRecoveryRegistration: %v", err)
	}
	if _, err := a.Create(options); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if string(a.UserHandle) != user.ID {
		t.Fatalf("user handle = %q, want the account id like toWebAuthnUser uses", a.UserHandle)
	}
}

// The account takeover: a valid registration ceremony from a challenge
// that was not issued for this account's recovery must not reach the
// account. Registration and login challenges are refused, and so is a
// recovery challenge handed to the registration finish step.
func TestChallengesOnlyFinishTheirOwnCeremony(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	t.Run("a registration challenge cannot finish a recovery", func(t *testing.T) {
		options, challengeID, err := s.BeginRegistration(ctx, "ander@example.com", "Andere Gebruiker")
		if err != nil {
			t.Fatalf("BeginRegistration: %v", err)
		}
		body, err := newAuthenticator(t).Create(options)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		_, userID, err := s.FinishRecoveryRegistration(ctx, challengeID, finishRequest(body))
		if !errors.Is(err, ErrWrongCeremony) || userID != "" {
			t.Fatalf("got user %q, err %v; want ErrWrongCeremony", userID, err)
		}
		// Consumed: a second try finds nothing.
		if _, _, err := s.FinishRecoveryRegistration(ctx, challengeID, finishRequest(body)); !errors.Is(err, ErrChallengeExpired) {
			t.Fatalf("second try: %v, want ErrChallengeExpired", err)
		}
	})

	t.Run("a login challenge cannot finish a recovery", func(t *testing.T) {
		_, challengeID, err := s.BeginLogin(ctx)
		if err != nil {
			t.Fatalf("BeginLogin: %v", err)
		}
		if _, _, err := s.FinishRecoveryRegistration(ctx, challengeID, finishRequest([]byte("{}"))); !errors.Is(err, ErrWrongCeremony) {
			t.Fatalf("got %v, want ErrWrongCeremony", err)
		}
	})

	t.Run("a recovery challenge cannot finish a registration or a login", func(t *testing.T) {
		options, challengeID, err := s.BeginRecoveryRegistration(ctx, exampleUser())
		if err != nil {
			t.Fatalf("BeginRecoveryRegistration: %v", err)
		}
		body, err := newAuthenticator(t).Create(options)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := s.FinishRegistration(ctx, challengeID, finishRequest(body)); !errors.Is(err, ErrWrongCeremony) {
			t.Fatalf("FinishRegistration: %v, want ErrWrongCeremony", err)
		}

		_, challengeID, err = s.BeginRecoveryRegistration(ctx, exampleUser())
		if err != nil {
			t.Fatalf("BeginRecoveryRegistration: %v", err)
		}
		if _, err := s.FinishLogin(ctx, challengeID, finishRequest([]byte("{}"))); !errors.Is(err, ErrWrongCeremony) {
			t.Fatalf("FinishLogin: %v, want ErrWrongCeremony", err)
		}
	})
}

func TestUnknownAndExpiredChallenges(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, _, err := s.FinishRecoveryRegistration(ctx, "onbekend", finishRequest([]byte("{}"))); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("unknown: %v, want ErrChallengeExpired", err)
	}

	s.challenges.Store("verlopen", &challengeEntry{
		SessionData: &gowebauthn.SessionData{},
		Purpose:     purposeRecovery,
		UserID:      exampleUser().ID,
		ExpiresAt:   time.Now().Add(-time.Second),
	})
	if _, _, err := s.FinishRecoveryRegistration(ctx, "verlopen", finishRequest([]byte("{}"))); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("expired: %v, want ErrChallengeExpired", err)
	}

	// The frontend recognises the text; it must stay.
	if ErrChallengeExpired.Error() != "challenge not found or expired" {
		t.Fatalf("ErrChallengeExpired text changed: %q", ErrChallengeExpired)
	}
}
