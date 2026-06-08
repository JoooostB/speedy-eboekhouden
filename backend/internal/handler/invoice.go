package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
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

// invoiceAnalysisCacheTTL is how long an analyzed PDF's parsed data stays
// in Redis. 7 days is long enough for bulk re-uploads of an old archive
// to produce stable, identical suggestions, and short enough that prompt
// updates roll through within a week as the cache expires naturally.
const invoiceAnalysisCacheTTL = 7 * 24 * time.Hour

// invoiceSubmittedMarkerTTL keeps the "this PDF was successfully booked
// as mutNr X" marker around long enough to catch re-uploads weeks or
// months later (90 days). The user typically sees their old PDFs again
// when they're cleaning up an archive — the marker prevents accidental
// duplicate bookings even at long delays.
const invoiceSubmittedMarkerTTL = 90 * 24 * time.Hour

// invoicePDFContentHash returns a stable hex-encoded SHA-256 of the raw
// PDF bytes. Used as the canonical key for invoice-level caching so a
// re-upload of the exact same file produces identical analysis results
// (deterministic) AND surfaces a "this was already booked" warning when
// the file was previously processed.
func invoicePDFContentHash(pdfBytes []byte) string {
	sum := sha256.Sum256(pdfBytes)
	return hex.EncodeToString(sum[:])
}

// invoiceAnalysisCacheKey returns the Redis key for cached invoice analysis
// results. Scoped per user (so cross-tenant collisions are impossible),
// per PDF hash, and per ledger-account-set hash because the suggested
// grootboekcode is a function of the available accounts — re-running an
// analyze after the user adds/removes an account should produce a fresh
// suggestion rather than reusing the stale one.
func invoiceAnalysisCacheKey(userID string, pdfBytes []byte, accounts []claude.LedgerAccountInfo) string {
	pdfHash := invoicePDFContentHash(pdfBytes)
	// Stable ordering — sort accounts by code so callers passing the same
	// set in different orders still hit the same cache entry.
	codes := make([]string, 0, len(accounts))
	for _, a := range accounts {
		codes = append(codes, a.Code)
	}
	sort.Strings(codes)
	accountsHash := sha256.Sum256([]byte(strings.Join(codes, ",")))
	return fmt.Sprintf("invoice:analyze:%s:%s:%s", userID, pdfHash, hex.EncodeToString(accountsHash[:8]))
}

// invoiceSubmittedMarkerKey returns the Redis key for the "this PDF has
// been successfully booked" marker. Scoped per user + PDF hash; the value
// is a JSON blob with the resulting mutNr and submission timestamp so the
// analyze endpoint can show a clear duplicate warning.
func invoiceSubmittedMarkerKey(userID, pdfHash string) string {
	return fmt.Sprintf("invoice:submitted:%s:%s", userID, pdfHash)
}

// invoiceSubmittedMarker is the value we cache against the submission key.
// Kept small — the analyze endpoint just needs enough to render a Dutch
// warning ("Deze factuur is al eerder geboekt op X als mutatie nr Y").
type invoiceSubmittedMarker struct {
	MutNr        int       `json:"mutNr"`
	PaymentMutNr int       `json:"paymentMutNr,omitempty"`
	Leverancier  string    `json:"leverancier,omitempty"`
	Factuur      string    `json:"factuur,omitempty"`
	BedragIncl   float64   `json:"bedragIncl,omitempty"`
	SubmittedAt  time.Time `json:"submittedAt"`
}

// InvoiceHandler handles invoice processing with Claude.
type InvoiceHandler struct {
	claude *claude.Service
	db     *database.DB
	encKey crypto.AESKey
	r2     *storage.Client
	redis  *redis.Client
}

// NewInvoiceHandler creates a new invoice handler.
func NewInvoiceHandler(claudeSvc *claude.Service, db *database.DB, encKey crypto.AESKey, r2 *storage.Client, redisClient *redis.Client) *InvoiceHandler {
	return &InvoiceHandler{claude: claudeSvc, db: db, encKey: encKey, r2: r2, redis: redisClient}
}

