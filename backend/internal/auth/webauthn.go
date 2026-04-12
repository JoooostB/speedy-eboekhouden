package auth

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
)

// WebAuthnService handles passkey registration and login ceremonies.
type WebAuthnService struct {
	webauthn *gowebauthn.WebAuthn
	db       *database.DB

	// Ephemeral challenge storage (keyed by challenge ID, short TTL).
	challenges sync.Map
}

type challengeEntry struct {
	SessionData *gowebauthn.SessionData
	UserID      string // empty during registration
	Email       string
	Name        string
	ExpiresAt   time.Time
}

// NewWebAuthnService creates a new WebAuthn relying party.
func NewWebAuthnService(db *database.DB, rpID, rpDisplayName, origin string) (*WebAuthnService, error) {
	requireResidentKey := true
	wconfig := &gowebauthn.Config{
		RPID:          rpID,
		RPDisplayName: rpDisplayName,
		RPOrigins:     []string{origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			RequireResidentKey: &requireResidentKey,
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationPreferred,
		},
	}

	w, err := gowebauthn.New(wconfig)
	if err != nil {
		return nil, fmt.Errorf("creating webauthn: %w", err)
	}

	svc := &WebAuthnService{webauthn: w, db: db}
	go svc.cleanupChallenges()
	return svc, nil
}

// DB returns the database handle (for handlers that need direct DB access).
func (s *WebAuthnService) DB() *database.DB {
	return s.db
}

// webAuthnUser adapts our database user to the go-webauthn User interface.
type webAuthnUser struct {
	id          []byte
	name        string
	displayName string
	credentials []gowebauthn.Credential
}

func (u *webAuthnUser) WebAuthnID() []byte                           { return u.id }
func (u *webAuthnUser) WebAuthnName() string                         { return u.name }
func (u *webAuthnUser) WebAuthnDisplayName() string                  { return u.displayName }
func (u *webAuthnUser) WebAuthnCredentials() []gowebauthn.Credential { return u.credentials }

// BeginRegistration starts the passkey registration ceremony.
// Returns WebAuthn creation options and a challenge ID for the client.
func (s *WebAuthnService) BeginRegistration(ctx context.Context, email, name string) (json.RawMessage, string, error) {
	// Generate a random 16-byte WebAuthn user handle.
	// Stored in the DB and used for discoverable login matching.
	userHandle := uuid.New()

	user := &webAuthnUser{
		id:          userHandle[:],
		name:        email,
		displayName: name,
		credentials: nil,
	}

	options, sessionData, err := s.webauthn.BeginRegistration(user)
	if err != nil {
		return nil, "", fmt.Errorf("beginning registration: %w", err)
	}

	challengeID := uuid.New().String()
	s.challenges.Store(challengeID, &challengeEntry{
		SessionData: sessionData,
		Email:       email,
		Name:        name,
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	})

	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling options: %w", err)
	}

	return optionsJSON, challengeID, nil
}

// FinishRegistration completes the passkey registration ceremony.
// Creates the user and team in the database. Returns the new user.
func (s *WebAuthnService) FinishRegistration(ctx context.Context, challengeID string, r *http.Request) (*database.User, error) {
	val, ok := s.challenges.LoadAndDelete(challengeID)
	if !ok {
		return nil, fmt.Errorf("challenge not found or expired")
	}
	entry := val.(*challengeEntry)
	if time.Now().After(entry.ExpiresAt) {
		return nil, fmt.Errorf("challenge expired")
	}

	user := &webAuthnUser{
		id:          entry.SessionData.UserID,
		name:        entry.Email,
		displayName: entry.Name,
		credentials: nil,
	}

	credential, err := s.webauthn.FinishRegistration(user, *entry.SessionData, r)
	if err != nil {
		return nil, fmt.Errorf("finishing registration: %w", err)
	}

	// Create user in database with the WebAuthn user handle
	dbUser, err := s.db.CreateUser(ctx, entry.Email, entry.Name, entry.SessionData.UserID)
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	// Store credential
	var transports []string
	for _, t := range credential.Transport {
		transports = append(transports, string(t))
	}

	dbCred := &database.PasskeyCredential{
		ID:              credential.ID,
		UserID:          dbUser.ID,
		PublicKey:       credential.PublicKey,
		AttestationType: string(credential.AttestationType),
		AAGUID:          credential.Authenticator.AAGUID,
		Transport:       transports,
		SignCount:       credential.Authenticator.SignCount,
		FriendlyName:    "Eerste passkey",
		// Persist the WebAuthn flags so future logins can validate that
		// BackupEligible hasn't changed (which it never should — the spec
		// requires it to be immutable per credential).
		BackupEligible: credential.Flags.BackupEligible,
		BackupState:    credential.Flags.BackupState,
		UserPresent:    credential.Flags.UserPresent,
		UserVerified:   credential.Flags.UserVerified,
	}
	log.Printf("Storing new credential for %s: id=%s (len=%d) BE=%t BS=%t",
		entry.Email, hex.EncodeToString(credential.ID), len(credential.ID),
		credential.Flags.BackupEligible, credential.Flags.BackupState)
	if err := s.db.StoreCredential(ctx, dbCred); err != nil {
		return nil, fmt.Errorf("storing credential: %w", err)
	}

	// Create default team
	_, err = s.db.CreateTeam(ctx, entry.Name, dbUser.ID)
	if err != nil {
		return nil, fmt.Errorf("creating default team: %w", err)
	}

	return dbUser, nil
}

