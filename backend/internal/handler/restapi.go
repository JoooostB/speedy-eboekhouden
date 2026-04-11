package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/crypto"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// RestAPIHandler provides endpoints backed by the e-boekhouden REST API.
type RestAPIHandler struct {
	db     *database.DB
	encKey crypto.AESKey
}

// NewRestAPIHandler creates a new REST API handler.
func NewRestAPIHandler(db *database.DB, encKey crypto.AESKey) *RestAPIHandler {
	return &RestAPIHandler{db: db, encKey: encKey}
}

func (h *RestAPIHandler) getClient(c *gin.Context) (*eboekhouden.RestClient, error) {
	sess := session.FromContext(c)
	if sess == nil {
		return nil, errNotAuthenticated
	}

	settings, err := h.db.GetSettings(c.Request.Context(), sess.UserID)
	if err != nil || !settings.HasRestAccessToken {
		return nil, fmt.Errorf("no_rest_credentials")
	}

	decrypted, err := crypto.Decrypt(h.encKey, settings.RestAccessTokenEnc)
	if err != nil {
		return nil, errDecryptFailed
	}

	return eboekhouden.NewRestClient(string(decrypted)), nil
}

// GetInvoices handles GET /api/v1/rest/invoices
func (h *RestAPIHandler) GetInvoices(c *gin.Context) {
	client, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "no_rest_credentials", "message": "Stel REST API-gegevens in via Instellingen."})
		return
	}
	raw, err := client.GetInvoices(parseInt(c.DefaultQuery("limit", "100")), parseInt(c.DefaultQuery("offset", "0")))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}

// CreateInvoice handles POST /api/v1/rest/invoices
func (h *RestAPIHandler) CreateInvoice(c *gin.Context) {
	client, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "no_rest_credentials", "message": "Stel REST API-gegevens in via Instellingen."})
		return
	}
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	raw, err := client.CreateInvoice(json.RawMessage(body))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}

// GetCostCenters handles GET /api/v1/rest/costcenters
func (h *RestAPIHandler) GetCostCenters(c *gin.Context) {
	client, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "no_rest_credentials", "message": "Stel REST API-gegevens in via Instellingen."})
		return
	}
	raw, err := client.GetCostCenters()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}

// GetEmailTemplates handles GET /api/v1/rest/emailtemplates
func (h *RestAPIHandler) GetEmailTemplates(c *gin.Context) {
	client, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "no_rest_credentials", "message": "Stel REST API-gegevens in via Instellingen."})
		return
	}
	raw, err := client.GetEmailTemplates()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}
