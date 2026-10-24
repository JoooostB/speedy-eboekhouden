package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
	"github.com/redis/go-redis/v9"
)

// The document inbox exposes the files that land in the administration's
// "Mail & herken" folder (the mail folder). The folder is always found through
// menhFolderId from the folder header, never by name: Joost's is called
// "Inbox", other users' can have any name and any path.
//
// Redis keys, all per user:
//
//	doc:analysis:v{prompt}:{userID}:{fileID}  cached analysis, 30 days, JSON documentAnalysisEntry
//	doc:analyzing:{userID}:{fileID}           lock while an analysis runs, 120 s
//	doc:ignored:{userID}                      set of ignored file ids, no TTL
const (
	documentAnalysisCacheTTL = 30 * 24 * time.Hour
	documentAnalyzeLockTTL   = 120 * time.Second
	maxArchiveFileIDs        = 100
	mailFolderDefaultName    = "Inbox"
)

var (
	// errMailFolderNotConfigured means the administration has no Mail &
	// herken folder (menhFolderId is 0).
	errMailFolderNotConfigured = errors.New("mail folder not configured")
	// errDocumentNotInMailFolder means the requested file id is not a live
	// file in the mail folder; handlers answer 404.
	errDocumentNotInMailFolder = errors.New("document not in mail folder")
)

func documentAnalysisKey(userID string, fileID int) string {
	return fmt.Sprintf("doc:analysis:v%s:%s:%d", invoicePromptVersion, userID, fileID)
}

func documentAnalyzingKey(userID string, fileID int) string {
	return fmt.Sprintf("doc:analyzing:%s:%d", userID, fileID)
}

func documentIgnoredKey(userID string) string {
	return "doc:ignored:" + userID
}

// documentAnalysisEntry is the cached result of POST /documents/:id/analyze.
// Response is the analyzeBytes body as served, so the list endpoint can hand
// it back without re-deriving anything.
type documentAnalysisEntry struct {
	PdfHash    string          `json:"pdfHash"`
	AnalyzedAt time.Time       `json:"analyzedAt"`
	Response   json.RawMessage `json:"response"`
}

// invoice extracts the normalised InvoiceData from the cached response, or
// nil when the entry is unreadable.
func (e *documentAnalysisEntry) invoice() *claude.InvoiceData {
	if e == nil {
		return nil
	}
	var r struct {
		Invoice *claude.InvoiceData `json:"invoice"`
	}
	if json.Unmarshal(e.Response, &r) != nil || r.Invoice == nil {
		return nil
	}
	claude.NormalizeDocumentType(r.Invoice)
	return r.Invoice
}

// mailDocument is one entry of GET /documents.
type mailDocument struct {
	ID          int    `json:"id"`
	Naam        string `json:"naam"`
	Datum       string `json:"datum"`
	Size        int64  `json:"size"`
	MediaType   string `json:"mediaType"`
	KoppelCount int    `json:"koppelCount"`
	Status      string `json:"status"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle"`
	Ignored     bool   `json:"ignored"`
	// The fields below are present for analysed documents only.
	DocumentType string          `json:"documentType,omitempty"`
	AnalyzedAt   string          `json:"analyzedAt,omitempty"`
	Analysis     json.RawMessage `json:"analysis,omitempty"`
	// Match is the bank line assigned to this document (legacy
	// matchedBankLine shape); Candidates are the document's own ranked lines.
	Match            map[string]any      `json:"match,omitempty"`
	Candidates       []BankLineCandidate `json:"candidates,omitempty"`
	AlreadySubmitted gin.H               `json:"alreadySubmitted,omitempty"`
	// matchedLineID is the id of the bank line in Match, 0 without one. The
	// bank-line inbox reads it to annotate the line with this document.
	matchedLineID int
}

// archiveFolder is one entry of GET /v1/api/folder/directory.
type archiveFolder struct {
	ID        int    `json:"id"`
	Naam      string `json:"naam"`
	ParentID  int    `json:"parentId"`
	IsDeleted bool   `json:"isDeleted"`
}

// listArchiveFolders returns every archive folder, deleted ones included.
func listArchiveFolders(client *eboekhouden.Client) ([]archiveFolder, error) {
	raw, err := client.GetArchiveFolders()
	if err != nil {
		return nil, err
	}
	var folders []archiveFolder
	if err := json.Unmarshal(raw, &folders); err != nil {
		return nil, fmt.Errorf("parsing folders: %w", err)
	}
	return folders, nil
}

