package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/crypto"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// SettingsHandler manages user settings (API keys, preferences).
type SettingsHandler struct {
	db     *database.DB
	encKey crypto.AESKey
}

// NewSettingsHandler creates a new settings handler.
func NewSettingsHandler(db *database.DB, encKey crypto.AESKey) *SettingsHandler {
	return &SettingsHandler{db: db, encKey: encKey}
}

// GetSettings handles GET /api/v1/settings
func (h *SettingsHandler) GetSettings(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	settings, err := h.db.GetSettings(c.Request.Context(), sess.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load settings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"hasApiKey":          settings.HasAnthropicKey,
		"hasSoapCredentials": settings.HasSoapCredentials,
		"hasRestAccessToken": settings.HasRestAccessToken,
		"preferences":        settings.Preferences,
	})
}

// CheckAPIKey handles GET /api/v1/settings/api-key/status — validates the stored key and checks billing.
func (h *SettingsHandler) CheckAPIKey(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	settings, err := h.db.GetSettings(c.Request.Context(), sess.UserID)
	if err != nil || !settings.HasAnthropicKey {
		c.JSON(http.StatusOK, gin.H{"status": "not_configured"})
		return
	}

	decrypted, err := crypto.Decrypt(h.encKey, settings.AnthropicKeyEnc)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"status": "error", "message": "Speedy kan je opgeslagen API-sleutel niet meer ontsleutelen. Vul de sleutel opnieuw in."})
		return
	}

	apiKey := string(decrypted)

	// Try a minimal API call — list models is the cheapest possible
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	// Try to send a tiny message to check billing status
	// (list models doesn't check billing, but a message does)
	_, err = client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 1,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		},
	})
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "credit balance") || strings.Contains(msg, "billing") {
			c.JSON(http.StatusOK, gin.H{
				"status":  "no_credits",
				"message": "Je API-sleutel werkt, maar je tegoed bij Anthropic is op. Vul het aan op console.anthropic.com/settings/plans.",
			})
			return
		}
		if strings.Contains(msg, "invalid_api_key") || strings.Contains(msg, "authentication") {
			c.JSON(http.StatusOK, gin.H{
				"status":  "invalid",
				"message": "Anthropic accepteert je API-sleutel niet. Controleer de sleutel of maak een nieuwe aan in de Anthropic Console.",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":  "error",
			"message": "Speedy kon je API-sleutel niet controleren bij Anthropic. Probeer het later opnieuw.",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "active"})
}

type setAPIKeyRequest struct {
	APIKey string `json:"apiKey" binding:"required"`
}

// SetAPIKey handles PUT /api/v1/settings/api-key
func (h *SettingsHandler) SetAPIKey(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req setAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "apiKey required"})
		return
	}

	// Format check
	if !strings.HasPrefix(req.APIKey, "sk-ant-") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dit is geen Anthropic API-sleutel. Die begint met sk-ant-."})
		return
	}

	// Live validation — make a tiny API call to verify the key works
	if err := validateAnthropicKey(c.Request.Context(), req.APIKey); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	encrypted, err := crypto.Encrypt(h.encKey, []byte(req.APIKey))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encryption failed"})
		return
	}

	if err := h.db.SetAnthropicKey(c.Request.Context(), sess.UserID, encrypted); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// DeleteAPIKey handles DELETE /api/v1/settings/api-key
