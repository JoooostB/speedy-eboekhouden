package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	// Learned is true when this row was filled in from the user's learned
	// classification memory rather than from a fresh Claude call. The UI
	// shows a small "eerder geboekt" badge so the user knows where the
	// suggestion came from.
	Learned bool `json:"learned"`
}

// InboxHandler handles the bookkeeping inbox workflow.
type InboxHandler struct {
	claude   *claude.Service
	db       *database.DB
	encKey   crypto.AESKey
	redis    *redis.Client
	r2       *storage.Client
	sessions *session.Store
}

// NewInboxHandler creates a new inbox handler.
func NewInboxHandler(claudeSvc *claude.Service, db *database.DB, encKey crypto.AESKey, redisClient *redis.Client, r2 *storage.Client, sessions *session.Store) *InboxHandler {
	return &InboxHandler{claude: claudeSvc, db: db, encKey: encKey, redis: redisClient, r2: r2, sessions: sessions}
}

// handleEBError is a thin instance wrapper around HandleEBoekhoudenSessionExpired.
func (h *InboxHandler) handleEBError(c *gin.Context, sess *session.SessionData, err error) bool {
	return HandleEBoekhoudenSessionExpired(c, sess, h.sessions, h.db, err)
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
		if h.handleEBError(c, sess, err) {
			return
		}
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

	// Pre-fetch learned mappings for this user. Lines whose normalized
	// signal matches a confirmed mapping are auto-classified locally and
	// never reach Claude — saves API tokens and gives the user instant
	// feedback for recurring transactions they've already booked once.
	learned, _ := h.db.LookupLearnedBatch(c.Request.Context(), sess.UserID)

	// Convert to batch lines, but skip lines that have a confirmed learned
	// mapping (count >= 2 with confirmed_at set).
	var lines []claude.BatchLine
	learnedHits := make(map[int]*database.LearnedClassification)
	for _, row := range rows {
		id, _ := toInt(row["id"])
		bedrag, _ := toFloat(row["mutBedrag"])
		omschrijving := toString(row["mutOmschrijving"])
		tegenrekening := toString(row["rekening"])

		signal := database.BuildClassificationSignal(omschrijving, tegenrekening)
		if hit, ok := learned[signal]; ok && hit.ConfirmedAt != nil {
			learnedHits[id] = hit
			continue
		}

		lines = append(lines, claude.BatchLine{
			ID:            id,
			Datum:         toString(row["mutDatum"]),
			Bedrag:        bedrag,
			Omschrijving:  omschrijving,
			Tegenrekening: tegenrekening,
		})
	}

	// Classify with AI — only the lines that didn't hit the learned cache.
	// Pass the user's entity type so the prompt can apply BV-vs-ZZP rules
	// (e.g. investments on a B.V. account are business assets, not private).
	entityType := readEntityType(c.Request.Context(), h.db, sess.UserID)
	var results []claude.BatchClassifyResult
	if len(lines) > 0 {
		results, err = h.claude.ClassifyBatch(c.Request.Context(), apiKey, entityType, lines)
		if err != nil {
			log.Printf("Claude API error for user %s: %v", sess.UserID, err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "claude_error", "message": err.Error()})
			return
		}
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

		if hit, ok := learnedHits[id]; ok {
			// Confirmed learned mapping wins outright — high confidence,
			// "auto" category, and the indicator tells the user why.
			cr.Category = "auto"
			cr.NeedsInvoice = false
			cr.Confidence = 1.0
			cr.Grootboekcode = hit.Grootboekcode
			cr.BTWCode = hit.BTWCode
			cr.Soort = hit.Soort
			cr.AIOmschrijving = hit.SampleOmschrijving
			cr.Indicator = "Eerder geboekt"
			cr.Learned = true
		} else if cl, ok := classMap[id]; ok {
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
		} else if errors.Is(err, eboekhouden.ErrSessionExpired) {
			// Stale token — clear it so the next /me reflects the disconnected state
			sess.EBoekhoudenToken = ""
			sess.MFAPending = false
			if h.sessions != nil {
				h.sessions.Update(c.Request.Context(), sess)
			}
			h.db.ClearEBToken(c.Request.Context(), sess.UserID)
			client = nil
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

	type batchItem struct {
		ID            int     `json:"id"`            // bank line ID (importId)
		GrootboekId   int     `json:"grootboekId"`   // bank account internal ID
		Soort         int     `json:"soort"`         // mutation type code
		Grootboekcode string  `json:"grootboekcode"` // tegenrekening code (from AI)
		BTWCode       string  `json:"btwCode"`
		Omschrijving  string  `json:"omschrijving"`
		Bedrag        float64 `json:"bedrag"`
		RelatieId     int     `json:"relatieId,omitempty"`
		Factuurnummer string  `json:"factuurnummer,omitempty"`
	}

	var req struct {
		Items []batchItem `json:"items"`
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

	// Look up ledger accounts to resolve grootboekcode → internal ID
	var ledgerAccounts []map[string]any
	raw, err := client.GetActiveLedgerAccounts()
	if err == nil {
		json.Unmarshal(raw, &ledgerAccounts)
	}

	// Pre-fetch the bank lines so we can compute learning signals (description
	// + counterparty IBAN) per item. The batch item only carries the user's
	// edited values, not the original bank line text — and the signal needs
	// to match what ClassifyBatch will see on the next pass.
	type bankLineMeta struct {
		omschrijving string
		rekening     string
		datum        string
	}
	bankLineMetaByID := make(map[int]bankLineMeta)
	if gridRaw, gridErr := client.GetImportGrid(0, 2000); gridErr == nil {
		if rows, _, parseErr := eboekhouden.ParseImportGrid(gridRaw); parseErr == nil {
			for _, row := range rows {
				id, _ := toInt(row["id"])
				bankLineMetaByID[id] = bankLineMeta{
					omschrijving: toString(row["mutOmschrijving"]),
					rekening:     toString(row["rekening"]),
					datum:        toString(row["mutDatum"]),
				}
			}
		}
	}

	type result struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
		MutNr  string `json:"mutNr,omitempty"`
	}

	// Map e-boekhouden soort codes back to the names we store in the
	// learned_classifications table so the next ClassifyBatch lookup matches.
	soortName := map[int]string{
		1: "FactuurOntvangen",
		2: "FactuurVerstuurd",
		3: "FactuurbetalingOntvangen",
		4: "FactuurbetalingVerstuurd",
		5: "GeldOntvangen",
		6: "GeldUitgegeven",
		7: "Memoriaal",
	}

	var results []result
	for _, item := range req.Items {
		// Resolve tegenrekening code to internal ID
		tegenRekeningId := 0
		for _, acc := range ledgerAccounts {
			code, _ := acc["code"].(string)
			if code == item.Grootboekcode {
				if id, ok := acc["id"].(float64); ok {
					tegenRekeningId = int(id)
				}
				break
			}
		}

		// Use the bank line's actual date and prefer it over time.Now() —
		// this matches what the e-boekhouden web UI sends and avoids dating
		// every booking on the day the user happens to process it.
		mutDatum := time.Now().Format("2006-01-02")
		if meta, ok := bankLineMetaByID[item.ID]; ok && meta.datum != "" {
			// e-boekhouden returns dates as "2026-03-30T00:00:00" — strip the time.
			if len(meta.datum) >= 10 {
				mutDatum = meta.datum[:10]
			}
		}

		// e-Boekhouden enforces a direction match between BTW code and soort:
		// income soorten (3 FactuurbetalingOntvangen, 5 GeldOntvangen) require
		// a *_VERK_* code, expense soorten (4, 6) require *_INK_*. Claude
		// regularly picks the wrong direction (especially for refunds of past
		// purchases — it sees the original purchase mental model and reaches
		// for INK), so we normalize the code to match the direction here.
		btwCode := normalizeBTWForSoort(item.BTWCode, item.Soort)

		// Same direction-aware logic for inEx: bank shows the gross amount,
		// so we always use IN for incoming and EX for outgoing.
		inEx := "EX"
		if item.Soort == 3 || item.Soort == 5 {
			inEx = "IN"
		}

		// The bank line bedrag is always gross (inclusive of BTW). Split it
		// according to the BTW code so e-boekhouden can include the booking
		// in the BTW-aangifte.
		rate := btwRateFromCode(btwCode)
		bedragIncl := item.Bedrag
		bedragExcl := item.Bedrag
		btwBedrag := 0.0
		if rate > 0 {
			btwBedrag = roundCents(bedragIncl * rate / (100 + rate))
			bedragExcl = roundCents(bedragIncl - btwBedrag)
		}

		// Build the mutation payload — mirrors what the e-boekhouden web UI
		// sends when manually creating a Geld uitgegeven / Geld ontvangen
		// mutation. The inEx field on the mutation is REQUIRED ("EX" or "IN");
		// for money-out/money-in soorten the bank shows the gross amount, so
		// we pair inEx with explicit bedragInclusief/bedragExclusief on the
		// regel.
		regel := map[string]any{
			"index":           0,
			"bedrag":          bedragExcl,
			"btw":             btwBedrag,
			"btwCode":         btwCode,
			"tegenRekening":   tegenRekeningId,
			"bedragInclusief": bedragIncl,
			"bedragExclusief": bedragExcl,
		}
		// Optional fields — only include when actually set so the e-boekhouden
		// validator doesn't choke on empty strings or zero IDs.
		if item.RelatieId != 0 {
			regel["relatieId"] = item.RelatieId
		}
		if item.Factuurnummer != "" {
			regel["factuur"] = item.Factuurnummer
		}

		mutPayload, _ := json.Marshal(map[string]any{
			"mutatie": map[string]any{
				"rekening":     item.GrootboekId,
				"datum":        mutDatum,
				"soort":        item.Soort,
				"inEx":         inEx,
				"omschrijving": item.Omschrijving,
			},
			"mutatieRegels": []map[string]any{regel},
			"importId":       item.ID,
		})

		mutResp, err := client.CreateMutatie(mutPayload)
		if err != nil {
			// Surface the real error message — the client now extracts the
			// e-Boekhouden message from JSON error envelopes, so this is
			// human-readable instead of just "Boeking mislukt".
			log.Printf("CreateMutatie error for item %d: %v", item.ID, err)
			results = append(results, result{ID: item.ID, Status: "error", Error: err.Error()})
			continue
		}
		var resp struct {
			MutNr int `json:"mutNr"`
		}
		if jsonErr := json.Unmarshal(mutResp, &resp); jsonErr != nil {
			log.Printf("CreateMutatie response unmarshal error for item %d: %v body=%s", item.ID, jsonErr, string(mutResp))
			results = append(results, result{ID: item.ID, Status: "error", Error: "Onverwacht antwoord van e-Boekhouden"})
			continue
		}
		// Defense-in-depth: even if the client missed an error envelope,
		// a successful mutation always carries a non-zero mutNr. Anything
		// else means nothing was actually booked.
		if resp.MutNr == 0 {
			log.Printf("CreateMutatie returned mutNr=0 for item %d, body=%s", item.ID, string(mutResp))
			results = append(results, result{ID: item.ID, Status: "error", Error: "Boeking niet aangemaakt — controleer de gegevens"})
			continue
		}
		results = append(results, result{ID: item.ID, Status: "ok", MutNr: fmt.Sprintf("%d", resp.MutNr)})

		// Update learned classifications memory now that the booking
		// definitely succeeded. Best-effort: failures are logged but never
		// block the main flow. The sample description is truncated to keep
		// the table from growing unbounded if e-boekhouden ever returns a
		// pathologically long mutOmschrijving.
		if sess != nil && bankLineMetaByID != nil {
			if meta, ok := bankLineMetaByID[item.ID]; ok {
				signal := database.BuildClassificationSignal(meta.omschrijving, meta.rekening)
				if signal != "" {
					sample := truncate(meta.omschrijving, 200)
					if upsertErr := h.db.UpsertLearned(c.Request.Context(), sess.UserID, signal,
						item.Grootboekcode, item.BTWCode, soortName[item.Soort], sample); upsertErr != nil {
						log.Printf("UpsertLearned error for user %s signal %s: %v", sess.UserID, signal, upsertErr)
					}
				}
			}
		}
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

// readEntityType pulls the user's entityType preference (BV / ZZP / EM /
// ANDERS) from user_settings.preferences. Returns empty string when not set.
func readEntityType(ctx context.Context, db *database.DB, userID string) string {
	if db == nil {
		return ""
	}
	settings, err := db.GetSettings(ctx, userID)
	if err != nil || settings == nil || len(settings.Preferences) == 0 {
		return ""
	}
	var prefs struct {
		EntityType string `json:"entityType"`
	}
	if json.Unmarshal(settings.Preferences, &prefs) != nil {
		return ""
	}
	return prefs.EntityType
}

// normalizeBTWForSoort enforces e-Boekhouden's direction rule: income soorten
// (3 FactuurbetalingOntvangen, 5 GeldOntvangen) require *_VERK_* codes,
// expense soorten (4, 6) require *_INK_*. We swap _INK_↔_VERK_ as needed.
// Reverse charge codes (VERL_INK*, BU_EU_INK, BI_EU_INK) and GEEN are passed
// through unchanged because they don't have a direction inversion.
func normalizeBTWForSoort(code string, soort int) string {
	isIncome := soort == 3 || soort == 5
	isExpense := soort == 4 || soort == 6
	if !isIncome && !isExpense {
		return code
	}
	switch code {
	case "HOOG_INK_21":
		if isIncome {
			return "HOOG_VERK_21"
		}
	case "HOOG_VERK_21":
		if isExpense {
			return "HOOG_INK_21"
		}
	case "LAAG_INK_9":
		if isIncome {
			return "LAAG_VERK_9"
		}
	case "LAAG_VERK_9":
		if isExpense {
			return "LAAG_INK_9"
		}
	}
	return code
}

// btwRateFromCode maps an e-boekhouden BTW code to its percentage. Reverse
// charge codes return 0 because the BTW is shifted to the recipient and
// doesn't get split out of the bank amount on the Dutch side.
func btwRateFromCode(code string) float64 {
	switch code {
	case "HOOG_INK_21", "HOOG_VERK_21":
		return 21
	case "LAAG_INK_9", "LAAG_VERK_9":
		return 9
	}
	// GEEN, VERL_INK, VERL_INK_L9, BU_EU_INK, BI_EU_INK, etc.
	return 0
}

// roundCents rounds to 2 decimal places using banker's rounding-equivalent
// half-away-from-zero, which matches what e-boekhouden's web UI does for the
// BTW split. Without this we get IEEE 754 noise like 21.000000000000004.
func roundCents(v float64) float64 {
	if v >= 0 {
		return float64(int64(v*100+0.5)) / 100
	}
	return float64(int64(v*100-0.5)) / 100
}