// findArchiveFolder looks a folder up by id.
func findArchiveFolder(folders []archiveFolder, id int) (archiveFolder, bool) {
	for _, f := range folders {
		if f.ID == id {
			return f, true
		}
	}
	return archiveFolder{}, false
}

// mailFolderID returns the configured mail folder id, or
// errMailFolderNotConfigured when the administration has none.
func mailFolderID(client *eboekhouden.Client) (int, error) {
	header, err := client.GetFolderHeader(0)
	if err != nil {
		return 0, err
	}
	if header.MenhFolderID <= 0 {
		return 0, errMailFolderNotConfigured
	}
	return header.MenhFolderID, nil
}

// mailFolderFiles returns the mail folder id and its live files.
func mailFolderFiles(client *eboekhouden.Client) (int, []eboekhouden.ArchiveFile, error) {
	id, err := mailFolderID(client)
	if err != nil {
		return 0, nil, err
	}
	files, err := client.ListArchiveFiles(id)
	if err != nil {
		return 0, nil, err
	}
	return id, files, nil
}

// mailFolderFile is the mail-folder check every per-document endpoint runs:
// the file must be a live file in the configured mail folder. It returns the
// mail folder id and the file's listing entry, or errDocumentNotInMailFolder.
func mailFolderFile(client *eboekhouden.Client, fileID int) (int, eboekhouden.ArchiveFile, error) {
	id, files, err := mailFolderFiles(client)
	if err != nil {
		return 0, eboekhouden.ArchiveFile{}, err
	}
	for _, f := range files {
		if f.ID == fileID {
			return id, f, nil
		}
	}
	return id, eboekhouden.ArchiveFile{}, errDocumentNotInMailFolder
}

// parseDocumentID reads a :id path parameter as a positive integer.
func parseDocumentID(s string) (int, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// validateFileIDs checks a fileIds request body: at least one id, at most
// maxArchiveFileIDs, every id positive. Duplicates are dropped. The error text
// is user-facing Dutch.
func validateFileIDs(ids []int) ([]int, error) {
	if len(ids) == 0 {
		return nil, errors.New("Geen documenten opgegeven")
	}
	if len(ids) > maxArchiveFileIDs {
		return nil, fmt.Errorf("Maximaal %d documenten per keer", maxArchiveFileIDs)
	}
	seen := make(map[int]bool, len(ids))
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, errors.New("Ongeldig document-id")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// ignoredDocuments returns the user's ignored file ids. Redis trouble yields
// an empty set: a hidden document reappearing is the harmless failure mode.
func ignoredDocuments(ctx context.Context, rdb *redis.Client, userID string) map[int]bool {
	set := map[int]bool{}
	if rdb == nil {
		return set
	}
	members, err := rdb.SMembers(ctx, documentIgnoredKey(userID)).Result()
	if err != nil {
		return set
	}
	for _, m := range members {
		if id, err := strconv.Atoi(m); err == nil {
			set[id] = true
		}
	}
	return set
}

// countOpenMailDocuments counts the documents the dashboard should draw
// attention to: in the mail folder, not ignored, not yet linked, readable by
// the analyser. An administration without a mail folder has zero.
func countOpenMailDocuments(ctx context.Context, rdb *redis.Client, client *eboekhouden.Client, userID string) (int, error) {
	_, files, err := mailFolderFiles(client)
	if errors.Is(err, errMailFolderNotConfigured) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	ignored := ignoredDocuments(ctx, rdb, userID)
	n := 0
	for _, f := range files {
		if f.KoppelCount == 0 && !ignored[f.ID] && mediaTypeFromExtension(f.Naam) != "" {
			n++
		}
	}
	return n, nil
}

// writeDocumentsError maps the errors of the mail-folder helpers and other
// e-boekhouden calls onto a response: expired session (412 via
// HandleEBoekhoudenSessionExpired), no mail folder (409), file not in the
// mail folder (404), anything else 502 with the upstream message.
func (h *InvoiceHandler) writeDocumentsError(c *gin.Context, sess *session.SessionData, err error) {
	switch {
	case HandleEBoekhoudenSessionExpired(c, sess, h.sessions, h.db, err):
	case errors.Is(err, errMailFolderNotConfigured):
		c.JSON(http.StatusConflict, gin.H{"error": "mail_folder_not_configured", "message": "Er is nog geen mailmap ingesteld"})
	case errors.Is(err, errDocumentNotInMailFolder):
		c.JSON(http.StatusNotFound, gin.H{"error": "Document niet gevonden in de mailmap"})
	default:
		log.Printf("Documents: e-boekhouden error: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
	}
}

// requireRedis answers 503 when the handler has no Redis; the ignore list,
// analysis cache and lock all live there.
func (h *InvoiceHandler) requireRedis(c *gin.Context) bool {
	if h.redis == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Opslag is niet beschikbaar"})
		return false
	}
	return true
}

// ListDocuments handles GET /api/v1/documents. It lists the mail folder with
// the cached analysis state per file and a fresh bank-line assignment, and
// makes no Claude and no download calls.
//
// Not configured: {configured:false}. Configured but the folder is gone
// (missing from the directory list or marked deleted): {configured:false,
// reason:"missing"}. Otherwise {configured:true, folder:{id,name},
// documents:[…], ignoredCount}. Ignored documents are included with
// ignored:true; the frontend hides them.
func (h *InvoiceHandler) ListDocuments(c *gin.Context) {
	sess := session.FromContext(c)
	client := session.ClientFromContext(c)
	if sess == nil || client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}
	ctx := c.Request.Context()

	menh, err := mailFolderID(client)
	if errors.Is(err, errMailFolderNotConfigured) {
		c.JSON(http.StatusOK, gin.H{"configured": false})
		return
	}
	if err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}

	folders, err := listArchiveFolders(client)
	if err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}
	folder, ok := findArchiveFolder(folders, menh)
	if !ok || folder.IsDeleted {
		c.JSON(http.StatusOK, gin.H{"configured": false, "reason": "missing"})
		return
	}

	files, err := client.ListArchiveFiles(menh)
	if err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}

	docs, ignoredCount, err := buildDocumentList(ctx, h.redis, client, sess.UserID, files, fetchImportGridRows(client))
	if err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"configured":   true,
		"folder":       gin.H{"id": folder.ID, "name": folder.Naam},
		"documents":    docs,
		"ignoredCount": ignoredCount,
	})
}