// Analyze handles POST /api/v1/invoices/analyze — reads a PDF via Claude.
// Returns extracted data for the user to review. Does NOT create any mutation.
func (h *InvoiceHandler) Analyze(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	apiKey, err := h.getAPIKey(c, sess.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no_api_key", "message": "Stel eerst een Anthropic API-sleutel in"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	defer file.Close()

	const maxFileSize = 10 << 20
	pdfBytes, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read file"})
		return
	}
	if len(pdfBytes) > maxFileSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "bestand is te groot (max 10 MB)"})
		return
	}

	pdfBase64 := base64.StdEncoding.EncodeToString(pdfBytes)

	// Fetch the user's actual ledger accounts so Claude only suggests valid codes
	var accounts []claude.LedgerAccountInfo
	ebClient := session.ClientFromContext(c)
	if ebClient != nil {
		raw, err := ebClient.GetActiveLedgerAccounts()
		if err == nil {
			var accs []map[string]any
			if json.Unmarshal(raw, &accs) == nil {
				for _, a := range accs {
					code, _ := a["code"].(string)
					omschr, _ := a["omschrijving"].(string)
					if code != "" && omschr != "" {
						accounts = append(accounts, claude.LedgerAccountInfo{Code: code, Omschrijving: omschr})
					}
				}
			}
		}
	}

	// Content-addressed analysis cache: hash the PDF bytes + the user's
	// ledger accounts (because the suggested grootboekcode is a function
	// of the available accounts) and check Redis before calling Claude.
	// A second upload of the same PDF returns the same JSON instantly,
	// instead of re-classifying with non-deterministic outputs.
	pdfHash := invoicePDFContentHash(pdfBytes)
	cacheKey := invoiceAnalysisCacheKey(sess.UserID, pdfBytes, accounts)
	var invoice *claude.InvoiceData
	cacheHit := false
	if h.redis != nil {
		if cached, err := h.redis.Get(c.Request.Context(), cacheKey).Bytes(); err == nil {
			var cachedInvoice claude.InvoiceData
			if json.Unmarshal(cached, &cachedInvoice) == nil {
				invoice = &cachedInvoice
				cacheHit = true
				log.Printf("Invoice analyze cache HIT for user %s pdfHash=%s", sess.UserID, pdfHash[:16])
			}
		}
	}
	if invoice == nil {
		fresh, err := h.claude.ReadInvoice(c.Request.Context(), apiKey, pdfBase64, accounts)
		if err != nil {
			log.Printf("Claude invoice read error: %v", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "claude_error", "message": err.Error()})
			return
		}
		invoice = fresh
		// Best-effort cache write — failures are logged but never fail the request.
		if h.redis != nil {
			if body, err := json.Marshal(invoice); err == nil {
				if setErr := h.redis.Set(c.Request.Context(), cacheKey, body, invoiceAnalysisCacheTTL).Err(); setErr != nil {
					log.Printf("Invoice analyze cache write error: %v", setErr)
				}
			}
		}
	}

	// Duplicate-upload detection: if the user previously booked the same
	// PDF (same content hash) we surface the prior mutNr + datum in the
	// response so the dialog can warn before they accidentally book it
	// again. Scoped per user via the submission marker key.
	var alreadySubmitted *invoiceSubmittedMarker
	if h.redis != nil {
		markerKey := invoiceSubmittedMarkerKey(sess.UserID, pdfHash)
		if raw, err := h.redis.Get(c.Request.Context(), markerKey).Bytes(); err == nil {
			var marker invoiceSubmittedMarker
			if json.Unmarshal(raw, &marker) == nil {
				alreadySubmitted = &marker
				log.Printf("Invoice analyze duplicate detected for user %s pdfHash=%s priorMutNr=%d",
					sess.UserID, pdfHash[:16], marker.MutNr)
			}
		}
	}

	// Store PDF temporarily in R2 for later submission
	var uploadKey string
	var pdfURL string
	if h.r2 != nil {
		uploadKey = fmt.Sprintf("uploads/%s/document.pdf", uuid.New().String())
		h.r2.Upload(c.Request.Context(), uploadKey, pdfBytes, "application/pdf")
		// Generate a signed URL (15 min TTL) — never expose raw public URLs for financial docs
		signed, err := h.r2.GeneratePresignedURL(c.Request.Context(), uploadKey)
		if err == nil {
			pdfURL = signed
		}
	}

	// Search for matching relation
	var matchedRelation *gin.H
	if invoice.Leverancier != "" {
		client := session.ClientFromContext(c)
		if client != nil {
			raw, err := client.SearchRelations(invoice.Leverancier)
			if err == nil {
				var relations []map[string]any
				if json.Unmarshal(raw, &relations) == nil && len(relations) > 0 {
					r := relations[0]
					matchedRelation = &gin.H{
						"id":      r["id"],
						"code":    r["code"],
						"bedrijf": r["bedrijf"],
					}
				}
			}
		}
	}

	// Try to match against unprocessed bank statement lines.
	//
	// For EUR invoices the bedrag matches the bank line within 2 cents.
	// For non-EUR invoices (Tesla Supercharger receipts in CHF, foreign
	// hotels, USD/GBP SaaS) we convert the invoice total to an approximate
	// EUR value and match within a wider tolerance to absorb the FX margin
	// the user's bank applied. The matched bank line carries the user's
	// real EUR amount — the FX estimate is only used to FIND the right
	// line, not to book any number. We surface the fuzzy match to the
	// frontend via "currencyConverted" so the user knows to verify.
	var matchedBankLine *gin.H
	if invoice.BedragInclBTW > 0 {
		client := session.ClientFromContext(c)
		if client != nil {
			raw, err := client.GetImportGrid(0, 500)
			if err == nil {
				rows, _, _ := eboekhouden.ParseImportGrid(raw)

				eurEstimate, fxConverted := convertToEUR(invoice.BedragInclBTW, invoice.Currency)
				for _, row := range rows {
					bedrag, _ := toFloat(row["mutBedrag"])
					if bedrag >= 0 {
						continue // we only match outgoing payments to invoices
					}
					absBedrag := abs(bedrag)

					var hit bool
					if !fxConverted {
						// EUR: tight 2-cent tolerance to avoid false positives
						hit = abs(absBedrag-invoice.BedragInclBTW) < 0.02
					} else {
						// Non-EUR: relative tolerance around the FX estimate
						diff := abs(absBedrag - eurEstimate)
						hit = diff/eurEstimate < currencyMatchTolerance
					}

					if hit {
						id, _ := toInt(row["id"])
						matchedBankLine = &gin.H{
							"id":                 id,
							"datum":              toString(row["mutDatum"]),
							"bedrag":             bedrag,
							"omschrijving":       toString(row["mutOmschrijving"]),
							"currencyConverted":  fxConverted,
							"invoiceCurrency":    strings.ToUpper(invoice.Currency),
							"invoiceAmount":      invoice.BedragInclBTW,
						}
						break
					}
				}
			}
		}
	}

	filename := "document.pdf"
	if header != nil && header.Filename != "" {
		filename = header.Filename
	}

	// Find crediteuren account ID by category for the frontend
	var crediteurenId int
	if ebClient != nil {
		raw, err := ebClient.GetActiveLedgerAccounts()
		if err == nil {
			var fullAccs []map[string]any
			if json.Unmarshal(raw, &fullAccs) == nil {
				for _, fa := range fullAccs {
					cat, _ := fa["rekeningCategorie"].(string)
					if strings.EqualFold(cat, "CRED") {
						if id, ok := fa["id"].(float64); ok {
							crediteurenId = int(id)
						}
						break
					}
				}
			}
		}
	}

	resp := gin.H{
		"invoice":         invoice,
		"uploadKey":       uploadKey,
		"pdfUrl":          pdfURL,
		"matchedBankLine": matchedBankLine,
		"filename":        filename,
		"matchedRelation": matchedRelation,
		"crediteurenId":   crediteurenId,
		// pdfHash is echoed back so SubmitFull can write the submission
		// marker without rehashing the PDF (which we'd otherwise need to
		// re-download from R2). The frontend treats it as opaque.
		"pdfHash":         pdfHash,
		// cachedAnalysis lets the UI surface a small "eerder geanalyseerd"
		// hint so the user knows why the result feels familiar.
		"cachedAnalysis":  cacheHit,
	}
	// alreadySubmitted is set when the same PDF (by content hash) was
	// previously booked successfully via SubmitFull. The dialog renders a
	// loud Dutch warning so the user can decide whether they really want
	// to book it again — typical case is accidentally re-uploading from
	// an archive folder that's already been processed.
	if alreadySubmitted != nil {
		resp["alreadySubmitted"] = gin.H{
			"mutNr":        alreadySubmitted.MutNr,
			"paymentMutNr": alreadySubmitted.PaymentMutNr,
			"leverancier":  alreadySubmitted.Leverancier,
			"factuur":      alreadySubmitted.Factuur,
			"bedragIncl":   alreadySubmitted.BedragIncl,
			"submittedAt":  alreadySubmitted.SubmittedAt.Format(time.RFC3339),
		}
	}
	c.JSON(http.StatusOK, resp)
}

