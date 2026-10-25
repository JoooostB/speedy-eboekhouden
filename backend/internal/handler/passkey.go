package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/auth"
	"github.com/joooostb/speedy-eboekhouden/internal/config"
	"github.com/joooostb/speedy-eboekhouden/internal/crypto"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/mail"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
	"github.com/joooostb/speedy-eboekhouden/internal/storage"
)

// PasskeyHandler handles WebAuthn registration and login.
type PasskeyHandler struct {
	webauthn *auth.WebAuthnService
	sessions *session.Store
	config   *config.Config
	db       *database.DB
	encKey   crypto.AESKey
	mailer   *mail.Mailer
	r2       *storage.Client
	// startRecovery runs the work behind a recovery request (account lookup,
	// token, mail) after RecoverRequest has answered; by default
	// sendRecoveryMail in a goroutine. Tests replace it.
	startRecovery func(email string)
}

// NewPasskeyHandler creates a new passkey handler.
func NewPasskeyHandler(wa *auth.WebAuthnService, sessions *session.Store, cfg *config.Config, db *database.DB, encKey crypto.AESKey, mailer *mail.Mailer, r2 *storage.Client) *PasskeyHandler {
	h := &PasskeyHandler{webauthn: wa, sessions: sessions, config: cfg, db: db, encKey: encKey, mailer: mailer, r2: r2}
	h.startRecovery = func(email string) { go h.sendRecoveryMail(email) }
	return h
}

type registerBeginRequest struct {
	Email string `json:"email" binding:"required,email,max=254"`
	Name  string `json:"name" binding:"required,max=100"`
}

// RegisterBegin handles POST /api/v1/auth/register/begin
func (h *PasskeyHandler) RegisterBegin(c *gin.Context) {
	var req registerBeginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and name required"})
		return
	}

	// Check if email is already registered
	existing, _ := h.webauthn.DB().GetUserByEmail(c.Request.Context(), req.Email)
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Er bestaat al een account met dit e-mailadres. Log in met je passkey."})
		return
	}

	options, challengeID, err := h.webauthn.BeginRegistration(c.Request.Context(), req.Email, req.Name)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"options":     rawJSON(options),
		"challengeId": challengeID,
	})
}

// RegisterFinish handles POST /api/v1/auth/register/finish
func (h *PasskeyHandler) RegisterFinish(c *gin.Context) {
	challengeID := c.GetHeader("X-Challenge-ID")
	if challengeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Challenge-ID header required"})
		return
	}

	user, err := h.webauthn.FinishRegistration(c.Request.Context(), challengeID, c.Request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Create session
	sess, err := h.sessions.Create(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session creation failed"})
		return
	}

	// Set first team as active
	teams, _ := h.webauthn.DB().GetTeamsByUserID(c.Request.Context(), user.ID)
	if len(teams) > 0 {
		sess.TeamID = teams[0].ID
		h.sessions.Update(c.Request.Context(), sess)
	}

	h.setSessionCookie(c, sess.ID)

	// Send welcome email async (non-blocking)
	if h.mailer != nil {
		go func(userID, email, name string) {
			if err := h.mailer.SendWelcomeEmail(email, name, h.config.WebAuthnOrigin+"/app/"); err != nil {
				log.Printf("Registration: welcome mail for user %s not sent: %s", userID, mail.ErrClass(err))
			}
		}(user.ID, user.Email, user.Name)
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"user":   user,
	})
}

// LoginBegin handles POST /api/v1/auth/login/begin
func (h *PasskeyHandler) LoginBegin(c *gin.Context) {
	options, challengeID, err := h.webauthn.BeginLogin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"options":     rawJSON(options),
		"challengeId": challengeID,
	})
}

// LoginFinish handles POST /api/v1/auth/login/finish
func (h *PasskeyHandler) LoginFinish(c *gin.Context) {
	challengeID := c.GetHeader("X-Challenge-ID")
	if challengeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Challenge-ID header required"})
		return
	}

	user, err := h.webauthn.FinishLogin(c.Request.Context(), challengeID, c.Request)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// Create session
	sess, err := h.sessions.Create(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session creation failed"})
		return
	}

	// Set first team as active
	teams, _ := h.webauthn.DB().GetTeamsByUserID(c.Request.Context(), user.ID)
	if len(teams) > 0 {
		sess.TeamID = teams[0].ID
	}

	// Restore e-boekhouden token from PostgreSQL (survives logout/login cycles)
	h.restoreEBToken(c, sess)

	h.sessions.Update(c.Request.Context(), sess)
	h.setSessionCookie(c, sess.ID)

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"user":   user,
	})
}