// loadDocumentAnalyses fetches the cached analyses for fileIDs in one round
// trip. Missing or unreadable entries are absent from the result.
func loadDocumentAnalyses(ctx context.Context, rdb *redis.Client, userID string, fileIDs []int) map[int]*documentAnalysisEntry {
	entries := make(map[int]*documentAnalysisEntry)
	if rdb == nil || len(fileIDs) == 0 {
		return entries
	}
	keys := make([]string, len(fileIDs))
	for i, id := range fileIDs {
		keys[i] = documentAnalysisKey(userID, id)
	}
	values, err := rdb.MGet(ctx, keys...).Result()
	if err != nil {
		log.Printf("Documents: analysis cache read error: %v", err)
		return entries
	}
	for i, v := range values {
		s, ok := v.(string)
		if !ok {
			continue
		}
		var entry documentAnalysisEntry
		if json.Unmarshal([]byte(s), &entry) == nil && len(entry.Response) > 0 {
			entries[fileIDs[i]] = &entry
		}
	}
	return entries
}

// loadSubmittedMarkers fetches the invoice:submitted markers for the given
// content hashes in one round trip, keyed by hash.
func loadSubmittedMarkers(ctx context.Context, rdb *redis.Client, userID string, hashes []string) map[string]*invoiceSubmittedMarker {
	markers := make(map[string]*invoiceSubmittedMarker)
	if rdb == nil || len(hashes) == 0 {
		return markers
	}
	keys := make([]string, len(hashes))
	for i, hash := range hashes {
		keys[i] = invoiceSubmittedMarkerKey(userID, hash)
	}
	values, err := rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return markers
	}
	for i, v := range values {
		s, ok := v.(string)
		if !ok {
			continue
		}
		var m invoiceSubmittedMarker
		if json.Unmarshal([]byte(s), &m) == nil {
			markers[hashes[i]] = &m
		}
	}
	return markers
}

// gridRowsSource supplies the import-grid rows a document list is matched
// against. buildDocumentList calls it at most once, and only when an analysed
// document can match a bank line at all.
type gridRowsSource func() ([]map[string]any, error)

// fetchImportGridRows is the gridRowsSource that fetches and parses the
// import grid from e-boekhouden.
func fetchImportGridRows(client *eboekhouden.Client) gridRowsSource {
	return func() ([]map[string]any, error) {
		raw, err := client.GetImportGrid(0, importGridFetchLimit)
		if err != nil {
			return nil, err
		}
		rows, _, err := eboekhouden.ParseImportGrid(raw)
		return rows, err
	}
}

// staticGridRows is the gridRowsSource for rows the caller already has, so a
// request that fetched the grid for its own purposes does not fetch it twice.
func staticGridRows(rows []map[string]any) gridRowsSource {
	return func() ([]map[string]any, error) { return rows, nil }
}