// SubmitFull handles POST /api/v1/invoices/submit-full
// This is the complete chain: archive upload → mutation → file link.
func (h *InvoiceHandler) SubmitFull(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	var req struct {
		// Invoice details (from Claude + user review)
		Datum         string  `json:"datum"`
		Leverancier   string  `json:"leverancier"`
		Factuurnummer string  `json:"factuurnummer"`
		Omschrijving  string  `json:"omschrijving"`
		BedragExcl    float64 `json:"bedragExcl"`
		BedragIncl    float64 `json:"bedragIncl"`
		BTWBedrag     float64 `json:"btwBedrag"`
		BTWCode       string  `json:"btwCode"`
		InEx          string  `json:"inEx"` // "IN" or "EX"

		// Account IDs (from user selection)
		RelatieId       int `json:"relatieId"`
		TegenRekeningId int `json:"tegenRekeningId"` // cost account
		RekeningId      int `json:"rekeningId"`      // crediteuren account

		// PDF reference. Two sources are supported and PdfBase64 wins when
		// both are provided so a client can opt out of R2 entirely:
		//   - PdfBase64: PDF inlined as base64 (self-hosted, no R2 needed)
		//   - UploadKey: R2 object key from a prior /invoices/analyze call
		UploadKey string `json:"uploadKey"`
		PdfBase64 string `json:"pdfBase64,omitempty"`
		Filename  string `json:"filename"`

		// FolderID is the e-boekhouden archive folder the user picked. When
		// zero we fall back to the legacy findOrCreateArchiveFolder behavior
		// (only relevant for R2 deployments — self-hosted clients should
		// always send a FolderID or skip archiving entirely).
		FolderID int `json:"folderId,omitempty"`

		// Optional: bank statement line to mark as processed
		ImportId int `json:"importId,omitempty"`

		// Content hash echoed back from /analyze. We use it to write a
		// per-user "this PDF has been booked as mutNr X" marker so a
		// later re-upload triggers a duplicate-detection warning.
		// Optional — when absent (legacy clients), no marker is written
		// but the booking still succeeds.
		PdfHash string `json:"pdfHash,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig verzoek"})
		return
	}

	// Validate monetary amounts
	if req.BedragExcl <= 0 || req.BedragIncl <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Bedrag moet positief zijn"})
		return
	}
	if math.IsNaN(req.BedragExcl) || math.IsInf(req.BedragExcl, 0) ||
		math.IsNaN(req.BedragIncl) || math.IsInf(req.BedragIncl, 0) ||
		math.IsNaN(req.BTWBedrag) || math.IsInf(req.BTWBedrag, 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig bedrag"})
		return
	}

	if req.RelatieId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Selecteer een relatie (leverancier)"})
		return
	}
	if req.TegenRekeningId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Selecteer een kostenrekening (grootboekrekening)"})
		return
	}

	// Look up crediteuren account by category if not provided
	if req.RekeningId == 0 {
		if ebClient := session.ClientFromContext(c); ebClient != nil {
			raw, err := ebClient.GetActiveLedgerAccounts()
			if err == nil {
				var accs []map[string]any
				if json.Unmarshal(raw, &accs) == nil {
					for _, a := range accs {
						cat, _ := a["rekeningCategorie"].(string)
						// Look for the crediteuren category account
						if strings.EqualFold(cat, "CRED") {
							if id, ok := a["id"].(float64); ok {
								req.RekeningId = int(id)
							}
							break
						}
					}
				}
			}
		}
		if req.RekeningId == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Geen crediteurenrekening gevonden in je administratie. Controleer je grootboekrekeningen in e-Boekhouden."})
			return
		}
	}
	if req.InEx == "" {
		req.InEx = "EX"
	}
	if req.BTWCode == "" {
		req.BTWCode = "HOOG_INK_21"
	}

	// Step 1: Upload PDF to e-boekhouden digitaal archief.
	//
	// PDF source: prefer the inlined PdfBase64 from the client (no R2 required)
	// and fall back to an R2 download keyed by UploadKey for backward compat
	// with deployments that still use the analyze→R2→submit flow.
	//
	// Folder selection: prefer the explicit user choice (FolderID from the
	// frontend picker); when zero AND R2 is in play, keep the legacy auto
	// "Inkoopfacturen/jaar/maand" behavior so R2 deployments don't regress.
	// Self-hosted clients without R2 that omit FolderID skip archiving — an
	// explicit opt-out, not a silent failure.
	var archiefFileId int
	var pdfBytes []byte
	if req.PdfBase64 != "" {
		if decoded, err := base64.StdEncoding.DecodeString(req.PdfBase64); err != nil {
			log.Printf("SubmitFull: invalid pdfBase64: %v", err)
		} else {
			pdfBytes = decoded
		}
	} else if req.UploadKey != "" && h.r2 != nil {
		if downloaded, _, err := h.r2.Download(c.Request.Context(), req.UploadKey); err != nil {
			log.Printf("R2 download error for %s: %v", req.UploadKey, err)
		} else {
			pdfBytes = downloaded
		}
	}
	if len(pdfBytes) > 0 {
		folderID := req.FolderID
		if folderID == 0 && h.r2 != nil {
			datum, _ := time.Parse("2006-01-02", req.Datum)
			if datum.IsZero() {
				datum = time.Now()
			}
			if id, err := h.findOrCreateArchiveFolder(client, datum); err != nil {
				log.Printf("Archive folder error: %v", err)
			} else {
				folderID = id
			}
		}
		if folderID > 0 {
			filename := req.Filename
			if filename == "" {
				filename = fmt.Sprintf("factuur_%s.pdf", req.Factuurnummer)
			}
			archiefFileId = h.uploadAndResolveArchiveFile(client, pdfBytes, folderID, filename)
		}
	}

	// Step 2: Create "Factuur ontvangen" mutation (soort 1)
	// This books: debit cost account (tegenRekening), credit crediteuren (rekening)
	invoiceMutPayload, _ := json.Marshal(map[string]any{
		"mutatie": map[string]any{
			"rekening":     req.RekeningId, // crediteuren account
			"relatieId":    req.RelatieId,
			"datum":        req.Datum,
			"termijn":      30,
			"factuur":      req.Factuurnummer,
			"soort":        1, // FactuurOntvangen
			"inEx":         req.InEx,
			"omschrijving": truncate(req.Omschrijving, 200),
		},
		"mutatieRegels": []map[string]any{{
			"index":           0,
			"bedrag":          req.BedragExcl,
			"tegenRekening":   req.TegenRekeningId,
			"bedragExclusief": req.BedragExcl,
			"bedragInclusief": req.BedragIncl,
			"btwCode":         req.BTWCode,
			"btw":             req.BTWBedrag,
		}},
	})

	mutResp, err := client.CreateMutatie(invoiceMutPayload)
	if err != nil {
		log.Printf("SubmitFull CreateMutatie error: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Factuur ontvangen mislukt: " + err.Error()})
		return
	}

	var mutResult struct {
		MutNr int `json:"mutNr"`
		MutId int `json:"mutId"`
	}
	if jsonErr := json.Unmarshal(mutResp, &mutResult); jsonErr != nil {
		log.Printf("SubmitFull invoice unmarshal error: %v body=%s", jsonErr, string(mutResp))
		c.JSON(http.StatusBadGateway, gin.H{"error": "Onverwacht antwoord van e-Boekhouden bij factuur ontvangen"})
		return
	}
	if mutResult.MutNr == 0 {
		log.Printf("SubmitFull invoice mutNr=0, body=%s", string(mutResp))
		c.JSON(http.StatusBadGateway, gin.H{"error": "Factuur ontvangen niet aangemaakt — controleer de gegevens"})
		return
	}

	// Step 3: If linked to a bank line, also create "Factuurbetaling verstuurd"
	// mutation (soort 4). This books: debit crediteuren, credit bank account —
	// and marks the bank line as processed via the importId field.
	//
	// IMPORTANT — for soort 4 the crediteurenboeking is implicit via
	// relatieId+factuur. e-boekhouden looks up the matching open factuur and
	// books it automatically. We must NOT send tegenRekening on the regel:
	// the e-boekhouden web UI does not, and including it caused silent
	// failures where the FactuurOntvangen succeeded but the payment was
	// rejected, leaving the bank line unprocessed (mutate.har showed the
	// canonical web-UI shape — no tegenRekening on the regel).
	//
	// We also surface payment failures to the user instead of swallowing
	// them: an unprocessed bank line is a real problem the user needs to
	// know about — the FactuurOntvangen is booked but the bank line stays
	// in the inbox, leading to duplicate bookings on retry.
	var paymentMutNr int
	var paymentWarning string
	if req.ImportId > 0 {
		bankAccountId := 0
		// Capture the bank line's own omschrijving so we can use it on the
		// payment mutation, matching what the e-boekhouden web UI does
		// (manually-import.har shows the bank line's raw description being
		// used verbatim). Better for audit reconciliation than a synthetic
		// "Betaling X Y" string because it's literally what shows up on
		// the bank statement.
		bankLineOmschrijving := ""
		raw, err := client.GetImportGrid(0, 500)
		if err == nil {
			rows, _, _ := eboekhouden.ParseImportGrid(raw)
			for _, row := range rows {
				id, _ := toInt(row["id"])
				if id == req.ImportId {
					bankAccountId, _ = toInt(row["grootboekId"])
					bankLineOmschrijving = toString(row["mutOmschrijving"])
					break
				}
			}
		}

		if bankAccountId > 0 {
			// Prefer the bank line's own description; fall back to a
			// synthetic "Betaling …" if for some reason it's empty.
			paymentOmschrijving := bankLineOmschrijving
			if paymentOmschrijving == "" {
				paymentOmschrijving = "Betaling " + req.Factuurnummer + " " + req.Leverancier
			}
			paymentPayload, _ := json.Marshal(map[string]any{
				"mutatie": map[string]any{
					"rekening":     bankAccountId,
					"datum":        req.Datum,
					"soort":        4,
					"omschrijving": truncate(paymentOmschrijving, 200),
				},
				"mutatieRegels": []map[string]any{{
					"index":     0,
					"bedrag":    req.BedragIncl,
					"btw":       0,
					"btwCode":   "GEEN",
					"relatieId": req.RelatieId,
					"factuur":   req.Factuurnummer,
				}},
				"importId": req.ImportId,
			})

			payResp, err := client.CreateMutatie(paymentPayload)
			if err != nil {
				log.Printf("SubmitFull payment mutation error for invoice mutNr=%d: %v", mutResult.MutNr, err)
				paymentWarning = "Factuur is geboekt, maar de afschriftregel kon niet worden gemarkeerd: " + err.Error()
			} else {
				var payResult struct {
					MutNr int `json:"mutNr"`
				}
				if jsonErr := json.Unmarshal(payResp, &payResult); jsonErr != nil {
					log.Printf("SubmitFull payment unmarshal error: %v body=%s", jsonErr, string(payResp))
					paymentWarning = "Factuur is geboekt, maar het antwoord van e-Boekhouden voor de betaling was onverwacht. Controleer de afschriftregel handmatig."
				} else if payResult.MutNr == 0 {
					log.Printf("SubmitFull payment returned mutNr=0; body=%s", string(payResp))
					paymentWarning = "Factuur is geboekt, maar de betalingsmutatie is niet aangemaakt — afschriftregel is mogelijk niet gemarkeerd. Controleer in e-Boekhouden."
				} else {
					paymentMutNr = payResult.MutNr
				}
			}
		} else {
			log.Printf("SubmitFull: could not resolve bankAccountId for importId=%d (bank line not found in grid)", req.ImportId)
			paymentWarning = "Factuur is geboekt, maar de afschriftregel kon niet worden gevonden. Controleer in e-Boekhouden."
		}
	}

	// Step 4: Link archived file to the invoice mutation
	if archiefFileId > 0 && mutResult.MutNr > 0 {
		linkPayload, _ := json.Marshal(map[string]any{
			"koppelId":   mutResult.MutNr,
			"folders":    []map[string]any{{"id": archiefFileId, "soort": "F"}},
			"koppelType": "MUT",
		})
		_, err := client.LinkFileToMutation(linkPayload)
		if err != nil {
			log.Printf("File link error: %v", err)
		}
	}

	// Invalidate inbox classification cache so "Vernieuwen" fetches fresh data
	sess := session.FromContext(c)
	if sess != nil && h.redis != nil {
		h.redis.Del(c.Request.Context(), fmt.Sprintf("inbox:classify:%s", sess.UserID))
	}

	// Write the duplicate-detection marker so a future re-upload of this
	// exact PDF surfaces an "already booked" warning at /analyze time.
	// Best-effort: a marker write failure never blocks the booking response.
	if sess != nil && h.redis != nil && req.PdfHash != "" && mutResult.MutNr > 0 {
		marker := invoiceSubmittedMarker{
			MutNr:        mutResult.MutNr,
			PaymentMutNr: paymentMutNr,
			Leverancier:  req.Leverancier,
			Factuur:      req.Factuurnummer,
			BedragIncl:   req.BedragIncl,
			SubmittedAt:  time.Now(),
		}
		if body, mErr := json.Marshal(marker); mErr == nil {
			if setErr := h.redis.Set(
				c.Request.Context(),
				invoiceSubmittedMarkerKey(sess.UserID, req.PdfHash),
				body,
				invoiceSubmittedMarkerTTL,
			).Err(); setErr != nil {
				log.Printf("Invoice submitted marker write error: %v", setErr)
			}
		}
	}

	resp := gin.H{
		"mutNr":        mutResult.MutNr,
		"mutId":        mutResult.MutId,
		"paymentMutNr": paymentMutNr,
		"archived":     archiefFileId > 0,
		"linked":       archiefFileId > 0 && mutResult.MutNr > 0,
	}
	// Surface a Dutch payment-step warning to the frontend so the user
	// sees that the bank-line clearing didn't happen. The FactuurOntvangen
	// itself succeeded so we still return 200, but the warning lets the UI
	// show a yellow alert instead of green.
	if paymentWarning != "" {
		resp["paymentWarning"] = paymentWarning
	}
	c.JSON(http.StatusOK, resp)
}

// SubmitReceipt handles POST /api/v1/invoices/submit-receipt — books a
// "bonnetje" (restaurant, supermarket, etc.) directly as a "Geld uitgegeven"
// mutation, with the receipt file attached to the mutation. No leverancier
// relation is created or linked — the supplier name is only used in the
// description so the booking is still searchable.
//
// This is the lightweight cousin of SubmitFull: one mutation instead of two,
// no crediteurenrekening lookup, no relatie required.
func (h *InvoiceHandler) SubmitReceipt(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	// extraLine is one additional booking line within the same mutation.
	// Used most commonly for restaurant tips: the receipt itself shows
	// €40 food at 9% BTW, but the bank charged €45 because the user added
	// a €5 tip on the card. The €5 tip portion gets its own line with
	// btwCode GEEN (tips are not deductible BTW under Dutch tax rules)
	// and typically the same tegenrekening as the main line
	// (representatiekosten). Generalises to any case where one booking
	// needs to be split across multiple BTW codes / tegenrekeningen.
	type extraLine struct {
		BedragExcl      float64 `json:"bedragExcl"`
		BedragIncl      float64 `json:"bedragIncl"`
		BTWBedrag       float64 `json:"btwBedrag"`
		BTWCode         string  `json:"btwCode"`
		TegenRekeningId int     `json:"tegenRekeningId"`
		Omschrijving    string  `json:"omschrijving,omitempty"`
	}

	var req struct {
		Datum           string  `json:"datum"`
		Leverancier     string  `json:"leverancier"`
		Omschrijving    string  `json:"omschrijving"`
		BedragExcl      float64 `json:"bedragExcl"`
		BedragIncl      float64 `json:"bedragIncl"`
		BTWBedrag       float64 `json:"btwBedrag"`
		BTWCode         string  `json:"btwCode"`
		TegenRekeningId int     `json:"tegenRekeningId"`
		// PDF source: PdfBase64 (preferred, no R2 needed) or UploadKey + R2.
		UploadKey string `json:"uploadKey"`
		PdfBase64 string `json:"pdfBase64,omitempty"`
		Filename  string `json:"filename"`
		// FolderID is the picked archive folder; zero falls back to legacy
		// auto-folder when R2 is configured.
		FolderID int `json:"folderId,omitempty"`
		ImportId int `json:"importId,omitempty"`
		// BankAccountId is the e-boekhouden internal ID of the bank account
		// to debit. When ImportId is set we look it up from the bank line.
		BankAccountId int `json:"bankAccountId,omitempty"`
		// ExtraLines is optional — used by the dialog when the user
		// splits a receipt into multiple booking lines (e.g. food vs tip).
		// Each line is appended to mutatieRegels after the main line.
		ExtraLines []extraLine `json:"extraLines,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig verzoek"})
		return
	}

	if req.BedragIncl <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Bedrag moet positief zijn"})
		return
	}
	if math.IsNaN(req.BedragIncl) || math.IsInf(req.BedragIncl, 0) ||
		math.IsNaN(req.BedragExcl) || math.IsInf(req.BedragExcl, 0) ||
		math.IsNaN(req.BTWBedrag) || math.IsInf(req.BTWBedrag, 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig bedrag"})
		return
	}
	if req.TegenRekeningId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Selecteer een kostenrekening (grootboekrekening)"})
		return
	}
	if req.BTWCode == "" {
		req.BTWCode = "HOOG_INK_21"
	}
	// If excl is missing but incl + btw are present, derive it.
	if req.BedragExcl == 0 && req.BedragIncl > 0 {
		req.BedragExcl = req.BedragIncl - req.BTWBedrag
	}

	// Validate every extra line: positive bedragIncl, finite values, valid
	// tegenrekening. Derive bedragExcl when missing, same as the main line.
	const maxExtraLines = 10
	if len(req.ExtraLines) > maxExtraLines {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Maximaal %d extra regels per boeking", maxExtraLines)})
		return
	}
	for i := range req.ExtraLines {
		el := &req.ExtraLines[i]
		if el.BedragIncl <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Extra regel %d: bedrag moet positief zijn", i+1)})
			return
		}
		if math.IsNaN(el.BedragIncl) || math.IsInf(el.BedragIncl, 0) ||
			math.IsNaN(el.BedragExcl) || math.IsInf(el.BedragExcl, 0) ||
			math.IsNaN(el.BTWBedrag) || math.IsInf(el.BTWBedrag, 0) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Extra regel %d: ongeldig bedrag", i+1)})
			return
		}
		if el.TegenRekeningId == 0 {
			// Default to the main line's tegenrekening — the common tip
			// case routes through the same expense account anyway.
			el.TegenRekeningId = req.TegenRekeningId
		}
		if el.BTWCode == "" {
			el.BTWCode = "GEEN"
		}
		if el.BedragExcl == 0 {
			el.BedragExcl = el.BedragIncl - el.BTWBedrag
		}
	}

	// Resolve the bank account: prefer the matched bank line's grootboekId
	// (which is by definition a bank account the user owns and is currently
	// accessible in their administration), fall back to the explicit field
	// only after validating it against the user's active ledger accounts.
	// Without this check, a caller could supply an arbitrary internal ID and
	// have a "Geld uitgegeven" mutation booked against an unrelated account
	// in the same administration (e.g. savings, investment).
	bankAccountId := 0
	if req.ImportId > 0 {
		raw, err := client.GetImportGrid(0, 500)
		if err == nil {
			rows, _, _ := eboekhouden.ParseImportGrid(raw)
			for _, row := range rows {
				id, _ := toInt(row["id"])
				if id == req.ImportId {
					bankAccountId, _ = toInt(row["grootboekId"])
					break
				}
			}
		}
	}
	if bankAccountId == 0 && req.BankAccountId != 0 {
		// Caller-provided bank account ID — validate it appears in the
		// user's list of active ledger accounts and is in the bank category.
		raw, err := client.GetActiveLedgerAccounts()
		if err == nil {
			var accs []map[string]any
			if json.Unmarshal(raw, &accs) == nil {
				for _, a := range accs {
					id, _ := a["id"].(float64)
					cat, _ := a["rekeningCategorie"].(string)
					if int(id) == req.BankAccountId && (strings.EqualFold(cat, "BANK") || strings.EqualFold(cat, "KAS")) {
						bankAccountId = req.BankAccountId
						break
					}
				}
			}
		}
		if bankAccountId == 0 {
			log.Printf("SubmitReceipt: rejected unverified bankAccountId %d for user", req.BankAccountId)
		}
	}
	if bankAccountId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Geen bankrekening gevonden voor deze boeking. Koppel een afschriftregel."})
		return
	}

	// Step 1: Upload PDF to e-boekhouden archive — see SubmitFull for the
	// dual-source (PdfBase64 vs R2) + dual-folder (explicit vs legacy auto)
	// rationale. Identical decision tree here, only the default filename
	// differs ("bonnetje_…" instead of "factuur_…").
	var archiefFileId int
	var pdfBytes []byte
	if req.PdfBase64 != "" {
		if decoded, err := base64.StdEncoding.DecodeString(req.PdfBase64); err != nil {
			log.Printf("SubmitReceipt: invalid pdfBase64: %v", err)
		} else {
			pdfBytes = decoded
		}
	} else if req.UploadKey != "" && h.r2 != nil {
		if downloaded, _, err := h.r2.Download(c.Request.Context(), req.UploadKey); err != nil {
			log.Printf("R2 download error for %s: %v", req.UploadKey, err)
		} else {
			pdfBytes = downloaded
		}
	}
	if len(pdfBytes) > 0 {
		folderID := req.FolderID
		if folderID == 0 && h.r2 != nil {
			datum, _ := time.Parse("2006-01-02", req.Datum)
			if datum.IsZero() {
				datum = time.Now()
			}
			if id, err := h.findOrCreateArchiveFolder(client, datum); err != nil {
				log.Printf("Archive folder error: %v", err)
			} else {
				folderID = id
			}
		}
		if folderID > 0 {
			filename := req.Filename
			if filename == "" {
				filename = fmt.Sprintf("bonnetje_%s_%s.pdf", req.Datum, req.Leverancier)
			}
			archiefFileId = h.uploadAndResolveArchiveFile(client, pdfBytes, folderID, filename)
		}
	}

	// Step 2: Create the "Geld uitgegeven" mutation (soort 6).
	// Bank account is debited (rekening), cost account is credited (tegenRekening).
	// The bedrag/btw split lets e-boekhouden include this in the BTW-aangifte.
	desc := req.Omschrijving
	if req.Leverancier != "" {
		if desc == "" {
			desc = req.Leverancier
		} else {
			desc = req.Leverancier + " - " + desc
		}
	}
	regels := []map[string]any{{
		"index":           0,
		"bedrag":          req.BedragExcl,
		"bedragExclusief": req.BedragExcl,
		"bedragInclusief": req.BedragIncl,
		"btw":             req.BTWBedrag,
		"btwCode":         req.BTWCode,
		"tegenRekening":   req.TegenRekeningId,
	}}
	// Append any user-added extra lines (typically tip splits). The
	// per-line `index` is sequential — matches what the web UI does
	// when a user manually adds multiple regels to a single mutation.
	for i, el := range req.ExtraLines {
		regel := map[string]any{
			"index":           i + 1,
			"bedrag":          el.BedragExcl,
			"bedragExclusief": el.BedragExcl,
			"bedragInclusief": el.BedragIncl,
			"btw":             el.BTWBedrag,
			"btwCode":         el.BTWCode,
			"tegenRekening":   el.TegenRekeningId,
		}
		if el.Omschrijving != "" {
			regel["omschrijving"] = truncate(el.Omschrijving, 200)
		}
		regels = append(regels, regel)
	}

	mutPayload, _ := json.Marshal(map[string]any{
		"mutatie": map[string]any{
			"rekening":     bankAccountId,
			"datum":        req.Datum,
			"soort":        6, // GeldUitgegeven
			"inEx":         "EX",
			"omschrijving": truncate(desc, 200),
		},
		"mutatieRegels": regels,
		"importId":      nilIfZero(req.ImportId),
	})

	mutResp, err := client.CreateMutatie(mutPayload)
	if err != nil {
		log.Printf("SubmitReceipt CreateMutatie error: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Bonnetje boeken mislukt: " + err.Error()})
		return
	}

	var mutResult struct {
		MutNr int `json:"mutNr"`
		MutId int `json:"mutId"`
	}
	if jsonErr := json.Unmarshal(mutResp, &mutResult); jsonErr != nil {
		log.Printf("SubmitReceipt unmarshal error: %v body=%s", jsonErr, string(mutResp))
		c.JSON(http.StatusBadGateway, gin.H{"error": "Onverwacht antwoord van e-Boekhouden"})
		return
	}
	if mutResult.MutNr == 0 {
		log.Printf("SubmitReceipt mutNr=0, body=%s", string(mutResp))
		c.JSON(http.StatusBadGateway, gin.H{"error": "Bonnetje niet aangemaakt — controleer de gegevens"})
		return
	}

	// Step 3: Link the archived file to the mutation.
	if archiefFileId > 0 && mutResult.MutNr > 0 {
		linkPayload, _ := json.Marshal(map[string]any{
			"koppelId":   mutResult.MutNr,
			"folders":    []map[string]any{{"id": archiefFileId, "soort": "F"}},
			"koppelType": "MUT",
		})
		if _, err := client.LinkFileToMutation(linkPayload); err != nil {
			log.Printf("File link error: %v", err)
		}
	}

	// Invalidate inbox cache so the bank line disappears from the inbox view.
	sess := session.FromContext(c)
	if sess != nil && h.redis != nil {
		h.redis.Del(c.Request.Context(), fmt.Sprintf("inbox:classify:%s", sess.UserID))
	}

	c.JSON(http.StatusOK, gin.H{
		"mutNr":    mutResult.MutNr,
		"mutId":    mutResult.MutId,
		"archived": archiefFileId > 0,
		"linked":   archiefFileId > 0 && mutResult.MutNr > 0,
	})
}