// restoreEBToken attempts to load a previously stored e-boekhouden token from PostgreSQL.
func (h *PasskeyHandler) restoreEBToken(c *gin.Context, sess *session.SessionData) {
	settings, err := h.db.GetSettings(c.Request.Context(), sess.UserID)
	if err != nil || !settings.HasEBToken {
		return
	}
	decrypted, err := crypto.Decrypt(h.encKey, settings.EBTokenEnc)
	if err != nil {
		return
	}
	sess.EBoekhoudenToken = string(decrypted)
}

// Logout handles POST /api/v1/auth/logout
func (h *PasskeyHandler) Logout(c *gin.Context) {
	cookie, err := c.Cookie(session.CookieName)
	if err == nil && cookie != "" {
		h.sessions.Delete(c.Request.Context(), cookie)
	}

	c.SetCookie(session.CookieName, "", -1, "/", h.config.CookieDomain, h.config.CookieSecure, true)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Me handles GET /api/v1/auth/me
func (h *PasskeyHandler) Me(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	user, err := h.webauthn.DB().GetUserByID(c.Request.Context(), sess.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user not found"})
		return
	}

	var team *gin.H
	if sess.TeamID != "" {
		t, err := h.webauthn.DB().GetTeamByID(c.Request.Context(), sess.TeamID)
		if err == nil {
			team = &gin.H{"id": t.ID, "name": t.Name}
		}
	}

	var avatarURL string
	if user.AvatarKey != "" && h.r2 != nil {
		avatarURL = h.r2.PublicObjectURL(user.AvatarKey)
	}

	c.JSON(http.StatusOK, gin.H{
		"user":                 user,
		"team":                 team,
		"eboekhoudenConnected": sess.EBoekhoudenToken != "",
		"avatarUrl":            avatarURL,
	})
}

func (h *PasskeyHandler) setSessionCookie(c *gin.Context, sessionID string) {
	session.SetCookie(c, sessionID, time.Duration(h.config.SessionMaxAge)*time.Minute,
		session.CookieOptions{Domain: h.config.CookieDomain, Secure: h.config.CookieSecure})
}

// RecoverRequest handles POST /api/v1/auth/recover — sends a recovery email.
func (h *PasskeyHandler) RecoverRequest(c *gin.Context) {
	if h.mailer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Deze server kan geen e-mail versturen, dus ook geen herstellink."})
		return
	}

	var req struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Vul een geldig e-mailadres in."})
		return
	}

	// Answer the same for every address, and before anything about the
	// address is looked up: the lookup, the token and the mail run in the
	// background (sendRecoveryMail). Neither the answer nor its timing then
	// tells a caller whether the address has an account, also not during an
	// SMTP outage. The frontend words the success conditionally ("Als er een
	// account bestaat…"), so it does not promise a mail. A server without
	// mailer at all is a server-wide condition and is reported above.
	h.startRecovery(req.Email)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// recoveryWorkTimeout bounds the database part of sendRecoveryMail; the
// mail itself has the mailer's own timeouts.
const recoveryWorkTimeout = time.Minute

// sendRecoveryMail looks up the account of a recovery request, stores a
// recovery token and mails the link. It runs after RecoverRequest has
// answered, so it has its own context. An address without an account ends
// here silently. Failures are logged by user id and error class only: never
// the address, and never the SMTP reply text, which often echoes it.
func (h *PasskeyHandler) sendRecoveryMail(email string) {
	ctx, cancel := context.WithTimeout(context.Background(), recoveryWorkTimeout)
	defer cancel()

	user, err := h.db.GetUserByEmail(ctx, email)
	if err != nil {
		return
	}
	token, err := h.db.CreateRecoveryToken(ctx, user.ID)
	if err != nil {
		log.Printf("Recovery: storing a token for user %s failed: %v", user.ID, err)
		return
	}
	recoveryURL := h.config.WebAuthnOrigin + "/app/herstel?token=" + token
	if err := h.mailer.SendRecoveryEmail(user.Email, user.Name, recoveryURL); err != nil {
		log.Printf("Recovery: mail for user %s not sent: %s", user.ID, mail.ErrClass(err))
	}
}

// RecoverFinishBegin handles POST /api/v1/auth/recover/begin — starts passkey registration with a recovery token.
func (h *PasskeyHandler) RecoverFinishBegin(c *gin.Context) {
	var req struct {
		Token string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "De herstellink is onvolledig. Open de link uit de e-mail opnieuw."})
		return
	}

	userID, err := h.db.ValidateRecoveryToken(c.Request.Context(), req.Token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Deze herstellink is ongeldig, verlopen of al gebruikt. Vraag een nieuwe aan."})
		return
	}

	user, err := h.db.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Het account bij deze herstellink bestaat niet meer."})
		return
	}

	// The challenge is bound to this account and the new passkey gets the
	// account's own WebAuthn handle (see BeginRecoveryRegistration).
	options, challengeID, err := h.webauthn.BeginRecoveryRegistration(c.Request.Context(), user)
	if err != nil {
		log.Printf("Recovery: starting the ceremony for user %s failed: %v", user.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": msgRecoveryStartFailed})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"options":     rawJSON(options),
		"challengeId": challengeID,
		"userId":      userID,
	})
}

