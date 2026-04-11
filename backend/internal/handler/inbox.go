package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/joooostb/speedy-eboekhouden/internal/crypto"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
	"github.com/joooostb/speedy-eboekhouden/internal/storage"
	"github.com/redis/go-redis/v9"
)

type inboxClassifiedRow struct {
	ID             int     `json:"id"`
	Datum          string  `json:"datum"`
	Bedrag         float64 `json:"bedrag"`
	Omschrijving   string  `json:"omschrijving"`
	Rekening       string  `json:"rekening"`
	GrootboekId    int     `json:"grootboekId"`
	Category       string  `json:"category"`
	NeedsInvoice   bool    `json:"needsInvoice"`
	Confidence     float64 `json:"confidence"`
	Grootboekcode  string  `json:"grootboekcode"`
	BTWCode        string  `json:"btwCode"`
	Soort          string  `json:"soort"`
	AIOmschrijving string  `json:"aiOmschrijving"`
	Indicator      string  `json:"indicator"`
}

// InboxHandler handles the bookkeeping inbox workflow.
type InboxHandler struct {
	claude *claude.Service
	db     *database.DB
	encKey crypto.AESKey
	redis  *redis.Client
	r2     *storage.Client
}

// NewInboxHandler creates a new inbox handler.
func NewInboxHandler(claudeSvc *claude.Service, db *database.DB, encKey crypto.AESKey, redisClient *redis.Client, r2 *storage.Client) *InboxHandler {
	return &InboxHandler{claude: claudeSvc, db: db, encKey: encKey, redis: redisClient, r2: r2}
}

// ClassifyBatch handles POST /api/v1/inbox/classify — classifies all unprocessed bank lines with AI.
func (h *InboxHandler) ClassifyBatch(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	// Check for cached classifications (skip if ?force=true)
	cacheKey := fmt.Sprintf("inbox:classify:%s", sess.UserID)
	if c.Query("force") != "true" {
		if cached, err := h.redis.Get(c.Request.Context(), cacheKey).Bytes(); err == nil {
			c.Data(http.StatusOK, "application/json", cached)
			return
		}
	} else {
		// Force refresh — delete the cache
		h.redis.Del(c.Request.Context(), cacheKey)
	}

	// Get API key
	apiKey, err := h.getAPIKey(c, sess.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no_api_key", "message": "Stel eerst een Anthropic API-sleutel in"})
		return
	}

	// Fetch unprocessed lines
	raw, err := client.GetImportGrid(0, 2000)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	rows, totalCount, err := eboekhouden.ParseImportGrid(raw)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse bank statements"})
		return
	}

	if totalCount == 0 {
		c.JSON(http.StatusOK, gin.H{"classifications": []any{}, "totalCount": 0})
		return
	}

	// Convert to batch lines
	var lines []claude.BatchLine
	for _, row := range rows {
		id, _ := toInt(row["id"])
		bedrag, _ := toFloat(row["mutBedrag"])
		lines = append(lines, claude.BatchLine{
			ID:            id,
			Datum:         toString(row["mutDatum"]),
			Bedrag:        bedrag,
			Omschrijving:  toString(row["mutOmschrijving"]),
			Tegenrekening: toString(row["rekening"]),
		})
	}

	// Classify with AI
	results, err := h.claude.ClassifyBatch(c.Request.Context(), apiKey, lines)
	if err != nil {
		log.Printf("Claude API error for user %s: %v", sess.UserID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "claude_error", "message": err.Error()})
		return
	}

	// Merge classifications with original row data
	classMap := make(map[int]claude.BatchClassifyResult)
	for _, r := range results {
		classMap[r.ID] = r
	}

	var classified []inboxClassifiedRow
	for _, row := range rows {
		id, _ := toInt(row["id"])
		bedrag, _ := toFloat(row["mutBedrag"])
		grootboekId, _ := toInt(row["grootboekId"])

		cr := inboxClassifiedRow{
			ID:           id,
			Datum:        toString(row["mutDatum"]),
			Bedrag:       bedrag,
			Omschrijving: toString(row["mutOmschrijving"]),
			Rekening:     toString(row["rekening"]),
			GrootboekId:  grootboekId,
			Category:     "manual",
			Confidence:   0,
		}

		if cl, ok := classMap[id]; ok {
			cr.Category = cl.Category
			cr.NeedsInvoice = cl.NeedsInvoice
			cr.Confidence = cl.Confidence
			cr.Grootboekcode = cl.Grootboekcode
			cr.BTWCode = cl.BTWCode
			cr.Soort = cl.Soort
			cr.AIOmschrijving = cl.Omschrijving
			cr.Indicator = cl.Indicator
		}

		classified = append(classified, cr)
	}

	response := gin.H{
		"classifications": classified,
		"totalCount":      totalCount,
		"summary": gin.H{
			"auto":    countCategory(classified, "auto"),
			"review":  countCategory(classified, "review"),
			"invoice": countCategory(classified, "invoice"),
			"manual":  countCategory(classified, "manual"),
		},
	}

	// Cache for 1 hour
	if data, err := json.Marshal(response); err == nil {
		h.redis.Set(c.Request.Context(), cacheKey, data, 1*time.Hour)
	}

	c.JSON(http.StatusOK, response)
}