// buildDocumentList turns the mail-folder listing into mailDocuments: cached
// analyses and duplicate markers from Redis, candidate bank lines from the
// grid rows (fetched only when an analysed document can match), and the
// global assignment so no bank line is offered to two documents. Only an
// expired e-boekhouden session is returned as an error; a failed grid fetch
// just leaves the documents unmatched.
//
// GET /documents and the bank-line inbox (ClassifyBatch) both build their
// view from this function, so the two sides always agree on which document
// belongs to which bank line.
func buildDocumentList(ctx context.Context, rdb *redis.Client, client *eboekhouden.Client, userID string, files []eboekhouden.ArchiveFile, grid gridRowsSource) ([]mailDocument, int, error) {
	ids := make([]int, len(files))
	for i, f := range files {
		ids[i] = f.ID
	}
	ignored := ignoredDocuments(ctx, rdb, userID)
	entries := loadDocumentAnalyses(ctx, rdb, userID, ids)

	invoices := make(map[int]*claude.InvoiceData, len(entries))
	var hashes []string
	needGrid := false
	for id, entry := range entries {
		inv := entry.invoice()
		if inv == nil {
			continue
		}
		invoices[id] = inv
		if entry.PdfHash != "" {
			hashes = append(hashes, entry.PdfHash)
		}
		if bankLineMatchable(inv) {
			needGrid = true
		}
	}
	markers := loadSubmittedMarkers(ctx, rdb, userID, hashes)

	candidates := make(map[int][]BankLineCandidate)
	if needGrid {
		rows, err := grid()
		if errors.Is(err, eboekhouden.ErrSessionExpired) {
			return nil, 0, err
		}
		if err != nil {
			log.Printf("Documents: import grid fetch failed, listing without matches: %v", err)
		} else {
			for id, inv := range invoices {
				if bankLineMatchable(inv) {
					candidates[id] = rankBankLineMatches(inv, rows, nil)
				}
			}
		}
	}
	assigned := assignDocumentsToBankLines(candidates)

	docs := make([]mailDocument, 0, len(files))
	ignoredCount := 0
	for _, f := range files {
		inv := invoices[f.ID]
		entry := entries[f.ID]
		doc := mailDocument{
			ID:          f.ID,
			Naam:        f.Naam,
			Datum:       f.Datum,
			Size:        f.Size,
			MediaType:   mediaTypeFromExtension(f.Naam),
			KoppelCount: f.KoppelCount,
			Ignored:     ignored[f.ID],
		}
		if doc.Ignored {
			ignoredCount++
		}
		var marker *invoiceSubmittedMarker
		if entry != nil && entry.PdfHash != "" {
			marker = markers[entry.PdfHash]
		}
		match, hasMatch := assigned[f.ID]
		if inv != nil {
			doc.DocumentType = inv.DocumentType
			doc.AnalyzedAt = entry.AnalyzedAt.Format(time.RFC3339)
			doc.Candidates = candidates[f.ID]
			var matchPtr *BankLineCandidate
			if hasMatch {
				doc.Match = match.legacyJSON()
				doc.matchedLineID = match.ID
				matchPtr = &match
			}
			doc.Analysis = withFreshMatches(entry.Response, matchPtr, candidates[f.ID])
			if marker != nil {
				doc.AlreadySubmitted = submittedMarkerJSON(marker)
			}
		}
		doc.Status = documentStatus(docStatusInput{
			KoppelCount:      f.KoppelCount,
			Ignored:          doc.Ignored,
			MediaType:        doc.MediaType,
			Size:             f.Size,
			Analysis:         inv,
			AlreadySubmitted: marker != nil,
			HasMatch:         hasMatch,
		})
		doc.Title, doc.Subtitle = documentTitle(f, inv)
		docs = append(docs, doc)
	}
	return docs, ignoredCount, nil
}

// withFreshMatches replaces matchedBankLine and bankLineCandidates in a
// cached analyze response with the current assignment, so a client that
// reads the analysis object sees the same match as the list row. Everything
// else in the response is passed through untouched.
func withFreshMatches(response json.RawMessage, match *BankLineCandidate, candidates []BankLineCandidate) json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(response, &fields) != nil || fields == nil {
		return response
	}
	if candidates == nil {
		candidates = []BankLineCandidate{}
	}
	var matched any
	if match != nil {
		matched = match.legacyJSON()
	}
	if b, err := json.Marshal(matched); err == nil {
		fields["matchedBankLine"] = b
	}
	if b, err := json.Marshal(candidates); err == nil {
		fields["bankLineCandidates"] = b
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return response
	}
	return out
}

