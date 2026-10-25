package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
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

// invoicePromptVersion is part of the analysis cache key. Bump it whenever
// the invoice prompt or the InvoiceData shape changes in a way old cached
// answers cannot be normalised into. Version 2 added documentType: entries
// written by the version-1 prompt can never yield "overig" or "creditnota",
// and waiting for the 7-day TTL to roll them out is too slow. The
// invoice:submitted marker is deliberately NOT versioned: it must keep
// recognising files booked before a bump.
const invoicePromptVersion = "2"

// importGridFetchLimit is the page size used whenever a handler needs the
// whole unprocessed import grid at once (bank-line matching, bank-account
// lookup for a booking, batch classification). One page of 2000 covers any
// realistic backlog; nobody here pages further.
const importGridFetchLimit = 2000

// maxInvoiceFileSize is the upload limit for a document to analyse (10 MB),
// matching the security page's claim.
const maxInvoiceFileSize = 10 << 20

// errFileTooLarge is readUploadedFile's answer for a file above the limit.
var errFileTooLarge = errors.New("uploaded file too large")

// readUploadedFile reads an uploaded document of at most maxInvoiceFileSize
// bytes. A file above the limit is errFileTooLarge; a read error (a broken
// or aborted upload) comes back as itself, so writeUploadReadError can tell
// the user which of the two happened.
func readUploadedFile(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxInvoiceFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxInvoiceFileSize {
		return nil, errFileTooLarge
	}
	return data, nil
}

// writeUploadReadError answers a readUploadedFile error: 413 for a file
// above the limit, 400 for a file that could not be read.
func writeUploadReadError(c *gin.Context, err error) {
	if errors.Is(err, errFileTooLarge) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Het bestand is te groot. Speedy leest bestanden tot 10 MB."})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": "Het bestand kon niet worden gelezen. Probeer het opnieuw."})
}

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
// suggestion rather than reusing the stale one. The prompt version sits
// right after the prefix so a bump bypasses every entry of the old prompt.
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
	return fmt.Sprintf("invoice:analyze:v%s:%s:%s:%s", invoicePromptVersion, userID, pdfHash, hex.EncodeToString(accountsHash[:8]))
}

// invoiceSubmittedMarkerKey returns the Redis key for the "this PDF has
// been successfully booked" marker. Scoped per user + PDF hash; the value
// is a JSON blob with the resulting mutNr and submission timestamp so the
// analyze endpoint can show a clear duplicate warning.
func invoiceSubmittedMarkerKey(userID, pdfHash string) string {
	return fmt.Sprintf("invoice:submitted:%s:%s", userID, pdfHash)
}

// invoiceSubmittedMarker is the value we cache against the submission key:
// only what the UI shows ("Zelfde bestand geboekt op 3 okt (mutatie 1234)").
// Nothing about the invoice itself is kept. Markers written before Step 10
// also carry leverancier, factuur and bedragIncl; encoding/json ignores those
// fields on read and submittedMarkerJSON never echoes them.
type invoiceSubmittedMarker struct {
	MutNr        int       `json:"mutNr"`
	PaymentMutNr int       `json:"paymentMutNr,omitempty"`
	SubmittedAt  time.Time `json:"submittedAt"`
}

// InvoiceHandler handles invoice processing with Claude.
type InvoiceHandler struct {
	claude   *claude.Service
	db       *database.DB
	encKey   crypto.AESKey
	r2       *storage.Client
	redis    *redis.Client
	sessions *session.Store
}

// NewInvoiceHandler creates a new invoice handler. sessions is used to clear
// a stale e-boekhouden token when an analysis runs into an expired session;
// it may be nil (see HandleEBoekhoudenSessionExpired).
func NewInvoiceHandler(claudeSvc *claude.Service, db *database.DB, encKey crypto.AESKey, r2 *storage.Client, redisClient *redis.Client, sessions *session.Store) *InvoiceHandler {
	return &InvoiceHandler{claude: claudeSvc, db: db, encKey: encKey, r2: r2, redis: redisClient, sessions: sessions}
}

// errNoAPIKey is returned by analyzeBytes when the user has not configured
// an Anthropic API key. Wrappers answer 400 no_api_key.
var errNoAPIKey = errors.New("no Anthropic API key configured")