// DashboardSummary handles GET /api/v1/inbox/summary — aggregated dashboard data.
func (h *InboxHandler) DashboardSummary(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	client := session.ClientFromContext(c)
	summary := gin.H{}

	// Bank statement count
	if client != nil {
		count, err := client.GetImportCount()
		if err == nil {
			summary["unprocessedCount"] = count
		}

		// Check for cached classifications summary
		cacheKey := fmt.Sprintf("inbox:classify:%s", sess.UserID)
		if cached, err := h.redis.Get(c.Request.Context(), cacheKey).Bytes(); err == nil {
			var cached_data struct {
				Summary map[string]int `json:"summary"`
			}
			if json.Unmarshal(cached, &cached_data) == nil && cached_data.Summary != nil {
				summary["classificationSummary"] = cached_data.Summary
			}
		}
	}

	// Open items (if SOAP configured)
	settings, _ := h.db.GetSettings(c.Request.Context(), sess.UserID)
	if settings != nil && settings.HasSoapCredentials {
		h.addOpenItemsSummary(c.Request.Context(), sess.UserID, settings, summary)
	}

	// Settings status
	summary["hasApiKey"] = settings != nil && settings.HasAnthropicKey
	summary["hasSoap"] = settings != nil && settings.HasSoapCredentials
	summary["hasRest"] = settings != nil && settings.HasRestAccessToken
	summary["eboekhoudenConnected"] = client != nil

	c.JSON(http.StatusOK, summary)
}

func (h *InboxHandler) addOpenItemsSummary(ctx context.Context, userID string, settings *database.UserSettings, summary gin.H) {
	decrypted, err := crypto.Decrypt(h.encKey, settings.SoapCredentialsEnc)
	if err != nil {
		return
	}
	var creds SoapCredentials
	if json.Unmarshal(decrypted, &creds) != nil {
		return
	}
	soapClient := eboekhouden.NewSoapClient(creds.Username, creds.SecurityCode1, creds.SecurityCode2)

	// Overdue open items
	raw, err := soapClient.GetOpenPosten("Crediteuren")
	if err != nil {
		return
	}
	var posts []map[string]any
	json.Unmarshal(raw, &posts)

	overdueCount := 0
	overdueTotal := 0.0
	now := time.Now()
	for _, p := range posts {
		if vd, ok := p["vervalDatum"].(string); ok && vd != "" {
			if t, err := time.Parse("2006-01-02T15:04:05", vd); err == nil && t.Before(now) {
				overdueCount++
				if amt, ok := p["openstaand"].(float64); ok {
					overdueTotal += amt
				}
			}
		}
	}
	summary["overdueCount"] = overdueCount
	summary["overdueTotal"] = overdueTotal
}