// AnalyzeDocument handles POST /api/v1/documents/:id/analyze. It downloads
// the file from the mail folder, runs the shared analysis core without R2
// and caches the result for 30 days. A cached analysis is returned without
// downloading unless ?force=true, which also bypasses the content cache so
// Claude really reads the file again. Concurrent analyses of the same file
// are refused with 409.
//
// The response is the analyze response plus archiveFileId, title, subtitle,
// status and analyzedAt.
func (h *InvoiceHandler) AnalyzeDocument(c *gin.Context) {
	sess := session.FromContext(c)
	client := session.ClientFromContext(c)
	if sess == nil || client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}
	if !h.requireRedis(c) {
		return
	}
	id, ok := parseDocumentID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig document-id"})
		return
	}
	ctx := c.Request.Context()
	force := c.Query("force") == "true"

	lockKey := documentAnalyzingKey(sess.UserID, id)
	locked, err := h.redis.SetNX(ctx, lockKey, "1", documentAnalyzeLockTTL).Result()
	if err != nil {
		log.Printf("Documents: lock error for file %d: %v", id, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Opslag is niet beschikbaar"})
		return
	}
	if !locked {
		c.JSON(http.StatusConflict, gin.H{"error": "Wordt al geanalyseerd"})
		return
	}
	// Release with a fresh context: the request context is cancelled when the
	// client aborts, and a lock left behind would block retries for 120 s.
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		h.redis.Del(rctx, lockKey)
	}()

	_, file, err := mailFolderFile(client, id)
	if err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}

	cacheKey := documentAnalysisKey(sess.UserID, id)
	if force {
		h.redis.Del(ctx, cacheKey)
	} else if entry := loadDocumentAnalyses(ctx, h.redis, sess.UserID, []int{id})[id]; entry != nil {
		c.JSON(http.StatusOK, h.documentAnalysisResponse(ctx, sess.UserID, file, entry))
		return
	}

	data, _, _, err := client.DownloadArchiveFile(id, maxInvoiceFileSize)
	if err != nil {
		if HandleEBoekhoudenSessionExpired(c, sess, h.sessions, h.db, err) {
			return
		}
		log.Printf("Documents: download failed for file %d: %v", id, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Downloaden uit e-Boekhouden mislukt"})
		return
	}
	mediaType, ok := detectDocumentMediaType(data)
	if !ok {
		writeUnsupportedMediaType(c)
		return
	}

	resp, err := h.analyzeBytes(c, sess, data, mediaType, file.Naam, analyzeOptions{skipR2: true, refresh: force})
	if err != nil {
		h.writeAnalyzeError(c, sess, err)
		return
	}

	body, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Documents: encoding analysis for file %d: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Analyse opslaan mislukt"})
		return
	}
	pdfHash, _ := resp["pdfHash"].(string)
	entry := &documentAnalysisEntry{PdfHash: pdfHash, AnalyzedAt: time.Now(), Response: body}
	if cached, err := json.Marshal(entry); err == nil {
		if err := h.redis.Set(ctx, cacheKey, cached, documentAnalysisCacheTTL).Err(); err != nil {
			log.Printf("Documents: analysis cache write error for file %d: %v", id, err)
		}
	}
	c.JSON(http.StatusOK, h.documentAnalysisResponse(ctx, sess.UserID, file, entry))
}

// documentAnalysisResponse decorates a cached analysis with the document
// fields the list also shows (archiveFileId, title, subtitle, status,
// analyzedAt). The status uses the analysis' own best bank line; the global
// assignment in GET /documents may still hand that line to another document.
func (h *InvoiceHandler) documentAnalysisResponse(ctx context.Context, userID string, file eboekhouden.ArchiveFile, entry *documentAnalysisEntry) gin.H {
	resp := gin.H{}
	if json.Unmarshal(entry.Response, &resp) != nil {
		resp = gin.H{}
	}
	inv := entry.invoice()
	hasMatch := resp["matchedBankLine"] != nil
	alreadySubmitted := false
	if entry.PdfHash != "" {
		alreadySubmitted = len(loadSubmittedMarkers(ctx, h.redis, userID, []string{entry.PdfHash})) > 0
	}
	ignored := ignoredDocuments(ctx, h.redis, userID)[file.ID]
	title, subtitle := documentTitle(file, inv)
	resp["archiveFileId"] = file.ID
	resp["title"] = title
	resp["subtitle"] = subtitle
	resp["analyzedAt"] = entry.AnalyzedAt.Format(time.RFC3339)
	resp["status"] = documentStatus(docStatusInput{
		KoppelCount:      file.KoppelCount,
		Ignored:          ignored,
		MediaType:        mediaTypeFromExtension(file.Naam),
		Size:             file.Size,
		Analysis:         inv,
		AlreadySubmitted: alreadySubmitted,
		HasMatch:         hasMatch,
	})
	return resp
}

