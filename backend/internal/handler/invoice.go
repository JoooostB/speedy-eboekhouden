package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"regexp"
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

	invoice, err := h.claude.ReadInvoice(c.Request.Context(), apiKey, pdfBase64, accounts)
	if err != nil {
		log.Printf("Claude invoice read error: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "claude_error", "message": err.Error()})
		return
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

	// Try to match against unprocessed bank statement lines
	var matchedBankLine *gin.H
	if invoice.BedragInclBTW > 0 {
		client := session.ClientFromContext(c)
		if client != nil {
			raw, err := client.GetImportGrid(0, 500)
			if err == nil {
				rows, _, _ := eboekhouden.ParseImportGrid(raw)
				for _, row := range rows {
					bedrag, _ := toFloat(row["mutBedrag"])
					// Match on amount (within 2 cents, negative = outgoing payment)
					if abs(abs(bedrag)-invoice.BedragInclBTW) < 0.02 && bedrag < 0 {
						id, _ := toInt(row["id"])
						matchedBankLine = &gin.H{
							"id":           id,
							"datum":        toString(row["mutDatum"]),
							"bedrag":       bedrag,
							"omschrijving": toString(row["mutOmschrijving"]),
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

	c.JSON(http.StatusOK, gin.H{
		"invoice":         invoice,
		"uploadKey":       uploadKey,
		"pdfUrl":          pdfURL,
		"matchedBankLine": matchedBankLine,
		"filename":        filename,
		"matchedRelation": matchedRelation,
		"crediteurenId":   crediteurenId,
	})
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

		// PDF reference (R2 key from /invoices/analyze response)
		UploadKey string `json:"uploadKey"`
		Filename  string `json:"filename"`

		// Optional: bank statement line to mark as processed
		ImportId int `json:"importId,omitempty"`
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

	// Step 1: Upload PDF to e-boekhouden digitaal archief
	var archiefFileId int
	if req.UploadKey != "" && h.r2 != nil {
		// Fetch PDF from R2 (already uploaded during /invoices/analyze)
		pdfBytes, _, err := h.r2.Download(c.Request.Context(), req.UploadKey)
		if err != nil {
			log.Printf("R2 download error for %s: %v", req.UploadKey, err)
		} else {
			// Find or create the archive folder: Inkoopfacturen/year/month
			datum, _ := time.Parse("2006-01-02", req.Datum)
			if datum.IsZero() {
				datum = time.Now()
			}
			folderId, err := h.findOrCreateArchiveFolder(client, datum)
			if err != nil {
				log.Printf("Archive folder error: %v", err)
			} else {
				// Upload to e-boekhouden archive (base64 is required by their API)
				filename := sanitizeFilename(req.Filename)
				if req.Filename == "" {
					filename = sanitizeFilename(fmt.Sprintf("factuur_%s.pdf", req.Factuurnummer))
				}
				pdfBase64 := base64.StdEncoding.EncodeToString(pdfBytes)
				uploadPayload, _ := json.Marshal(map[string]any{
					"fileName":  filename,
					"data":      pdfBase64,
					"overwrite": false,
					"folderId":  folderId,
				})
				_, err := client.UploadArchiveFile(uploadPayload)
				if err != nil {
					log.Printf("Archive upload error: %v", err)
				} else {
					// The upload response returns folderId (the destination folder), NOT the file ID.
					// To get the file ID, list files in the folder and find the one we just uploaded.
					filesRaw, err := client.GetArchiveFiles(folderId)
					if err == nil {
						var files []map[string]any
						if json.Unmarshal(filesRaw, &files) == nil {
							for _, f := range files {
								naam, _ := f["naam"].(string)
								if naam == filename {
									if id, ok := f["id"].(float64); ok {
										archiefFileId = int(id)
									}
									break
								}
							}
						}
					}
				}
			}
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
		c.JSON(http.StatusBadGateway, gin.H{"error": "Factuur ontvangen mislukt: " + err.Error()})
		return
	}

	var mutResult struct {
		MutNr int `json:"mutNr"`
		MutId int `json:"mutId"`
	}
	json.Unmarshal(mutResp, &mutResult)

	// Step 3: If linked to a bank line, also create "Factuurbetaling verstuurd" mutation (soort 4)
	// This books: debit crediteuren, credit bank account — and marks the bank line as processed
	var paymentMutNr int
	if req.ImportId > 0 {
		// Find the bank account (grootboekId from the bank line)
		// The bank account ID comes from the matched bank line's grootboekId
		bankAccountId := 0
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

		if bankAccountId > 0 {
			paymentPayload, _ := json.Marshal(map[string]any{
				"mutatie": map[string]any{
					"rekening":     bankAccountId, // bank account
					"datum":        req.Datum,
					"soort":        4, // FactuurbetalingVerstuurd
					"omschrijving": truncate("Betaling "+req.Factuurnummer+" "+req.Leverancier, 200),
				},
				"mutatieRegels": []map[string]any{{
					"index":         0,
					"bedrag":        req.BedragIncl, // payment is incl BTW
					"btw":           0,
					"btwCode":       "GEEN",
					"tegenRekening": req.RekeningId, // crediteuren
					"relatieId":     req.RelatieId,
					"factuur":       req.Factuurnummer,
				}},
				"importId": req.ImportId, // marks the bank line as processed
			})

			payResp, err := client.CreateMutatie(paymentPayload)
			if err != nil {
				log.Printf("Payment mutation error: %v", err)
				// Non-fatal — invoice is booked, payment just failed
			} else {
				var payResult struct {
					MutNr int `json:"mutNr"`
				}
				json.Unmarshal(payResp, &payResult)
				paymentMutNr = payResult.MutNr
			}
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

	c.JSON(http.StatusOK, gin.H{
		"mutNr":        mutResult.MutNr,
		"mutId":        mutResult.MutId,
		"paymentMutNr": paymentMutNr,
		"archived":     archiefFileId > 0,
		"linked":       archiefFileId > 0 && mutResult.MutNr > 0,
	})
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

	var req struct {
		Datum           string  `json:"datum"`
		Leverancier     string  `json:"leverancier"`
		Omschrijving    string  `json:"omschrijving"`
		BedragExcl      float64 `json:"bedragExcl"`
		BedragIncl      float64 `json:"bedragIncl"`
		BTWBedrag       float64 `json:"btwBedrag"`
		BTWCode         string  `json:"btwCode"`
		TegenRekeningId int     `json:"tegenRekeningId"`
		UploadKey       string  `json:"uploadKey"`
		Filename        string  `json:"filename"`
		ImportId        int     `json:"importId,omitempty"`
		// BankAccountId is the e-boekhouden internal ID of the bank account
		// to debit. When ImportId is set we look it up from the bank line.
		BankAccountId int `json:"bankAccountId,omitempty"`
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

	// Resolve the bank account: prefer the explicit field, fall back to the
	// matched bank line's grootboekId. Without one we cannot book.
	bankAccountId := req.BankAccountId
	if bankAccountId == 0 && req.ImportId > 0 {
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
	if bankAccountId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Geen bankrekening gevonden voor deze boeking. Koppel een afschriftregel of geef bankAccountId mee."})
		return
	}

	// Step 1: Upload PDF to e-boekhouden archive (same flow as SubmitFull).
	var archiefFileId int
	if req.UploadKey != "" && h.r2 != nil {
		pdfBytes, _, err := h.r2.Download(c.Request.Context(), req.UploadKey)
		if err != nil {
			log.Printf("R2 download error for %s: %v", req.UploadKey, err)
		} else {
			datum, _ := time.Parse("2006-01-02", req.Datum)
			if datum.IsZero() {
				datum = time.Now()
			}
			folderId, err := h.findOrCreateArchiveFolder(client, datum)
			if err != nil {
				log.Printf("Archive folder error: %v", err)
			} else {
				filename := sanitizeFilename(req.Filename)
				if filename == "" {
					filename = sanitizeFilename(fmt.Sprintf("bonnetje_%s_%s.pdf", req.Datum, req.Leverancier))
				}
				pdfBase64 := base64.StdEncoding.EncodeToString(pdfBytes)
				uploadPayload, _ := json.Marshal(map[string]any{
					"fileName":  filename,
					"data":      pdfBase64,
					"overwrite": false,
					"folderId":  folderId,
				})
				if _, err := client.UploadArchiveFile(uploadPayload); err != nil {
					log.Printf("Archive upload error: %v", err)
				} else {
					// Resolve the file ID by listing the folder.
					if filesRaw, err := client.GetArchiveFiles(folderId); err == nil {
						var files []map[string]any
						if json.Unmarshal(filesRaw, &files) == nil {
							for _, f := range files {
								naam, _ := f["naam"].(string)
								if naam == filename {
									if id, ok := f["id"].(float64); ok {
										archiefFileId = int(id)
									}
									break
								}
							}
						}
					}
				}
			}
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
	mutPayload, _ := json.Marshal(map[string]any{
		"mutatie": map[string]any{
			"rekening":     bankAccountId,
			"datum":        req.Datum,
			"soort":        6, // GeldUitgegeven
			"omschrijving": truncate(desc, 200),
		},
		"mutatieRegels": []map[string]any{{
			"index":           0,
			"bedrag":          req.BedragExcl,
			"bedragExclusief": req.BedragExcl,
			"bedragInclusief": req.BedragIncl,
			"btw":             req.BTWBedrag,
			"btwCode":         req.BTWCode,
			"tegenRekening":   req.TegenRekeningId,
		}},
		"importId": nilIfZero(req.ImportId),
	})

	mutResp, err := client.CreateMutatie(mutPayload)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Bonnetje boeken mislukt: " + err.Error()})
		return
	}

	var mutResult struct {
		MutNr int `json:"mutNr"`
		MutId int `json:"mutId"`
	}
	json.Unmarshal(mutResp, &mutResult)

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

// findOrCreateArchiveFolder finds or creates Basismap/Facturen/year/month folder structure.
// Path: Basismap → Facturen → 2026 → 01 Januari
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

	// Find "Basismap" root (parentId == 0)
	var basismapId int
	for _, f := range folders {
		if f.Naam == "Basismap" && f.ParentId == 0 {
			basismapId = f.ID
			break
		}
	}
	if basismapId == 0 {
		payload, _ := json.Marshal(map[string]any{"parentFolderId": 0, "name": "Basismap"})
		resp, err := client.CreateArchiveFolder(payload)
		if err != nil {
			return 0, fmt.Errorf("creating Basismap folder: %w", err)
		}
		var created struct {
			ID int `json:"id"`
		}
		json.Unmarshal(resp, &created)
		basismapId = created.ID
	}

	// Find or create "Facturen" under Basismap
	var rootId int
	for _, f := range folders {
		if f.Naam == "Facturen" && f.ParentId == basismapId {
			rootId = f.ID
			break
		}
	}
	if rootId == 0 {
		payload, _ := json.Marshal(map[string]any{"parentFolderId": basismapId, "name": "Facturen"})
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
