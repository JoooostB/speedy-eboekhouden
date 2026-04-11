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

// SoapAPIHandler provides endpoints backed by the e-boekhouden SOAP API.
type SoapAPIHandler struct {
	db     *database.DB
	encKey crypto.AESKey
}

// NewSoapAPIHandler creates a new SOAP API handler.
func NewSoapAPIHandler(db *database.DB, encKey crypto.AESKey) *SoapAPIHandler {
	return &SoapAPIHandler{db: db, encKey: encKey}
}

// SoapCredentials represents the three values needed for SOAP auth.
type SoapCredentials struct {
	Username      string `json:"username"`
	SecurityCode1 string `json:"securityCode1"`
	SecurityCode2 string `json:"securityCode2"`
}

func (h *SoapAPIHandler) getClient(c *gin.Context) (*eboekhouden.SoapClient, error) {
	sess := session.FromContext(c)
	if sess == nil {
		return nil, errNotAuthenticated
	}

	settings, err := h.db.GetSettings(c.Request.Context(), sess.UserID)
	if err != nil || !settings.HasSoapCredentials {
		return nil, errNoSoapCredentials
	}

	decrypted, err := crypto.Decrypt(h.encKey, settings.SoapCredentialsEnc)
	if err != nil {
		return nil, errDecryptFailed
	}

	var creds SoapCredentials
	if err := json.Unmarshal(decrypted, &creds); err != nil {
		return nil, errDecryptFailed
	}

	return eboekhouden.NewSoapClient(creds.Username, creds.SecurityCode1, creds.SecurityCode2), nil
}

// GetRelaties handles GET /api/v1/soap/relaties
func (h *SoapAPIHandler) GetRelaties(c *gin.Context) {
	client, err := h.getClient(c)
	if err != nil {
		respondSoapError(c, err)
		return
	}
	raw, err := client.GetRelaties(c.DefaultQuery("q", ""))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}

// GetGrootboekrekeningen handles GET /api/v1/soap/grootboekrekeningen
func (h *SoapAPIHandler) GetGrootboekrekeningen(c *gin.Context) {
	client, err := h.getClient(c)
	if err != nil {
		respondSoapError(c, err)
		return
	}
	raw, err := client.GetGrootboekrekeningen()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}

// GetSaldi handles GET /api/v1/soap/saldi
func (h *SoapAPIHandler) GetSaldi(c *gin.Context) {
	client, err := h.getClient(c)
	if err != nil {
		respondSoapError(c, err)
		return
	}
	raw, err := client.GetSaldi(
		c.DefaultQuery("datumVan", ""),
		c.DefaultQuery("datumTot", ""),
		parseInt(c.DefaultQuery("kostenplaatsId", "0")),
	)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}

// GetOpenPosten handles GET /api/v1/soap/openposten
func (h *SoapAPIHandler) GetOpenPosten(c *gin.Context) {
	client, err := h.getClient(c)
	if err != nil {
		respondSoapError(c, err)
		return
	}
	soort := c.DefaultQuery("soort", "Debiteuren") // Debiteuren or Crediteuren
	raw, err := client.GetOpenPosten(soort)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}

// GetMutaties handles GET /api/v1/soap/mutaties
func (h *SoapAPIHandler) GetMutaties(c *gin.Context) {
	client, err := h.getClient(c)
	if err != nil {
		respondSoapError(c, err)
		return
	}
	raw, err := client.GetMutaties(
		c.DefaultQuery("datumVan", ""),
		c.DefaultQuery("datumTot", ""),
		parseInt(c.DefaultQuery("mutatieNr", "0")),
	)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}

// GetArtikelen handles GET /api/v1/soap/artikelen
func (h *SoapAPIHandler) GetArtikelen(c *gin.Context) {
	client, err := h.getClient(c)
	if err != nil {
		respondSoapError(c, err)
		return
	}
	raw, err := client.GetArtikelen()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}

// GetKostenplaatsen handles GET /api/v1/soap/kostenplaatsen
func (h *SoapAPIHandler) GetKostenplaatsen(c *gin.Context) {
	client, err := h.getClient(c)
	if err != nil {
		respondSoapError(c, err)
		return
	}
	raw, err := client.GetKostenplaatsen()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}

var (
	errNotAuthenticated  = fmt.Errorf("not authenticated")
	errNoSoapCredentials = fmt.Errorf("no_soap_credentials")
	errDecryptFailed     = fmt.Errorf("decrypt_failed")
)

func respondSoapError(c *gin.Context, err error) {
	switch err {
	case errNotAuthenticated:
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
	case errNoSoapCredentials:
		c.JSON(http.StatusPreconditionFailed, gin.H{
			"error":   "no_soap_credentials",
			"message": "Stel SOAP API-gegevens in via Instellingen om deze functie te gebruiken.",
		})
	case errDecryptFailed:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Credentials konden niet worden ontsleuteld"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
	}
}