// DocumentContent handles GET /api/v1/documents/:id/content: streams the
// file for an inline preview. The content type is sniffed from the bytes and
// must be PDF, JPEG or PNG (415 otherwise); the response is never cached.
func (h *InvoiceHandler) DocumentContent(c *gin.Context) {
	sess := session.FromContext(c)
	client := session.ClientFromContext(c)
	if sess == nil || client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}
	id, ok := parseDocumentID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig document-id"})
		return
	}
	_, file, err := mailFolderFile(client, id)
	if err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}
	data, _, _, err := client.DownloadArchiveFile(id, maxInvoiceFileSize)
	if err != nil {
		if HandleEBoekhoudenSessionExpired(c, sess, h.sessions, h.db, err) {
			return
		}
		log.Printf("Documents: download failed for file %d: %v", id, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Downloaden uit e-Boekhouden mislukt"})
		return
	}
	mediaType, ok := detectDocumentMediaType(data)
	if !ok {
		writeUnsupportedMediaType(c)
		return
	}
	c.Header("Content-Disposition", inlineContentDisposition(file.Naam, mediaType))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, mediaType, data)
}

// inlineContentDisposition builds "inline; filename=…" for a preview. Path
// separators and control characters are dropped from the name, an empty name
// becomes document.<ext>, and mime.FormatMediaType takes care of quoting and
// of the RFC 5987 filename* form for non-ASCII names.
func inlineContentDisposition(name, mediaType string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	fallback := "document." + documentExtension(mediaType)
	if name == "" || name == "." || name == ".." {
		name = fallback
	}
	if v := mime.FormatMediaType("inline", map[string]string{"filename": name}); v != "" {
		return v
	}
	return mime.FormatMediaType("inline", map[string]string{"filename": fallback})
}

// IgnoreDocument handles POST /api/v1/documents/:id/ignore: hides the
// document in Speedy. Nothing changes in e-boekhouden.
func (h *InvoiceHandler) IgnoreDocument(c *gin.Context) {
	sess := session.FromContext(c)
	client := session.ClientFromContext(c)
	if sess == nil || client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}
	if !h.requireRedis(c) {
		return
	}
	id, ok := parseDocumentID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig document-id"})
		return
	}
	if _, _, err := mailFolderFile(client, id); err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}
	if err := h.redis.SAdd(c.Request.Context(), documentIgnoredKey(sess.UserID), id).Err(); err != nil {
		log.Printf("Documents: ignore write error for file %d: %v", id, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Negeren opslaan mislukt"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "ignored": true})
}

// UnignoreDocument handles DELETE /api/v1/documents/:id/ignore. It skips the
// mail-folder check so a file that has since vanished can still be cleaned
// out of the ignore list.
func (h *InvoiceHandler) UnignoreDocument(c *gin.Context) {
	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	if !h.requireRedis(c) {
		return
	}
	id, ok := parseDocumentID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig document-id"})
		return
	}
	if err := h.redis.SRem(c.Request.Context(), documentIgnoredKey(sess.UserID), id).Err(); err != nil {
		log.Printf("Documents: unignore write error for file %d: %v", id, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Negeren ongedaan maken mislukt"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "ignored": false})
}

// forgetDocument drops the Redis state of a file that left the mail folder:
// its cached analysis (current prompt version) and its ignore flag.
func (h *InvoiceHandler) forgetDocument(ctx context.Context, userID string, fileID int) {
	if h.redis == nil {
		return
	}
	h.redis.Del(ctx, documentAnalysisKey(userID, fileID))
	h.redis.SRem(ctx, documentIgnoredKey(userID), fileID)
}

// DeleteDocument handles DELETE /api/v1/documents/:id: permanently deletes
// the file from the e-boekhouden archive. A file linked to a booking is
// refused with 409.
func (h *InvoiceHandler) DeleteDocument(c *gin.Context) {
	sess := session.FromContext(c)
	client := session.ClientFromContext(c)
	if sess == nil || client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}
	id, ok := parseDocumentID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig document-id"})
		return
	}
	_, file, err := mailFolderFile(client, id)
	if err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}
	if file.KoppelCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "Dit document is gekoppeld aan een boeking"})
		return
	}
	if err := client.DeleteArchiveFile(id); err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}
	h.forgetDocument(c.Request.Context(), sess.UserID, id)
	c.JSON(http.StatusOK, gin.H{"id": id, "deleted": true})
}