// claudeReadError wraps a Claude failure other than an unsupported media
// type. Wrappers answer 502 claude_error with the upstream message, as the
// analyze endpoint always has.
type claudeReadError struct{ err error }

func (e *claudeReadError) Error() string { return e.err.Error() }
func (e *claudeReadError) Unwrap() error { return e.err }

// detectDocumentMediaType sniffs the media type from the file's own bytes
// (never from the client-supplied header) and reports whether it is one the
// invoice reader accepts: application/pdf, image/png or image/jpeg.
func detectDocumentMediaType(data []byte) (string, bool) {
	switch mt := http.DetectContentType(data); mt {
	case "application/pdf", "image/png", "image/jpeg":
		return mt, true
	}
	return "", false
}

// documentExtension maps a supported media type to the file extension used
// for R2 keys and default filenames: pdf, jpg or png. Unsupported types
// yield "".
func documentExtension(mediaType string) string {
	switch mediaType {
	case "application/pdf":
		return "pdf"
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	}
	return ""
}

// Analyze handles POST /api/v1/invoices/analyze — reads an uploaded PDF, JPG
// or PNG via Claude and returns the extracted data for the user to review.
// Does NOT create any mutation. It is a thin multipart wrapper around
// analyzeBytes.
func (h *InvoiceHandler) Analyze(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	defer file.Close()

	data, err := readUploadedFile(file)
	if err != nil {
		writeUploadReadError(c, err)
		return
	}

	mediaType, ok := detectDocumentMediaType(data)
	if !ok {
		writeUnsupportedMediaType(c)
		return
	}

	filename := "document." + documentExtension(mediaType)
	if header != nil && header.Filename != "" {
		filename = header.Filename
	}

	resp, err := h.analyzeBytes(c, sess, data, mediaType, filename, analyzeOptions{})
	if err != nil {
		h.writeAnalyzeError(c, sess, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// analyzeOptions tunes analyzeBytes for its callers. The zero value is the
// upload flow's behaviour.
type analyzeOptions struct {
	// refresh bypasses the invoice:analyze cache read (the result is still
	// written), so a forced re-analysis really asks Claude again.
	refresh bool
}

// writeUnsupportedMediaType answers 415 for a file that is not a PDF, JPG or PNG.
func writeUnsupportedMediaType(c *gin.Context) {
	c.JSON(http.StatusUnsupportedMediaType, gin.H{
		"error":   "unsupported_media_type",
		"message": "Speedy kan alleen PDF-, JPG- en PNG-bestanden lezen.",
	})
}

// writeAnalyzeError maps an analyzeBytes error onto the HTTP response. It is
// kept apart from analyzeBytes so other endpoints can reuse the core together
// with this exact mapping.
func (h *InvoiceHandler) writeAnalyzeError(c *gin.Context, sess *session.SessionData, err error) {
	var readErr *claudeReadError
	switch {
	case errors.Is(err, errNoAPIKey):
		c.JSON(http.StatusBadRequest, gin.H{"error": "no_api_key", "message": "Vul eerst je Anthropic API-sleutel in bij Instellingen."})
	case errors.Is(err, claude.ErrUnsupportedMediaType):
		writeUnsupportedMediaType(c)
	case HandleEBoekhoudenSessionExpired(c, sess, h.sessions, h.db, err):
		// Response written: the frontend shows the reconnect prompt.
	case errors.As(err, &readErr):
		log.Printf("Claude invoice read error: %s", errClass(readErr.err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "claude_error", "message": readErr.err.Error()})
	default:
		log.Printf("Invoice analyze error: %s", errClass(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "analyze_failed", "message": "Analyse mislukt. Probeer het opnieuw."})
	}
}

// analyzeBytes is the analysis core shared by Analyze and, later, the
// document inbox. Starting from the file bytes it:
//
//   - loads the user's ledger accounts (Claude only suggests codes from that
//     list) and takes the crediteuren account id from the same response,
//   - serves the analysis from the per-user Redis cache or reads the
//     document with Claude,
//   - flags an earlier booking of the same bytes (duplicate marker),
//   - looks up the supplier relation and ranks the candidate bank lines.
//
// It keeps no copy of the file: an upload stays in the browser, which inlines
// it as pdfBase64 when booking, and a mail-folder document already lives in
// e-boekhouden. Nothing is written to R2.
//
// It writes nothing to the response. The returned gin.H is the
// InvoiceAnalyzeResponse body; errors are typed so callers can map them with
// writeAnalyzeError: errNoAPIKey, claude.ErrUnsupportedMediaType,
// eboekhouden.ErrSessionExpired and *claudeReadError. e-boekhouden lookups
// that fail for any other reason are best-effort and leave their part of the
// response empty, as the endpoint always did.
func (h *InvoiceHandler) analyzeBytes(c *gin.Context, sess *session.SessionData, data []byte, mediaType, filename string, opts analyzeOptions) (gin.H, error) {
	ctx := c.Request.Context()

	ext := documentExtension(mediaType)
	if ext == "" {
		return nil, claude.ErrUnsupportedMediaType
	}

	apiKey, err := h.getAPIKey(c, sess.UserID)
	if err != nil {
		return nil, errNoAPIKey
	}

	// Fetch the user's actual ledger accounts so Claude only suggests valid
	// codes. The crediteuren account the frontend needs comes from the same
	// rows (first account in category CRED).
	var accounts []claude.LedgerAccountInfo
	var crediteurenId int
	ebClient := session.ClientFromContext(c)
	if ebClient != nil {
		raw, err := ebClient.GetActiveLedgerAccounts()
		if errors.Is(err, eboekhouden.ErrSessionExpired) {
			return nil, err
		}
		if err == nil {
			var accs []map[string]any
			if json.Unmarshal(raw, &accs) == nil {
				for _, a := range accs {
					code, _ := a["code"].(string)
					omschr, _ := a["omschrijving"].(string)
					if code != "" && omschr != "" {
						accounts = append(accounts, claude.LedgerAccountInfo{Code: code, Omschrijving: omschr})
					}
					if cat, _ := a["rekeningCategorie"].(string); crediteurenId == 0 && strings.EqualFold(cat, "CRED") {
						if id, ok := a["id"].(float64); ok {
							crediteurenId = int(id)
						}
					}
				}
			}
		}
	}

	// Content-addressed analysis cache: hash the file bytes + the user's
	// ledger accounts (because the suggested grootboekcode is a function
	// of the available accounts) and check Redis before calling Claude.
	// A second upload of the same file returns the same JSON instantly,
	// instead of re-classifying with non-deterministic outputs.
	pdfHash := invoicePDFContentHash(data)
	cacheKey := invoiceAnalysisCacheKey(sess.UserID, data, accounts)
	var invoice *claude.InvoiceData
	cacheHit := false
	if h.redis != nil && !opts.refresh {
		if cached, err := h.redis.Get(ctx, cacheKey).Bytes(); err == nil {
			var cachedInvoice claude.InvoiceData
			if json.Unmarshal(cached, &cachedInvoice) == nil {
				// Entries cached before documentType existed have it empty;
				// derive it from isReceipt so the response shape is stable.
				claude.NormalizeDocumentType(&cachedInvoice)
				invoice = &cachedInvoice
				cacheHit = true
				log.Printf("Invoice analyze cache HIT for user %s pdfHash=%s", sess.UserID, pdfHash[:16])
			}
		}
	}
	if invoice == nil {
		fresh, err := h.claude.ReadInvoice(ctx, apiKey, mediaType, base64.StdEncoding.EncodeToString(data), accounts)
		if err != nil {
			if errors.Is(err, claude.ErrUnsupportedMediaType) {
				return nil, err
			}
			return nil, &claudeReadError{err: err}
		}
		invoice = fresh
		// Best-effort cache write — failures are logged but never fail the request.
		if h.redis != nil {
			if body, err := json.Marshal(invoice); err == nil {
				if setErr := h.redis.Set(ctx, cacheKey, body, invoiceAnalysisCacheTTL).Err(); setErr != nil {
					log.Printf("Invoice analyze cache write error: %v", setErr)
				}
			}
		}
	}

	// Duplicate-upload detection: if the user previously booked the same
	// file (same content hash) we surface the prior mutNr + datum in the
	// response so the dialog can warn before they accidentally book it
	// again. Scoped per user via the submission marker key.
	var alreadySubmitted *invoiceSubmittedMarker
	if h.redis != nil {
		markerKey := invoiceSubmittedMarkerKey(sess.UserID, pdfHash)
		if raw, err := h.redis.Get(ctx, markerKey).Bytes(); err == nil {
			var marker invoiceSubmittedMarker
			if json.Unmarshal(raw, &marker) == nil {
				alreadySubmitted = &marker
				log.Printf("Invoice analyze duplicate detected for user %s pdfHash=%s priorMutNr=%d",
					sess.UserID, pdfHash[:16], marker.MutNr)
			}
		}
	}

	// Search for matching relation
	var matchedRelation *gin.H
	if invoice.Leverancier != "" && ebClient != nil {
		raw, err := ebClient.SearchRelations(invoice.Leverancier)
		if errors.Is(err, eboekhouden.ErrSessionExpired) {
			return nil, err
		}
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

	// Rank the unprocessed bank statement lines that could be this
	// document's payment (see rankBankLineMatches for eligibility, the
	// tolerances and the score). For non-EUR invoices the amount test goes
	// through an approximate FX conversion; the candidate carries the user's
	// real EUR amount and the estimate only serves to FIND the line, never
	// to book a number. The frontend sees currencyConverted so it can ask
	// the user to verify.
	candidates := []BankLineCandidate{}
	if ebClient != nil && assignsBankLine(invoice) {
		raw, err := ebClient.GetImportGrid(0, importGridFetchLimit)
		if errors.Is(err, eboekhouden.ErrSessionExpired) {
			return nil, err
		}
		if err == nil {
			rows, _, _ := eboekhouden.ParseImportGrid(raw)
			candidates = rankBankLineMatches(invoice, rows, nil)
		}
	}
	var matchedBankLine map[string]any
	if len(candidates) > 0 {
		matchedBankLine = candidates[0].legacyJSON()
	}

	resp := gin.H{
		"invoice": invoice,
		// uploadKey and pdfUrl are always empty since Step 10: the analyser
		// keeps no copy of the file (nothing goes to R2). They stay in the
		// response so older clients keep parsing it; the booking carries the
		// bytes inline (pdfBase64) or names a mail-folder file (archiveFileId).
		"uploadKey":       "",
		"pdfUrl":          "",
		"matchedBankLine": matchedBankLine,
		// bankLineCandidates is the full ranking (best first, at most three)
		// so the UI can offer a choice. Always an array, never null.
		"bankLineCandidates": candidates,
		"filename":           filename,
		"matchedRelation":    matchedRelation,
		"crediteurenId":      crediteurenId,
		// pdfHash is echoed back so SubmitFull can write the submission
		// marker without rehashing the file (which we'd otherwise need to
		// re-download from R2). The frontend treats it as opaque.
		"pdfHash": pdfHash,
		// cachedAnalysis lets the UI surface a small "eerder geanalyseerd"
		// hint so the user knows why the result feels familiar.
		"cachedAnalysis": cacheHit,
	}
	// alreadySubmitted is set when the same file (by content hash) was
	// previously booked successfully via SubmitFull. The dialog renders a
	// loud Dutch warning so the user can decide whether they really want
	// to book it again — typical case is accidentally re-uploading from
	// an archive folder that's already been processed.
	if alreadySubmitted != nil {
		resp["alreadySubmitted"] = submittedMarkerJSON(alreadySubmitted)
	}
	return resp, nil
}

// submittedMarkerJSON renders the duplicate marker the way the analyze
// response has always done; the document inbox reuses it so both flows
// describe an earlier booking identically.
func submittedMarkerJSON(m *invoiceSubmittedMarker) gin.H {
	return gin.H{
		"mutNr":        m.MutNr,
		"paymentMutNr": m.PaymentMutNr,
		"submittedAt":  m.SubmittedAt.Format(time.RFC3339),
	}
}

// SubmitFull handles POST /api/v1/invoices/submit-full
// This is the complete chain: archive file → mutation → file link → (for a
// mail-folder document) rename and move. See invoice_archive.go for the
// archive phases.
func (h *InvoiceHandler) SubmitFull(c *gin.Context) {
	sess := session.FromContext(c)
	client := session.ClientFromContext(c)
	if sess == nil || client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}
	ctx := c.Request.Context()

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

		// Document source. Three are supported; at most one may be given
		// together with ArchiveFileId:
		//   - PdfBase64: document inlined as base64 (self-hosted, no R2 needed);
		//     wins over UploadKey when both are present
		//   - UploadKey: R2 object key from a prior /invoices/analyze call
		//   - ArchiveFileId: a file that already sits in the Mail & herken
		//     folder; nothing is uploaded, the file is linked, renamed and
		//     moved to TargetFolderId (or Facturen/<jaar>/<maand> when 0)
		// Filename is accepted for compatibility but no longer used: the
		// archive name follows archiveFileName.
		UploadKey      string `json:"uploadKey"`
		PdfBase64      string `json:"pdfBase64,omitempty"`
		Filename       string `json:"filename"`
		ArchiveFileId  int    `json:"archiveFileId,omitempty"`
		TargetFolderId int    `json:"targetFolderId,omitempty"`
		// DocumentType influences the archive name ("factuur" by default).
		// "creditnota" is refused: see errCreditnotaNotSupported.
		DocumentType string `json:"documentType,omitempty"`

		// FolderID is the e-boekhouden archive folder the user picked for an
		// upload. When zero we fall back to the legacy
		// findOrCreateArchiveFolder behavior (only relevant for R2
		// deployments — self-hosted clients should always send a FolderID or
		// skip archiving entirely).
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig verzoek."})
		return
	}

	if submitFullDocType(req.DocumentType) == claude.DocumentTypeCreditnota {
		c.JSON(http.StatusBadRequest, gin.H{"error": errCreditnotaNotSupported})
		return
	}

	// Validate monetary amounts
	if req.BedragExcl <= 0 || req.BedragIncl <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Het bedrag moet groter zijn dan nul."})
		return
	}
	if math.IsNaN(req.BedragExcl) || math.IsInf(req.BedragExcl, 0) ||
		math.IsNaN(req.BedragIncl) || math.IsInf(req.BedragIncl, 0) ||
		math.IsNaN(req.BTWBedrag) || math.IsInf(req.BTWBedrag, 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig bedrag."})
		return
	}

	if req.RelatieId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Kies een relatie (crediteur)."})
		return
	}
	if req.TegenRekeningId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Kies een grootboekrekening."})
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Speedy vindt geen crediteurenrekening in je administratie. Controleer je grootboekrekeningen in e-Boekhouden."})
			return
		}
	}
	if req.InEx == "" {
		req.InEx = "EX"
	}
	if req.BTWCode == "" {
		req.BTWCode = "HOOG_INK_21"
	}

	// Step 1: Resolve the document and stage the archive file. Every check
	// that can fail the request happens here, before anything is booked; the
	// upload itself stays best-effort as it always was.
	src, err := h.prepareArchiveSource(ctx, client, archiveSourceInput{
		archiveFileID:  req.ArchiveFileId,
		targetFolderID: req.TargetFolderId,
		pdfBase64:      req.PdfBase64,
		uploadKey:      req.UploadKey,
	})
	if err != nil {
		h.writeArchiveSourceError(c, sess, err)
		return
	}
	datum := bookingDate(req.Datum)
	archiveName := src.archiveName(archiveNameInput{
		leverancier: req.Leverancier,
		kenmerk:     req.Factuurnummer,
		datum:       req.Datum,
		docType:     submitFullDocType(req.DocumentType),
	})
	arch := h.stageArchiveFile(client, src, req.FolderID, datum, archiveName)
	archiefFileId := arch.fileID

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
		log.Printf("SubmitFull CreateMutatie error: %s", errClass(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "De factuur is niet geboekt: " + err.Error()})
		return
	}

	var mutResult struct {
		MutNr int `json:"mutNr"`
		MutId int `json:"mutId"`
	}
	if jsonErr := json.Unmarshal(mutResp, &mutResult); jsonErr != nil {
		log.Printf("SubmitFull invoice unmarshal error: %v (%d bytes)", jsonErr, len(mutResp))
		c.JSON(http.StatusBadGateway, gin.H{"error": "e-Boekhouden gaf een onverwacht antwoord. Kijk in e-Boekhouden of de factuur is geboekt voordat je het opnieuw probeert."})
		return
	}
	if mutResult.MutNr == 0 {
		log.Printf("SubmitFull invoice mutNr=0 (%d bytes)", len(mutResp))
		c.JSON(http.StatusBadGateway, gin.H{"error": "e-Boekhouden heeft de factuur niet geboekt en gaf geen reden. Controleer de gegevens en probeer het opnieuw."})
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
		raw, err := client.GetImportGrid(0, importGridFetchLimit)
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
				log.Printf("SubmitFull payment mutation error for invoice mutNr=%d: %s", mutResult.MutNr, errClass(err))
				paymentWarning = "De factuur is geboekt, maar de betaling niet, dus de afschriftregel staat nog open. Reden: " + err.Error()
			} else {
				var payResult struct {
					MutNr int `json:"mutNr"`
				}
				if jsonErr := json.Unmarshal(payResp, &payResult); jsonErr != nil {
					log.Printf("SubmitFull payment unmarshal error: %v (%d bytes)", jsonErr, len(payResp))
					paymentWarning = "De factuur is geboekt. Op de betaling gaf e-Boekhouden een onverwacht antwoord: kijk in e-Boekhouden of de afschriftregel is verwerkt."
				} else if payResult.MutNr == 0 {
					log.Printf("SubmitFull payment returned mutNr=0 (%d bytes)", len(payResp))
					paymentWarning = "De factuur is geboekt, maar e-Boekhouden heeft de betaling niet geboekt en gaf geen reden. De afschriftregel staat dus nog open."
				} else {
					paymentMutNr = payResult.MutNr
				}
			}
		} else {
			log.Printf("SubmitFull: could not resolve bankAccountId for importId=%d (bank line not found in grid)", req.ImportId)
			paymentWarning = "De factuur is geboekt, maar de betaling niet: Speedy vond de afschriftregel niet meer. Kijk in e-Boekhouden of die al is verwerkt."
		}
	}

	// Step 4: Link the archive file to the invoice mutation, then (for a
	// mail-folder document) rename it and move it out of the mail folder.
	linked := linkArchiveFile(client, archiefFileId, mutResult.MutNr)
	arch = h.completeArchive(ctx, client, sess.UserID, src, arch, linked, datum, archiveName)

	// Invalidate inbox classification cache so "Vernieuwen" fetches fresh data
	if h.redis != nil {
		h.redis.Del(ctx, inboxClassifyKey(sess.UserID))
	}

	// Write the duplicate-detection marker so a future re-upload of this
	// exact PDF surfaces an "already booked" warning at /analyze time.
	// Best-effort: a marker write failure never blocks the booking response.
	if h.redis != nil && req.PdfHash != "" && mutResult.MutNr > 0 {
		marker := invoiceSubmittedMarker{
			MutNr:        mutResult.MutNr,
			PaymentMutNr: paymentMutNr,
			SubmittedAt:  time.Now(),
		}
		if body, mErr := json.Marshal(marker); mErr == nil {
			if setErr := h.redis.Set(
				ctx,
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
	}
	// archived, linked, renamed, moved and, when set, archiveName,
	// targetFolderId, targetFolderPath and archiveWarning.
	arch.addTo(resp)
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
	sess := session.FromContext(c)
	client := session.ClientFromContext(c)
	if sess == nil || client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}
	ctx := c.Request.Context()

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
		// Document source: PdfBase64 (preferred, no R2 needed), UploadKey + R2,
		// or ArchiveFileId for a file in the mail folder; see SubmitFull.
		// Filename is accepted for compatibility but no longer used.
		UploadKey      string `json:"uploadKey"`
		PdfBase64      string `json:"pdfBase64,omitempty"`
		Filename       string `json:"filename"`
		ArchiveFileId  int    `json:"archiveFileId,omitempty"`
		TargetFolderId int    `json:"targetFolderId,omitempty"`
		// FolderID is the picked archive folder for an upload; zero falls
		// back to legacy auto-folder when R2 is configured.
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig verzoek."})
		return
	}

	if req.BedragIncl <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Het bedrag moet groter zijn dan nul."})
		return
	}
	if math.IsNaN(req.BedragIncl) || math.IsInf(req.BedragIncl, 0) ||
		math.IsNaN(req.BedragExcl) || math.IsInf(req.BedragExcl, 0) ||
		math.IsNaN(req.BTWBedrag) || math.IsInf(req.BTWBedrag, 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig bedrag."})
		return
	}
	if req.TegenRekeningId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Kies een grootboekrekening."})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Een boeking kan maximaal %d extra regels hebben.", maxExtraLines)})
		return
	}
	for i := range req.ExtraLines {
		el := &req.ExtraLines[i]
		if el.BedragIncl <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Extra regel %d: het bedrag moet groter zijn dan nul.", i+1)})
			return
		}
		if math.IsNaN(el.BedragIncl) || math.IsInf(el.BedragIncl, 0) ||
			math.IsNaN(el.BedragExcl) || math.IsInf(el.BedragExcl, 0) ||
			math.IsNaN(el.BTWBedrag) || math.IsInf(el.BTWBedrag, 0) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Extra regel %d: ongeldig bedrag.", i+1)})
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
		raw, err := client.GetImportGrid(0, importGridFetchLimit)
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Speedy weet niet van welke bankrekening dit bonnetje is betaald. Koppel het aan een afschriftregel die nog openstaat."})
		return
	}

	// Step 1: Resolve the document and stage the archive file — see SubmitFull
	// and invoice_archive.go. A receipt has no invoice number, so its archive
	// name ends in "bonnetje".
	src, err := h.prepareArchiveSource(ctx, client, archiveSourceInput{
		archiveFileID:  req.ArchiveFileId,
		targetFolderID: req.TargetFolderId,
		pdfBase64:      req.PdfBase64,
		uploadKey:      req.UploadKey,
	})
	if err != nil {
		h.writeArchiveSourceError(c, sess, err)
		return
	}
	datum := bookingDate(req.Datum)
	archiveName := src.archiveName(archiveNameInput{
		leverancier: req.Leverancier,
		datum:       req.Datum,
		docType:     claude.DocumentTypeBonnetje,
	})
	arch := h.stageArchiveFile(client, src, req.FolderID, datum, archiveName)
	archiefFileId := arch.fileID

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
		log.Printf("SubmitReceipt CreateMutatie error: %s", errClass(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "Het bonnetje is niet geboekt: " + err.Error()})
		return
	}

	var mutResult struct {
		MutNr int `json:"mutNr"`
		MutId int `json:"mutId"`
	}
	if jsonErr := json.Unmarshal(mutResp, &mutResult); jsonErr != nil {
		log.Printf("SubmitReceipt unmarshal error: %v (%d bytes)", jsonErr, len(mutResp))
		c.JSON(http.StatusBadGateway, gin.H{"error": "e-Boekhouden gaf een onverwacht antwoord. Kijk in e-Boekhouden of het bonnetje is geboekt voordat je het opnieuw probeert."})
		return
	}
	if mutResult.MutNr == 0 {
		log.Printf("SubmitReceipt mutNr=0 (%d bytes)", len(mutResp))
		c.JSON(http.StatusBadGateway, gin.H{"error": "e-Boekhouden heeft het bonnetje niet geboekt en gaf geen reden. Controleer de gegevens en probeer het opnieuw."})
		return
	}

	// Step 3: Link the archive file to the mutation, then (for a mail-folder
	// document) rename it and move it out of the mail folder.
	linked := linkArchiveFile(client, archiefFileId, mutResult.MutNr)
	arch = h.completeArchive(ctx, client, sess.UserID, src, arch, linked, datum, archiveName)

	// Invalidate inbox cache so the bank line disappears from the inbox view.
	if h.redis != nil {
		h.redis.Del(ctx, inboxClassifyKey(sess.UserID))
	}

	resp := gin.H{
		"mutNr": mutResult.MutNr,
		"mutId": mutResult.MutId,
	}
	arch.addTo(resp)
	c.JSON(http.StatusOK, resp)
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

// getAPIKey returns the user's decrypted Anthropic key, or an error when
// none is configured. GetSettings returns defaults (no error) for a user
// without a settings row, so the HasAnthropicKey check must produce its own
// error — returning ("", nil) here used to let Analyze call Claude with an
// empty key.
func (h *InvoiceHandler) getAPIKey(c *gin.Context, userID string) (string, error) {
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