// uploadAndResolveArchiveFile uploads pdfBytes into folderID and returns the
// archive file ID assigned by e-boekhouden. The /folder/upload endpoint
// returns the destination folder ID — not the file ID — so we list the folder
// after upload and match on filename to recover the real ID, which the caller
// needs to link the file to a mutation. Returns 0 on any failure (logged).
//
// The filename is sanitized internally; an empty input becomes "document.pdf".
// Callers that want a more descriptive name (e.g. "factuur_X.pdf") should pass
// that string in rawFilename — sanitization keeps it but adds ".pdf" if missing.
func (h *InvoiceHandler) uploadAndResolveArchiveFile(client *eboekhouden.Client, pdfBytes []byte, folderID int, rawFilename string) int {
	if len(pdfBytes) == 0 || folderID == 0 {
		return 0
	}
	filename := sanitizeFilename(rawFilename)
	uploadPayload, _ := json.Marshal(map[string]any{
		"fileName":  filename,
		"data":      base64.StdEncoding.EncodeToString(pdfBytes),
		"overwrite": false,
		"folderId":  folderID,
	})
	if _, err := client.UploadArchiveFile(uploadPayload); err != nil {
		log.Printf("Archive upload error: %v", err)
		return 0
	}
	filesRaw, err := client.GetArchiveFiles(folderID)
	if err != nil {
		log.Printf("Archive file list error after upload: %v", err)
		return 0
	}
	var files []map[string]any
	if err := json.Unmarshal(filesRaw, &files); err != nil {
		log.Printf("Archive file list parse error: %v", err)
		return 0
	}
	for _, f := range files {
		if naam, _ := f["naam"].(string); naam == filename {
			if id, ok := f["id"].(float64); ok {
				return int(id)
			}
		}
	}
	return 0
}