// archiveNameFor decides the archive name of a mail-folder file. With a
// cached analysis the name follows the analysis (supplier, number, invoice
// date, type); without one it is the received date plus the current base
// name as supplier and no reference. The date falls back from the invoice
// date to the received date to now, and is returned for the folder choice.
func archiveNameFor(file eboekhouden.ArchiveFile, inv *claude.InvoiceData, now time.Time) (time.Time, string) {
	mediaType := archiveMediaType(file.Naam)
	base, _ := splitFileName(file.Naam)
	datum := now
	if received, ok := parseEBoekhoudenTime(file.Datum); ok {
		datum = received
	}
	if inv == nil {
		return datum, archiveFileName(base, "", datum.Format("2006-01-02"), "", mediaType)
	}
	if d, ok := parseISODate(inv.Datum); ok {
		datum = d
	}
	return datum, archiveFileName(inv.Leverancier, inv.Factuurnummer, datum.Format("2006-01-02"), inv.DocumentType, mediaType)
}

// ArchiveDocuments handles POST /api/v1/documents/archive: renames the given
// mail-folder files by the naming convention and moves them out of the mail
// folder, for "Opruimen" of linked documents and "Verplaatsen naar…".
//
// Body {fileIds:[…], targetFolderId?}. Without targetFolderId each file goes
// to Facturen/<jaar>/<maand> for its date (findOrCreateArchiveFolder). The
// mail folder itself is never a target. Response {moved:[{id, name,
// targetFolderId}], failed:[{id, error}]}; a failure of one file does not
// stop the others.
func (h *InvoiceHandler) ArchiveDocuments(c *gin.Context) {
	sess := session.FromContext(c)
	client := session.ClientFromContext(c)
	if sess == nil || client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}
	var req struct {
		FileIDs        []int `json:"fileIds"`
		TargetFolderID int   `json:"targetFolderId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig verzoek"})
		return
	}
	ids, err := validateFileIDs(req.FileIDs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.TargetFolderID < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldige doelmap"})
		return
	}
	ctx := c.Request.Context()

	menh, files, err := mailFolderFiles(client)
	if err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}
	if req.TargetFolderID != 0 && req.TargetFolderID == menh {
		c.JSON(http.StatusBadRequest, gin.H{"error": "De mailmap kan geen doelmap zijn"})
		return
	}
	byID := make(map[int]eboekhouden.ArchiveFile, len(files))
	for _, f := range files {
		byID[f.ID] = f
	}
	entries := loadDocumentAnalyses(ctx, h.redis, sess.UserID, ids)

	type movedItem struct {
		ID             int    `json:"id"`
		Name           string `json:"name"`
		TargetFolderID int    `json:"targetFolderId"`
	}
	type failedItem struct {
		ID    int    `json:"id"`
		Error string `json:"error"`
	}
	moved := []movedItem{}
	failed := []failedItem{}
	now := time.Now()
	// One folder lookup per month, not per file.
	monthFolders := make(map[string]int)

	for _, id := range ids {
		file, ok := byID[id]
		if !ok {
			failed = append(failed, failedItem{ID: id, Error: "Document niet gevonden in de mailmap"})
			continue
		}
		datum, name := archiveNameFor(file, entries[id].invoice(), now)

		target := req.TargetFolderID
		if target == 0 {
			month := datum.Format("2006-01")
			if folderID, ok := monthFolders[month]; ok {
				target = folderID
			} else {
				folderID, err := h.findOrCreateArchiveFolder(client, datum)
				if err != nil {
					if HandleEBoekhoudenSessionExpired(c, sess, h.sessions, h.db, err) {
						return
					}
					log.Printf("Documents: archive folder lookup failed for file %d: %v", id, err)
					failed = append(failed, failedItem{ID: id, Error: "Doelmap aanmaken mislukt"})
					continue
				}
				monthFolders[month] = folderID
				target = folderID
			}
		}
		if target == menh {
			failed = append(failed, failedItem{ID: id, Error: "De mailmap kan geen doelmap zijn"})
			continue
		}

		finalName, err := archiveMailDocument(client, file, menh, name, target)
		if err != nil {
			if HandleEBoekhoudenSessionExpired(c, sess, h.sessions, h.db, err) {
				return
			}
			// Error class only: upstream rename/move messages may echo the file name.
			log.Printf("Documents: archiving file %d failed: %T", id, err)
			failed = append(failed, failedItem{ID: id, Error: archiveErrorMessage(err)})
			continue
		}
		h.forgetDocument(ctx, sess.UserID, id)
		moved = append(moved, movedItem{ID: id, Name: finalName, TargetFolderID: target})
	}
	c.JSON(http.StatusOK, gin.H{"moved": moved, "failed": failed})
}

// archiveErrorMessage turns an archiveMailDocument error into Dutch text that
// says what state the file is in.
func archiveErrorMessage(err error) string {
	var renameErr *archiveRenameError
	var moveErr *archiveMoveError
	switch {
	case errors.As(err, &moveErr):
		return "Verplaatsen mislukt, het bestand is wel hernoemd: " + moveErr.err.Error()
	case errors.As(err, &renameErr):
		return "Hernoemen mislukt: " + renameErr.err.Error()
	default:
		return "Opruimen mislukt: " + err.Error()
	}
}

// SetupMailFolder handles POST /api/v1/documents/setup. Body {folderId?}.
// With folderId that existing, non-deleted folder becomes the mail folder.
// Without it a root folder named "Inbox" is reused or created. When a mail
// folder is already configured and still exists, a request without folderId
// is refused with 409; a configured folder that no longer exists ("missing")
// may be replaced either way. Response {folder:{id,name}}.
func (h *InvoiceHandler) SetupMailFolder(c *gin.Context) {
	sess := session.FromContext(c)
	client := session.ClientFromContext(c)
	if sess == nil || client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}
	var req struct {
		FolderID int `json:"folderId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldig verzoek"})
		return
	}
	if req.FolderID < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldige map"})
		return
	}

	header, err := client.GetFolderHeader(0)
	if err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}
	folders, err := listArchiveFolders(client)
	if err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}
	if header.MenhFolderID > 0 && req.FolderID == 0 {
		if current, ok := findArchiveFolder(folders, header.MenhFolderID); ok && !current.IsDeleted {
			c.JSON(http.StatusConflict, gin.H{"error": "Er is al een mailmap ingesteld"})
			return
		}
	}

	var target archiveFolder
	if req.FolderID > 0 {
		f, ok := findArchiveFolder(folders, req.FolderID)
		if !ok || f.IsDeleted {
			c.JSON(http.StatusNotFound, gin.H{"error": "Map niet gevonden"})
			return
		}
		target = f
	} else {
		for _, f := range folders {
			if f.ParentID == 0 && !f.IsDeleted && f.Naam == mailFolderDefaultName {
				target = f
				break
			}
		}
		if target.ID == 0 {
			payload, _ := json.Marshal(map[string]any{"parentFolderId": 0, "name": mailFolderDefaultName})
			raw, err := client.CreateArchiveFolder(payload)
			if err != nil {
				h.writeDocumentsError(c, sess, err)
				return
			}
			if json.Unmarshal(raw, &target) != nil || target.ID == 0 {
				c.JSON(http.StatusBadGateway, gin.H{"error": "Onverwacht antwoord van e-Boekhouden bij het aanmaken van de map"})
				return
			}
			if target.Naam == "" {
				target.Naam = mailFolderDefaultName
			}
		}
	}

	if err := client.SetMailFolder(target.ID); err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"folder": gin.H{"id": target.ID, "name": target.Naam}})
}

// GetOpenPosts handles GET /api/v1/openposts?soort=Crediteur|Debiteur and
// returns the outstanding invoices as a JSON array of eboekhouden.OpenPost.
func (h *InvoiceHandler) GetOpenPosts(c *gin.Context) {
	sess := session.FromContext(c)
	client := session.ClientFromContext(c)
	if sess == nil || client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}
	soort := c.Query("soort")
	if soort != eboekhouden.OpenPostCrediteur && soort != eboekhouden.OpenPostDebiteur {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldige soort, gebruik Crediteur of Debiteur"})
		return
	}
	raw, err := client.GetOpenPostGrid(soort)
	if err != nil {
		h.writeDocumentsError(c, sess, err)
		return
	}
	posts, err := eboekhouden.ParseOpenPostGrid(raw)
	if err != nil {
		log.Printf("Documents: open post grid parse error: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Onverwacht antwoord van e-Boekhouden"})
		return
	}
	c.JSON(http.StatusOK, posts)
}
