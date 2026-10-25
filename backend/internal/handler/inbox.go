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
	// The fields below are set by the overlays (see applyInboxOverlays) and
	// are never cached. RelatieID, RelatieNaam and Factuurnummer identify the
	// open post this line pays; OpenPostMatch says the suggestion came from
	// the open-post grid. DocumentMatch names the analysed mail-folder
	// document whose payment this line is.
	RelatieID     int                 `json:"relatieId,omitempty"`
	RelatieNaam   string              `json:"relatieNaam,omitempty"`
	Factuurnummer string              `json:"factuurnummer,omitempty"`
	OpenPostMatch bool                `json:"openPostMatch,omitempty"`
	DocumentMatch *inboxDocumentMatch `json:"documentMatch,omitempty"`
}

// inboxClassifyBase is the cached part of a classify response: the rows as
// the learned memory and Claude produced them, before the open-post and
// document overlays, which are recomputed on every response. Summary is the
// summary of the overlaid response at the time the cache was written, so
// DashboardSummary shows the counts the user last saw.
type inboxClassifyBase struct {
	Classifications []inboxClassifiedRow `json:"classifications"`
	TotalCount      int                  `json:"totalCount"`
	Summary         map[string]int       `json:"summary"`
}

// inboxClassifyCacheTTL is how long a classification is reused before Claude
// is asked again.
const inboxClassifyCacheTTL = time.Hour

// inboxClassifyKey is the Redis key of the user's cached classification.
func inboxClassifyKey(userID string) string {
	return "inbox:classify:" + userID
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

// ClassifyBatch handles POST /api/v1/inbox/classify — classifies all
// unprocessed bank lines.
//
// The response is a cached base (learned memory and Claude, one hour, see
// inboxClassifyBase) with two overlays recomputed on every response, cache
// hit or not: bank lines that pay an open post in e-boekhouden become
// factuurbetaling suggestions, and lines that are the assigned payment of an
// analysed mail-folder document are annotated with that document. On a cache
// miss the open posts are matched before Claude is called, so those lines
// never cost tokens. ?force=true drops the cached base first.
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
	ctx := c.Request.Context()
	cacheKey := inboxClassifyKey(sess.UserID)

	var base *inboxClassifyBase
	if c.Query("force") == "true" {
		h.redis.Del(ctx, cacheKey)
	} else if cached, err := h.redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var b inboxClassifyBase
		if json.Unmarshal(cached, &b) == nil && b.Classifications != nil {
			base = &b
		}
	}
	fresh := base == nil

	var openMatches map[int]openPostMatch
	if fresh {
		var ok bool
		base, openMatches, ok = h.classifyFresh(c, sess, client)
		if !ok {
			return
		}
	} else {
		// The cached rows carry everything the matcher needs; only the open
		// posts are fetched again so the overlay is current. Best-effort: a
		// cache hit made no upstream call before the overlays existed and
		// must not fail because of them.
		openMatches = matchOpenPosts(bankLinesFromClassified(base.Classifications), fetchOpenPosts(client))
	}

	docMatches := mailDocumentMatches(ctx, h.redis, client, sess.UserID, bankLinesFromClassified(base.Classifications))

	rows := applyInboxOverlays(base.Classifications, openMatches, docMatches)
	summary := summarizeInbox(rows)

	if fresh {
		base.Summary = summary
		if data, err := json.Marshal(base); err == nil {
			h.redis.Set(ctx, cacheKey, data, inboxClassifyCacheTTL)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"classifications": rows,
		"totalCount":      base.TotalCount,
		"summary":         summary,
	})
}

// writeEBError answers an e-boekhouden error: the reconnect prompt for an
// expired session, 502 with the upstream message otherwise.
func (h *InboxHandler) writeEBError(c *gin.Context, sess *session.SessionData, err error) {
	if h.handleEBError(c, sess, err) {
		return
	}
	c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
}