// MatchInvoice handles POST /api/v1/inbox/:id/match-invoice — upload PDF, match with bank line.
func (h *InboxHandler) MatchInvoice(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	bankLineID := parseInt(c.Param("id"))

	// Get API key
	apiKey, err := h.getAPIKey(c, sess.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no_api_key", "message": "Stel eerst een Anthropic API-sleutel in"})
		return
	}

	// Read bank line details from the request body
	var bankLine struct {
		Bedrag       float64 `json:"bedrag"`
		Omschrijving string  `json:"omschrijving"`
		Datum        string  `json:"datum"`
		GrootboekId  int     `json:"grootboekId"`
	}

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "PDF-bestand is verplicht"})
		return
	}
	defer file.Close()

	// Read bank line metadata from form fields
	bankLine.Bedrag = parseFloat(c.PostForm("bedrag"))
	bankLine.Omschrijving = c.PostForm("omschrijving")
	bankLine.Datum = c.PostForm("datum")
	bankLine.GrootboekId = parseInt(c.PostForm("grootboekId"))

	const maxFileSize = 10 << 20
	pdfBytes, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil || len(pdfBytes) > maxFileSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Bestand is te groot (max 10 MB)"})
		return
	}

	pdfBase64 := base64.StdEncoding.EncodeToString(pdfBytes)

	// Read invoice with Claude (no account list in inbox context — uses defaults)
	invoice, err := h.claude.ReadInvoice(c.Request.Context(), apiKey, pdfBase64, nil)
	if err != nil {
		log.Printf("Claude API error for user %s: %v", sess.UserID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "claude_error", "message": err.Error()})
		return
	}

	// Upload PDF to R2
	var uploadKey string
	if h.r2 != nil {
		uploadKey = fmt.Sprintf("uploads/%s/document.pdf", uuid.New().String())
		h.r2.Upload(c.Request.Context(), uploadKey, pdfBytes, "application/pdf")
	}

	// Verify match between invoice and bank line
	amountMatch := false
	amountDiff := 0.0
	if invoice.BedragInclBTW > 0 {
		amountDiff = abs(abs(bankLine.Bedrag) - invoice.BedragInclBTW)
		amountMatch = amountDiff < 0.02 // within 2 cents
	}

	c.JSON(http.StatusOK, gin.H{
		"invoice":     invoice,
		"uploadKey":   uploadKey,
		"bankLineId":  bankLineID,
		"amountMatch": amountMatch,
		"amountDiff":  amountDiff,
	})
}

// BatchProcess handles POST /api/v1/inbox/process-batch — processes multiple lines at once.
func (h *InboxHandler) BatchProcess(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	sess := session.FromContext(c)

	var req struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	const maxBatchItems = 100
	if len(req.Items) > maxBatchItems {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Maximaal %d regels per batch", maxBatchItems)})
		return
	}

	type result struct {
		ID     int    `json:"id"`
		Status string `json:"status"` // ok or error
		Error  string `json:"error,omitempty"`
		MutNr  string `json:"mutNr,omitempty"`
	}

	var results []result
	for _, item := range req.Items {
		raw, err := client.CreateMutatie(item)
		if err != nil {
			results = append(results, result{Status: "error", Error: err.Error()})
			continue
		}
		var resp struct {
			MutNr int `json:"mutNr"`
		}
		json.Unmarshal(raw, &resp)
		results = append(results, result{Status: "ok", MutNr: fmt.Sprintf("%d", resp.MutNr)})
	}

	// Invalidate classification cache
	if sess != nil {
		h.redis.Del(c.Request.Context(), fmt.Sprintf("inbox:classify:%s", sess.UserID))
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

func (h *InboxHandler) getAPIKey(c *gin.Context, userID string) (string, error) {
	settings, err := h.db.GetSettings(c.Request.Context(), userID)
	if err != nil || !settings.HasAnthropicKey {
		return "", fmt.Errorf("no API key")
	}
	decrypted, err := crypto.Decrypt(h.encKey, settings.AnthropicKeyEnc)
	if err != nil {
		return "", err
	}
	return string(decrypted), nil
}

func countCategory(rows []inboxClassifiedRow, cat string) int {
	count := 0
	for _, r := range rows {
		if r.Category == cat {
			count++
		}
	}
	return count
}

func toInt(v any) (int, bool) {
	switch val := v.(type) {
	case float64:
		return int(val), true
	case int:
		return val, true
	case string:
		i := parseInt(val)
		return i, i != 0
	}
	return 0, false
}

func toFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case string:
		f := parseFloat(val)
		return f, f != 0
	}
	return 0, false
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