// Dutch answers of the recovery ceremony. The recovery token is consumed by
// RecoverFinishBegin, so every failure after it asks for a new link; the
// request form sits below the message on the recovery page.
const (
	msgRecoveryStartFailed = "Speedy kon het herstel niet starten. Vraag hieronder een nieuwe herstellink aan."
	msgRecoveryIncomplete  = "De herstelaanvraag is onvolledig. Vraag hieronder een nieuwe herstellink aan."
	msgRecoveryExpired     = "Het herstel duurde te lang. Vraag hieronder een nieuwe herstellink aan."
	msgRecoveryInvalid     = "Deze herstelaanvraag is ongeldig. Vraag hieronder een nieuwe herstellink aan."
	msgRecoveryNotCreated  = "Je nieuwe passkey kon niet worden aangemaakt. Vraag hieronder een nieuwe herstellink aan."
)

// RecoverFinishComplete handles POST /api/v1/auth/recover/finish — completes passkey registration with a recovery token.
//
// The account comes from the challenge, which RecoverFinishBegin bound to
// the account of the recovery token. X-User-ID is still sent by the
// frontend but only checked against it: trusting the header let anyone
// with any registration challenge add a passkey to another account.
func (h *PasskeyHandler) RecoverFinishComplete(c *gin.Context) {
	challengeID := c.GetHeader("X-Challenge-ID")
	if challengeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msgRecoveryIncomplete})
		return
	}

	credential, userID, err := h.webauthn.FinishRecoveryRegistration(c.Request.Context(), challengeID, c.Request)
	switch {
	case errors.Is(err, auth.ErrChallengeExpired):
		c.JSON(http.StatusBadRequest, gin.H{"error": msgRecoveryExpired})
		return
	case errors.Is(err, auth.ErrWrongCeremony):
		log.Printf("Recovery: finish refused a challenge from another ceremony")
		c.JSON(http.StatusBadRequest, gin.H{"error": msgRecoveryInvalid})
		return
	case err != nil:
		// A WebAuthn protocol error (origin, challenge, attestation); it
		// carries no personal data.
		log.Printf("Recovery: ceremony failed: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": msgRecoveryNotCreated})
		return
	}
	if claimed := c.GetHeader("X-User-ID"); claimed != "" && claimed != userID {
		log.Printf("Recovery: finish for user %s carried another X-User-ID; refused", userID)
		c.JSON(http.StatusBadRequest, gin.H{"error": msgRecoveryInvalid})
		return
	}

	user, err := h.db.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Het account bij deze herstellink bestaat niet meer."})
		return
	}

	// Store the new credential for the existing user
	var transports []string
	for _, t := range credential.Transport {
		transports = append(transports, string(t))
	}
	dbCred := &database.PasskeyCredential{
		ID:              credential.ID,
		UserID:          user.ID,
		PublicKey:       credential.PublicKey,
		AttestationType: string(credential.AttestationType),
		AAGUID:          credential.Authenticator.AAGUID,
		Transport:       transports,
		SignCount:       credential.Authenticator.SignCount,
		FriendlyName:    "Herstelde passkey",
		BackupEligible:  credential.Flags.BackupEligible,
		BackupState:     credential.Flags.BackupState,
		UserPresent:     credential.Flags.UserPresent,
		UserVerified:    credential.Flags.UserVerified,
	}
	if err := h.db.StoreCredential(c.Request.Context(), dbCred); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "De nieuwe passkey kon niet worden opgeslagen. Vraag een nieuwe herstellink aan."})
		return
	}

	// Create session
	sess, err := h.sessions.Create(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Je nieuwe passkey is opgeslagen, maar Speedy kon je niet inloggen. Log in met je nieuwe passkey."})
		return
	}

	teams, _ := h.db.GetTeamsByUserID(c.Request.Context(), user.ID)
	if len(teams) > 0 {
		sess.TeamID = teams[0].ID
	}
	h.restoreEBToken(c, sess)
	h.sessions.Update(c.Request.Context(), sess)
	h.setSessionCookie(c, sess.ID)

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"user":   user,
	})
}

// rawJSON wraps a json.RawMessage for proper Gin serialization.
type rawJSON []byte

func (r rawJSON) MarshalJSON() ([]byte, error) { return r, nil }