// FinishRecoveryRegistration completes a passkey registration ceremony without creating a new user.
// Used during account recovery — the user already exists.
func (s *WebAuthnService) FinishRecoveryRegistration(ctx context.Context, challengeID string, r *http.Request) (*gowebauthn.Credential, error) {
	val, ok := s.challenges.LoadAndDelete(challengeID)
	if !ok {
		return nil, fmt.Errorf("challenge not found or expired")
	}
	entry := val.(*challengeEntry)
	if time.Now().After(entry.ExpiresAt) {
		return nil, fmt.Errorf("challenge expired")
	}

	user := &webAuthnUser{
		id:          entry.SessionData.UserID,
		name:        entry.Email,
		displayName: entry.Name,
		credentials: nil,
	}

	credential, err := s.webauthn.FinishRegistration(user, *entry.SessionData, r)
	if err != nil {
		return nil, fmt.Errorf("finishing registration: %w", err)
	}

	return credential, nil
}

// BeginLogin starts a discoverable passkey login ceremony (no email needed).
func (s *WebAuthnService) BeginLogin(ctx context.Context) (json.RawMessage, string, error) {
	options, sessionData, err := s.webauthn.BeginDiscoverableLogin()
	if err != nil {
		return nil, "", fmt.Errorf("beginning discoverable login: %w", err)
	}

	challengeID := uuid.New().String()
	s.challenges.Store(challengeID, &challengeEntry{
		SessionData: sessionData,
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	})

	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling options: %w", err)
	}

	return optionsJSON, challengeID, nil
}

// FinishLogin completes the passkey login ceremony. Returns the authenticated user.
func (s *WebAuthnService) FinishLogin(ctx context.Context, challengeID string, r *http.Request) (*database.User, error) {
	val, ok := s.challenges.LoadAndDelete(challengeID)
	if !ok {
		return nil, fmt.Errorf("challenge not found or expired")
	}
	entry := val.(*challengeEntry)
	if time.Now().After(entry.ExpiresAt) {
		return nil, fmt.Errorf("challenge expired")
	}

	handler := func(rawID, userHandle []byte) (gowebauthn.User, error) {
		log.Printf("Discoverable login: rawID=%s userHandle=%s",
			hex.EncodeToString(rawID), hex.EncodeToString(userHandle))
		cred, err := s.db.GetCredentialByID(ctx, rawID)
		if err != nil {
			log.Printf("Credential lookup failed for rawID=%s: %v", hex.EncodeToString(rawID), err)
			return nil, fmt.Errorf("credential not found")
		}
		log.Printf("Found credential for user %s", cred.UserID)
		dbUser, err := s.db.GetUserByID(ctx, cred.UserID)
		if err != nil {
			return nil, fmt.Errorf("user not found")
		}
		creds, err := s.db.GetCredentialsByUserID(ctx, dbUser.ID)
		if err != nil {
			return nil, fmt.Errorf("getting credentials: %w", err)
		}
		return s.toWebAuthnUser(dbUser, creds), nil
	}

	credential, err := s.webauthn.FinishDiscoverableLogin(handler, *entry.SessionData, r)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "credential not found") || strings.Contains(errMsg, "not found") {
			return nil, fmt.Errorf("passkey_not_found: deze passkey bestaat niet meer. Verwijder hem uit je browser (Instellingen → Wachtwoorden) en registreer opnieuw.")
		}
		if strings.Contains(errMsg, "User handle") || strings.Contains(errMsg, "do not match") {
			return nil, fmt.Errorf("passkey_mismatch: deze passkey hoort bij een ander account. Verwijder oude passkeys voor dit domein uit je browser.")
		}
		return nil, fmt.Errorf("login mislukt: %w", err)
	}

	// Find the user via the credential
	dbCred, err := s.db.GetCredentialByID(ctx, credential.ID)
	if err != nil {
		return nil, fmt.Errorf("credential not found after login")
	}

	s.db.UpdateSignCount(ctx, credential.ID, credential.Authenticator.SignCount)

	return s.db.GetUserByID(ctx, dbCred.UserID)
}

func (s *WebAuthnService) toWebAuthnUser(dbUser *database.User, creds []*database.PasskeyCredential) *webAuthnUser {
	var waCreds []gowebauthn.Credential
	for _, c := range creds {
		var transports []protocol.AuthenticatorTransport
		for _, t := range c.Transport {
			transports = append(transports, protocol.AuthenticatorTransport(t))
		}
		waCreds = append(waCreds, gowebauthn.Credential{
			ID:              c.ID,
			PublicKey:       c.PublicKey,
			AttestationType: c.AttestationType,
			Transport:       transports,
			// Replay the stored flags so the library's BackupEligible
			// immutability check passes for syncable passkeys (iCloud
			// Keychain, Bitwarden, 1Password, Google Password Manager all
			// register with BE=true).
			Flags: gowebauthn.CredentialFlags{
				UserPresent:    c.UserPresent,
				UserVerified:   c.UserVerified,
				BackupEligible: c.BackupEligible,
				BackupState:    c.BackupState,
			},
			Authenticator: gowebauthn.Authenticator{
				AAGUID:    c.AAGUID,
				SignCount: c.SignCount,
			},
		})
	}

	// Use the stored WebAuthn handle — must match what was used during registration
	userID := dbUser.WebAuthnHandle
	if len(userID) == 0 {
		// Fallback for users created before the handle was stored
		userID = []byte(dbUser.ID)
	}

	return &webAuthnUser{
		id:          userID,
		name:        dbUser.Email,
		displayName: dbUser.Name,
		credentials: waCreds,
	}
}

func (s *WebAuthnService) cleanupChallenges() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		now := time.Now()
		s.challenges.Range(func(key, value any) bool {
			entry := value.(*challengeEntry)
			if now.After(entry.ExpiresAt) {
				s.challenges.Delete(key)
			}
			return true
		})
	}
}