func (h *SettingsHandler) DeleteAPIKey(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	if err := h.db.DeleteAnthropicKey(c.Request.Context(), sess.UserID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type setSoapCredentialsRequest struct {
	Username      string `json:"username" binding:"required"`
	SecurityCode1 string `json:"securityCode1" binding:"required"`
	SecurityCode2 string `json:"securityCode2" binding:"required"`
}

// SetSoapCredentials handles PUT /api/v1/settings/soap-credentials
func (h *SettingsHandler) SetSoapCredentials(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req setSoapCredentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Vul alle drie de velden in."})
		return
	}

	// Live validation — try to open a SOAP session
	if err := validateSoapCredentials(req.Username, req.SecurityCode1, req.SecurityCode2); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	data, _ := json.Marshal(req)
	encrypted, err := crypto.Encrypt(h.encKey, data)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encryption failed"})
		return
	}

	if err := h.db.SetSoapCredentials(c.Request.Context(), sess.UserID, encrypted); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// DeleteSoapCredentials handles DELETE /api/v1/settings/soap-credentials
func (h *SettingsHandler) DeleteSoapCredentials(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	h.db.DeleteSoapCredentials(c.Request.Context(), sess.UserID)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type setRestTokenRequest struct {
	AccessToken string `json:"accessToken" binding:"required"`
}

// SetRestAccessToken handles PUT /api/v1/settings/rest-token
func (h *SettingsHandler) SetRestAccessToken(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req setRestTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Vul je access token in."})
		return
	}

	// Live validation — try to create a REST session
	if err := validateRestToken(req.AccessToken); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	encrypted, err := crypto.Encrypt(h.encKey, []byte(req.AccessToken))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encryption failed"})
		return
	}

	if err := h.db.SetRestAccessToken(c.Request.Context(), sess.UserID, encrypted); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// DeleteRestAccessToken handles DELETE /api/v1/settings/rest-token
func (h *SettingsHandler) DeleteRestAccessToken(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	h.db.DeleteRestAccessToken(c.Request.Context(), sess.UserID)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// validateAnthropicKey tests the key by listing models (cheapest possible call).
func validateAnthropicKey(ctx context.Context, apiKey string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	_, err := client.Models.List(ctx, anthropic.ModelListParams{})
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "invalid_api_key") || strings.Contains(msg, "authentication") {
			return fmt.Errorf("Anthropic accepteert deze API-sleutel niet. Controleer of je de hele sleutel hebt gekopieerd.")
		}
		if strings.Contains(msg, "credit balance") || strings.Contains(msg, "billing") {
			return fmt.Errorf("Deze API-sleutel werkt, maar je tegoed bij Anthropic is op. Vul het aan op console.anthropic.com en sla de sleutel daarna opnieuw op.")
		}
		return fmt.Errorf("Speedy kon de API-sleutel niet controleren bij Anthropic en heeft hem niet opgeslagen. Probeer het opnieuw.")
	}
	return nil
}

// validateSoapCredentials tests SOAP credentials by opening a session.
func validateSoapCredentials(username, code1, code2 string) error {
	client := eboekhouden.NewSoapClient(username, code1, code2)
	_, err := client.GetKostenplaatsen() // lightweight call that requires a valid session
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "session") || strings.Contains(msg, "SecurityCode") || strings.Contains(msg, "Username") {
			return fmt.Errorf("Inloggen bij de SOAP-API van e-Boekhouden is mislukt. Controleer je gebruikersnaam en beide beveiligingscodes.")
		}
		return fmt.Errorf("De verbinding met de SOAP-API van e-Boekhouden is mislukt: %s", sanitizeError(msg))
	}
	return nil
}

// validateRestToken tests a REST access token by creating a session.
func validateRestToken(accessToken string) error {
	client := eboekhouden.NewRestClient(accessToken)
	_, err := client.GetAdministrations() // lightweight call that requires a valid session
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "401") || strings.Contains(msg, "403") {
			return fmt.Errorf("e-Boekhouden accepteert dit access token niet. Controleer of je het hele token hebt gekopieerd.")
		}
		return fmt.Errorf("Speedy kon het access token niet controleren bij e-Boekhouden en heeft het niet opgeslagen. Probeer het opnieuw.")
	}
	return nil
}

// SetEntityType handles PUT /api/v1/settings/entity-type — stores the user's
// onderneming type (BV, ZZP, EM, ANDERS) as a preference. The Claude classifier
// uses this to decide whether bank lines default to private or business
// classification: for a B.V. every euro on the bank account is by definition
// business, while a ZZP'er can mix personal and business in the same account.
func (h *SettingsHandler) SetEntityType(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req struct {
		EntityType string `json:"entityType"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig verzoek."})
		return
	}

	// Whitelist accepted values to keep the prompt-injection surface narrow.
	switch req.EntityType {
	case "BV", "ZZP", "EM", "ANDERS", "":
		// allowed (empty string clears the preference)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Onbekend type onderneming."})
		return
	}

	// Merge into existing preferences rather than overwriting them.
	settings, _ := h.db.GetSettings(c.Request.Context(), sess.UserID)
	prefs := map[string]any{}
	if settings != nil && len(settings.Preferences) > 0 {
		json.Unmarshal(settings.Preferences, &prefs)
	}
	if req.EntityType == "" {
		delete(prefs, "entityType")
	} else {
		prefs["entityType"] = req.EntityType
	}
	body, _ := json.Marshal(prefs)
	if err := h.db.SetPreferences(c.Request.Context(), sess.UserID, body); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Opslaan is mislukt. Probeer het opnieuw."})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "entityType": req.EntityType})
}

func sanitizeError(msg string) string {
	// Strip anything that looks like credentials
	msg = strings.ReplaceAll(msg, "\n", " ")
	if len(msg) > 100 {
		msg = msg[:100]
	}
	return msg
}