// findOrCreateArchiveFolder finds or creates the Facturen/year/month folder
// structure. Path: (implicit root Basismap) → Facturen → 2026 → 01 Januari.
//
// IMPORTANT: e-Boekhouden treats parentFolderId=0 as "the implicit root
// Basismap" and Basismap itself does NOT appear as a real folder in the
// GetArchiveFolders response. Earlier versions of this function tried to
// find Basismap with parentId=0, didn't, and "helpfully" created a real
// folder literally named "Basismap" — producing a doubled
// Basismap/Basismap/Facturen/... structure. We now skip Basismap entirely
// and create Facturen directly under parentFolderId=0.
//
// For backwards-compat with users who have the doubled structure from the
// previous broken behavior, we look for an existing "Facturen" folder at
// any depth so we don't strand old files.
func (h *InvoiceHandler) findOrCreateArchiveFolder(client *eboekhouden.Client, datum time.Time) (int, error) {
	maandNamen := []string{
		"01 Januari", "02 Februari", "03 Maart", "04 April", "05 Mei", "06 Juni",
		"07 Juli", "08 Augustus", "09 September", "10 Oktober", "11 November", "12 December",
	}

	jaar := fmt.Sprintf("%d", datum.Year())
	maand := maandNamen[datum.Month()-1]

	// Get all folders
	raw, err := client.GetArchiveFolders()
	if err != nil {
		return 0, fmt.Errorf("getting folders: %w", err)
	}

	var folders []struct {
		ID       int    `json:"id"`
		Naam     string `json:"naam"`
		ParentId int    `json:"parentId"`
	}
	if err := json.Unmarshal(raw, &folders); err != nil {
		return 0, fmt.Errorf("parsing folders: %w", err)
	}

	// Find "Facturen" — preferring one at the top level (parentId == 0,
	// which is the implicit Basismap root). If we don't find one there but
	// there IS one under a folder literally named "Basismap" (legacy doubled
	// structure from the old broken code), reuse that one so old files stay
	// findable. If neither exists, create a fresh Facturen at the root.
	var rootId int
	for _, f := range folders {
		if f.Naam == "Facturen" && f.ParentId == 0 {
			rootId = f.ID
			break
		}
	}
	if rootId == 0 {
		// Legacy fallback: look for Facturen under any "Basismap" parent.
		basismapIDs := map[int]bool{}
		for _, f := range folders {
			if f.Naam == "Basismap" {
				basismapIDs[f.ID] = true
			}
		}
		for _, f := range folders {
			if f.Naam == "Facturen" && basismapIDs[f.ParentId] {
				rootId = f.ID
				break
			}
		}
	}
	if rootId == 0 {
		// Create Facturen directly at the implicit root.
		payload, _ := json.Marshal(map[string]any{"parentFolderId": 0, "name": "Facturen"})
		resp, err := client.CreateArchiveFolder(payload)
		if err != nil {
			return 0, fmt.Errorf("creating Facturen folder: %w", err)
		}
		var created struct {
			ID int `json:"id"`
		}
		json.Unmarshal(resp, &created)
		rootId = created.ID
	}

	// Find or create year folder
	var yearId int
	for _, f := range folders {
		if f.Naam == jaar && f.ParentId == rootId {
			yearId = f.ID
			break
		}
	}
	if yearId == 0 {
		payload, _ := json.Marshal(map[string]any{"parentFolderId": rootId, "name": jaar})
		resp, err := client.CreateArchiveFolder(payload)
		if err != nil {
			return 0, fmt.Errorf("creating year folder: %w", err)
		}
		var created struct {
			ID int `json:"id"`
		}
		json.Unmarshal(resp, &created)
		yearId = created.ID
	}

	// Find or create month folder
	var monthId int
	for _, f := range folders {
		if f.Naam == maand && f.ParentId == yearId {
			monthId = f.ID
			break
		}
	}
	if monthId == 0 {
		payload, _ := json.Marshal(map[string]any{"parentFolderId": yearId, "name": maand})
		resp, err := client.CreateArchiveFolder(payload)
		if err != nil {
			return 0, fmt.Errorf("creating month folder: %w", err)
		}
		var created struct {
			ID int `json:"id"`
		}
		json.Unmarshal(resp, &created)
		monthId = created.ID
	}

	return monthId, nil
}

// Submit handles POST /api/v1/invoices/submit — simple mutation passthrough (legacy).
func (h *InvoiceHandler) Submit(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	raw, err := client.CreateMutatie(body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}

func (h *InvoiceHandler) getAPIKey(c *gin.Context, userID string) (string, error) {
	settings, err := h.db.GetSettings(c.Request.Context(), userID)
	if err != nil || !settings.HasAnthropicKey {
		return "", err
	}
	decrypted, err := crypto.Decrypt(h.encKey, settings.AnthropicKeyEnc)
	if err != nil {
		return "", err
	}
	return string(decrypted), nil
}

var safeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

func sanitizeFilename(s string) string {
	s = safeFilenameRe.ReplaceAllString(s, "_")
	if len(s) > 100 {
		s = s[:100]
	}
	if s == "" || s == "_" {
		s = "document"
	}
	if len(s) < 5 || s[len(s)-4:] != ".pdf" {
		s += ".pdf"
	}
	return s
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func nilIfZero(i int) any {
	if i == 0 {
		return nil
	}
	return i
}