// classifyFresh fetches the unprocessed bank lines and builds the base
// classification: lines that pay an open post or hit the confirmed learned
// memory are decided locally, the rest goes to Claude in one batch. It
// returns the base and the open-post matches it found; ok is false when it
// already wrote the response (an error, or an empty inbox).
func (h *InboxHandler) classifyFresh(c *gin.Context, sess *session.SessionData, client *eboekhouden.Client) (base *inboxClassifyBase, openMatches map[int]openPostMatch, ok bool) {
	ctx := c.Request.Context()

	apiKey, err := h.getAPIKey(c, sess.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no_api_key", "message": "Stel eerst een Anthropic API-sleutel in"})
		return nil, nil, false
	}

	raw, err := client.GetImportGrid(0, importGridFetchLimit)
	if err != nil {
		h.writeEBError(c, sess, err)
		return nil, nil, false
	}
	rows, totalCount, err := eboekhouden.ParseImportGrid(raw)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse bank statements"})
		return nil, nil, false
	}
	if totalCount == 0 {
		c.JSON(http.StatusOK, gin.H{"classifications": []any{}, "totalCount": 0})
		return nil, nil, false
	}

	// Lines that settle an open invoice need no classification at all: the
	// booking is a factuurbetaling on that invoice, so match them before
	// anything is sent to Claude. Best-effort (see fetchOpenPosts).
	openMatches = matchOpenPosts(bankLinesFromRows(rows), fetchOpenPosts(client))

	// Pre-fetch learned mappings for this user. Lines whose normalized
	// signal matches a confirmed mapping are auto-classified locally and
	// never reach Claude — saves API tokens and gives the user instant
	// feedback for recurring transactions they've already booked once.
	learned, _ := h.db.LookupLearnedBatch(ctx, sess.UserID)

	// Convert to batch lines, but skip lines that have a confirmed learned
	// mapping (count >= 2 with confirmed_at set) and lines that pay an open
	// post by name. The learned hit is still recorded for open-post lines so
	// the row keeps a suggestion should the open post disappear from a later
	// overlay. A line that matches an open post on the amount alone still
	// goes to Claude: that overlay is only a "review" suggestion, and the row
	// needs its own classification when the user turns it down.
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
		if m, ok := openMatches[id]; ok && m.NameHit {
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

	// Classify with AI — only the lines that were not decided locally.
	// Pass the user's entity type so the prompt can apply BV-vs-ZZP rules
	// (e.g. investments on a B.V. account are business assets, not private).
	entityType := readEntityType(ctx, h.db, sess.UserID)
	var results []claude.BatchClassifyResult
	if len(lines) > 0 {
		results, err = h.claude.ClassifyBatch(ctx, apiKey, entityType, lines)
		if err != nil {
			log.Printf("Claude API error for user %s: %s", sess.UserID, errClass(err))
			c.JSON(http.StatusBadGateway, gin.H{"error": "claude_error", "message": err.Error()})
			return nil, nil, false
		}
	}

	// Merge classifications with original row data
	classMap := make(map[int]claude.BatchClassifyResult)
	for _, r := range results {
		classMap[r.ID] = r
	}

	classified := make([]inboxClassifiedRow, 0, len(rows))
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

	return &inboxClassifyBase{Classifications: classified, TotalCount: totalCount}, openMatches, true
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

		// Mail-folder documents still to handle (not ignored, not linked,
		// readable). Best-effort: a failure leaves the field out.
		if client != nil {
			if n, err := countOpenMailDocuments(c.Request.Context(), h.redis, client, sess.UserID); err == nil {
				summary["documentsCount"] = n
			}
		}

		// Check for cached classifications summary
		if cached, err := h.redis.Get(c.Request.Context(), inboxClassifyKey(sess.UserID)).Bytes(); err == nil {
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
	invoice, err := h.claude.ReadInvoice(c.Request.Context(), apiKey, "application/pdf", pdfBase64, nil)
	if err != nil {
		log.Printf("Claude API error for user %s: %s", sess.UserID, errClass(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "claude_error", "message": err.Error()})
		return
	}

	// Verify match between invoice and bank line
	amountMatch := false
	amountDiff := 0.0
	if invoice.BedragInclBTW > 0 {
		amountDiff = abs(abs(bankLine.Bedrag) - invoice.BedragInclBTW)
		amountMatch = amountDiff < 0.02 // within 2 cents
	}

	c.JSON(http.StatusOK, gin.H{
		"invoice": invoice,
		// No copy of the file is kept (see analyzeBytes); the field stays
		// for the response shape.
		"uploadKey":   "",
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
		Items []inboxBatchItem `json:"items"`
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
	if gridRaw, gridErr := client.GetImportGrid(0, importGridFetchLimit); gridErr == nil {
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

		// Build the mutation payload. A factuurbetaling (soort 3/4) settles
		// an open invoice and has its own shape; it is refused here when the
		// relation or invoice number is missing instead of being sent to
		// e-boekhouden, which would reject it silently.
		payload, err := buildBatchMutatiePayload(item, mutDatum, tegenRekeningId)
		if err != nil {
			results = append(results, result{ID: item.ID, Status: "error", Error: err.Error()})
			continue
		}
		mutPayload, _ := json.Marshal(payload)

		mutResp, err := client.CreateMutatie(mutPayload)
		if err != nil {
			// Surface the real error message — the client now extracts the
			// e-Boekhouden message from JSON error envelopes, so this is
			// human-readable instead of just "Boeking mislukt".
			log.Printf("CreateMutatie error for item %d: %s", item.ID, errClass(err))
			results = append(results, result{ID: item.ID, Status: "error", Error: err.Error()})
			continue
		}
		var resp struct {
			MutNr int `json:"mutNr"`
		}
		if jsonErr := json.Unmarshal(mutResp, &resp); jsonErr != nil {
			log.Printf("CreateMutatie response unmarshal error for item %d: %v (%d bytes)", item.ID, jsonErr, len(mutResp))
			results = append(results, result{ID: item.ID, Status: "error", Error: "Onverwacht antwoord van e-Boekhouden"})
			continue
		}
		// Defense-in-depth: even if the client missed an error envelope,
		// a successful mutation always carries a non-zero mutNr. Anything
		// else means nothing was actually booked.
		if resp.MutNr == 0 {
			log.Printf("CreateMutatie returned mutNr=0 for item %d (%d bytes)", item.ID, len(mutResp))
			results = append(results, result{ID: item.ID, Status: "error", Error: "Boeking niet aangemaakt — controleer de gegevens"})
			continue
		}
		results = append(results, result{ID: item.ID, Status: "ok", MutNr: fmt.Sprintf("%d", resp.MutNr)})

		// Update learned classifications memory now that the booking
		// definitely succeeded. Best-effort: failures are logged but never
		// block the main flow. The sample description is truncated to keep
		// the table from growing unbounded if e-boekhouden ever returns a
		// pathologically long mutOmschrijving. A factuurbetaling is tied to
		// one invoice, not a recurring pattern, so it is not learned.
		if sess != nil && bankLineMetaByID != nil && !isFactuurbetaling(item.Soort) {
			if meta, ok := bankLineMetaByID[item.ID]; ok {
				signal := database.BuildClassificationSignal(meta.omschrijving, meta.rekening)
				if signal != "" {
					sample := truncate(meta.omschrijving, 200)
					if upsertErr := h.db.UpsertLearned(c.Request.Context(), sess.UserID, signal,
						item.Grootboekcode, item.BTWCode, soortName[item.Soort], sample); upsertErr != nil {
						// The signal is built from the bank description and IBAN; log the user only.
						log.Printf("UpsertLearned error for user %s: %v", sess.UserID, upsertErr)
					}
				}
			}
		}
	}

	// Invalidate classification cache
	if sess != nil {
		h.redis.Del(c.Request.Context(), inboxClassifyKey(sess.UserID))
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
