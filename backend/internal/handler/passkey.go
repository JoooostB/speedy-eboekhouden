package handler

import (
	"net/http"

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
}

// NewPasskeyHandler creates a new passkey handler.
func NewPasskeyHandler(wa *auth.WebAuthnService, sessions *session.Store, cfg *config.Config, db *database.DB, encKey crypto.AESKey, mailer *mail.Mailer, r2 *storage.Client) *PasskeyHandler {
	return &PasskeyHandler{webauthn: wa, sessions: sessions, config: cfg, db: db, encKey: encKey, mailer: mailer, r2: r2}
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
		c.JSON(http.StatusConflict, gin.H{"error": "E-mailadres is al geregistreerd"})
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
		go h.mailer.SendWelcomeEmail(user.Email, user.Name, h.config.WebAuthnOrigin+"/app/")
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
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		session.CookieName,
		sessionID,
		h.config.SessionMaxAge*60,
		"/",
		h.config.CookieDomain,
		h.config.CookieSecure,
		true,
	)
}

// RecoverRequest handles POST /api/v1/auth/recover — sends a recovery email.
func (h *PasskeyHandler) RecoverRequest(c *gin.Context) {
	if h.mailer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "E-mail is niet geconfigureerd"})
		return
	}

	var req struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "E-mailadres is verplicht"})
		return
	}

	// Always return success to avoid email enumeration
	user, err := h.db.GetUserByEmail(c.Request.Context(), req.Email)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	token, err := h.db.CreateRecoveryToken(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	recoveryURL := h.config.WebAuthnOrigin + "/app/herstel?token=" + token
	h.mailer.SendRecoveryEmail(user.Email, user.Name, recoveryURL)

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// RecoverFinishBegin handles POST /api/v1/auth/recover/begin — starts passkey registration with a recovery token.
func (h *PasskeyHandler) RecoverFinishBegin(c *gin.Context) {
	var req struct {
		Token string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token is verplicht"})
		return
	}

	userID, err := h.db.ValidateRecoveryToken(c.Request.Context(), req.Token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldige of verlopen link. Vraag een nieuwe aan."})
		return
	}

	user, err := h.db.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gebruiker niet gevonden"})
		return
	}

	options, challengeID, err := h.webauthn.BeginRegistration(c.Request.Context(), user.Email, user.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"options":     rawJSON(options),
		"challengeId": challengeID,
		"userId":      userID,
	})
}

// RecoverFinishComplete handles POST /api/v1/auth/recover/finish — completes passkey registration with a recovery token.
func (h *PasskeyHandler) RecoverFinishComplete(c *gin.Context) {
	challengeID := c.GetHeader("X-Challenge-ID")
	userID := c.GetHeader("X-User-ID")
	if challengeID == "" || userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Challenge-ID and X-User-ID headers required"})
		return
	}

	// FinishRegistration expects a new user flow, but we need to register a credential for an existing user.
	// We use the WebAuthn service's BeginRegistration/FinishRegistration which creates a new user —
	// but since the email already exists, CreateUser will fail. We need a different approach.
	// Let's directly finish the WebAuthn ceremony and add the credential to the existing user.
	user, err := h.db.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Gebruiker niet gevonden"})
		return
	}

	// The WebAuthn FinishRegistration will try to create a new user and fail on duplicate email.
	// Instead, we need to handle this at the auth service level.
	// For now, use a workaround: the recovery begin already validated the token and returned the userID.
	// The finish just needs to store the new credential.
	// Since FinishRegistration in auth/webauthn.go creates a user, we need a separate method.
	credential, err := h.webauthn.FinishRecoveryRegistration(c.Request.Context(), challengeID, c.Request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	}
	if err := h.db.StoreCredential(c.Request.Context(), dbCred); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Passkey opslaan mislukt"})
		return
	}

	// Create session
	sess, err := h.sessions.Create(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Sessie aanmaken mislukt"})
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
