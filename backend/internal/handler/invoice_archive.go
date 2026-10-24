package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// The archive side of SubmitFull and SubmitReceipt, in three phases:
//
//  1. prepareArchiveSource resolves where the document comes from and checks
//     everything that can be checked before a mutation is booked, so a bad
//     request never leaves a half-done booking: a mail-folder document must
//     be in the mail folder and its target folder must exist outside it; an
//     upload must be a PDF, JPG or PNG.
//  2. stageArchiveFile makes sure there is a file id to link: a mail-folder
//     document already has one, an upload is uploaded now under its archive
//     name (archiveFileName), made unique in the target folder.
//  3. completeArchive runs after the mutation is linked: a mail-folder
//     document is renamed and moved out of the mail folder, and any failure
//     is reported as archiveWarning on an otherwise successful booking.

// badRequestError is a request problem with a Dutch message for the client.
type badRequestError struct{ msg string }

func (e *badRequestError) Error() string { return e.msg }

// Target-folder validation outcomes (validateTargetFolder). The texts are
// user-facing.
var (
	errTargetFolderMissing = errors.New("Doelmap niet gevonden")
	errTargetFolderDeleted = errors.New("Doelmap bestaat niet meer")
	errTargetIsMailFolder  = errors.New("De mailmap kan geen doelmap zijn")
	errTargetInMailFolder  = errors.New("Een submap van de mailmap kan geen doelmap zijn")
)

// archiveSourceInput is the archive part of a submit request.
type archiveSourceInput struct {
	archiveFileID  int
	targetFolderID int
	pdfBase64      string
	uploadKey      string
}

// archiveSource is the document a booking archives: a file that already sits
// in the mail folder, or bytes still to be uploaded. The zero value means
// there is nothing to archive.
type archiveSource struct {
	// Mail-folder document (fileID > 0). targetFolderID is the validated
	// explicit target, or 0 for Facturen/<jaar>/<maand>; folders is the
	// directory listing used for that validation, kept for the breadcrumb.
	fileID         int
	file           eboekhouden.ArchiveFile
	mailFolderID   int
	targetFolderID int
	folders        []archiveFolder

	// Upload (len(data) > 0).
	data []byte

	// mediaType decides the extension of the archive name: sniffed from the
	// bytes for an upload, taken from the file name for a mail-folder document.
	mediaType string
}

func (s archiveSource) fromMailFolder() bool { return s.fileID > 0 }

// archiveNameInput is what a booking contributes to the archive name.
type archiveNameInput struct {
	leverancier string
	kenmerk     string
	datum       string
	docType     string
}

// archiveName builds the archive name for this source by the naming
// convention. A mail-folder document whose booking date is unreadable falls
// back to the date the file was received, as the convention prescribes.
func (s archiveSource) archiveName(in archiveNameInput) string {
	datum := in.datum
	if _, ok := parseISODate(datum); !ok && s.fromMailFolder() {
		datum = s.file.Datum
	}
	return archiveFileName(in.leverancier, in.kenmerk, datum, in.docType, s.mediaType)
}

// submitFullDocType reduces the documentType a SubmitFull request may carry
// to the two types that endpoint books: creditnota when it says so, factuur
// otherwise. It only influences the archive name.
func submitFullDocType(s string) string {
	if strings.ToLower(strings.TrimSpace(s)) == claude.DocumentTypeCreditnota {
		return claude.DocumentTypeCreditnota
	}
	return claude.DocumentTypeFactuur
}

// bookingDate parses the request's datum, falling back to now, for the
// Facturen/<jaar>/<maand> folder choice.
func bookingDate(datum string) time.Time {
	if t, ok := parseISODate(datum); ok {
		return t
	}
	return time.Now()
}

// validateArchiveSource checks that a request names at most one document
// source and a sane archive file id.
func validateArchiveSource(archiveFileID int, pdfBase64, uploadKey string) error {
	if archiveFileID < 0 {
		return &badRequestError{"Ongeldig document-id"}
	}
	if archiveFileID > 0 && (pdfBase64 != "" || uploadKey != "") {
		return &badRequestError{"Kies één bron voor het document"}
	}
	return nil
}

// isFolderWithin reports whether folderID is ancestorID itself or lies
// anywhere below it. It walks the parent chain and stops at the root, at a
// parent that is not in the listing, or when a folder repeats (a corrupted
// cycle), so it always terminates.
func isFolderWithin(folders []archiveFolder, folderID, ancestorID int) bool {
	seen := make(map[int]bool, len(folders))
	for id := folderID; id != 0 && !seen[id]; {
		if id == ancestorID {
			return true
		}
		seen[id] = true
		f, ok := findArchiveFolder(folders, id)
		if !ok {
			return false
		}
		id = f.ParentID
	}
	return false
}

// mailFolderTargetError says why targetID cannot receive a document that
// leaves the mail folder: it is the mail folder itself (errTargetIsMailFolder)
// or lies inside it (errTargetInMailFolder). nil when it is neither. A nil
// listing places nothing inside the mail folder, so only the identity check
// remains.
func mailFolderTargetError(folders []archiveFolder, targetID, mailFolderID int) error {
	switch {
	case targetID == mailFolderID:
		return errTargetIsMailFolder
	case isFolderWithin(folders, targetID, mailFolderID):
		return errTargetInMailFolder
	}
	return nil
}

// validateTargetFolder checks an explicit target folder for a mail-folder
// document: it must exist, must not be deleted and must be neither the mail
// folder nor one of its descendants.
func validateTargetFolder(folders []archiveFolder, targetID, mailFolderID int) error {
	f, ok := findArchiveFolder(folders, targetID)
	switch {
	case !ok:
		return errTargetFolderMissing
	case f.IsDeleted:
		return errTargetFolderDeleted
	}
	return mailFolderTargetError(folders, targetID, mailFolderID)
}

// archiveFolderPath is the breadcrumb of a folder, "Facturen / 2026 / 10
// Oktober", built by walking the parent chain like the frontend does. A
// folder that is not in the listing has no path; a cycle ends the walk.
func archiveFolderPath(folders []archiveFolder, folderID int) string {
	var segments []string
	seen := make(map[int]bool, len(folders))
	for id := folderID; id != 0 && !seen[id]; {
		seen[id] = true
		f, ok := findArchiveFolder(folders, id)
		if !ok {
			break
		}
		segments = append([]string{f.Naam}, segments...)
		id = f.ParentID
	}
	return strings.Join(segments, " / ")
}

// prepareArchiveSource resolves the document of a submit request before
// anything is booked. Errors are *badRequestError (400),
// claude.ErrUnsupportedMediaType (415) or an e-boekhouden error, possibly
// eboekhouden.ErrSessionExpired; writeArchiveSourceError maps them.
func (h *InvoiceHandler) prepareArchiveSource(ctx context.Context, client *eboekhouden.Client, in archiveSourceInput) (archiveSource, error) {
	if err := validateArchiveSource(in.archiveFileID, in.pdfBase64, in.uploadKey); err != nil {
		return archiveSource{}, err
	}
	if in.archiveFileID > 0 {
		return prepareMailFolderSource(client, in.archiveFileID, in.targetFolderID)
	}
	return h.prepareUploadSource(ctx, in)
}

// prepareMailFolderSource checks that fileID is a live file in the mail
// folder and, when a target folder was picked, that the target is valid.
func prepareMailFolderSource(client *eboekhouden.Client, fileID, targetFolderID int) (archiveSource, error) {
	mailFolderID, file, err := mailFolderFile(client, fileID)
	switch {
	case errors.Is(err, errDocumentNotInMailFolder), errors.Is(err, errMailFolderNotConfigured):
		return archiveSource{}, &badRequestError{"Document niet gevonden in de mailmap"}
	case err != nil:
		return archiveSource{}, err
	}
	src := archiveSource{fileID: fileID, file: file, mailFolderID: mailFolderID, mediaType: archiveMediaType(file.Naam)}
	if targetFolderID > 0 {
		folders, err := listArchiveFolders(client)
		if err != nil {
			return archiveSource{}, err
		}
		if err := validateTargetFolder(folders, targetFolderID, mailFolderID); err != nil {
			return archiveSource{}, &badRequestError{err.Error()}
		}
		src.targetFolderID = targetFolderID
		src.folders = folders
	}
	return src, nil
}

// prepareUploadSource decodes the inlined document or fetches it from R2 by
// its upload key. As before, an undecodable body or a failed R2 download is
// logged and the booking goes on without an archive file; bytes that are not
// a PDF, JPG or PNG are refused, as the analyser refuses them.
func (h *InvoiceHandler) prepareUploadSource(ctx context.Context, in archiveSourceInput) (archiveSource, error) {
	var data []byte
	switch {
	case in.pdfBase64 != "":
		decoded, err := base64.StdEncoding.DecodeString(in.pdfBase64)
		if err != nil {
			log.Printf("Submit: invalid pdfBase64: %v", err)
			return archiveSource{}, nil
		}
		data = decoded
	case in.uploadKey != "" && h.r2 != nil:
		downloaded, _, err := h.r2.Download(ctx, in.uploadKey)
		if err != nil {
			log.Printf("R2 download error for %s: %v", in.uploadKey, err)
			return archiveSource{}, nil
		}
		data = downloaded
	}
	if len(data) == 0 {
		return archiveSource{}, nil
	}
	mediaType, ok := detectDocumentMediaType(data)
	if !ok {
		return archiveSource{}, claude.ErrUnsupportedMediaType
	}
	return archiveSource{data: data, mediaType: mediaType}, nil
}

// writeArchiveSourceError maps a prepareArchiveSource error onto the response.
func (h *InvoiceHandler) writeArchiveSourceError(c *gin.Context, sess *session.SessionData, err error) {
	var bad *badRequestError
	switch {
	case errors.As(err, &bad):
		c.JSON(http.StatusBadRequest, gin.H{"error": bad.msg})
	case errors.Is(err, claude.ErrUnsupportedMediaType):
		writeUnsupportedMediaType(c)
	case HandleEBoekhoudenSessionExpired(c, sess, h.sessions, h.db, err):
		// Response written: the frontend shows the reconnect prompt.
	default:
		log.Printf("Submit: archive source check failed: %s", errClass(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "Document controleren in e-Boekhouden mislukt"})
	}
}

// archiveOutcome is the archive side of a booking as reported to the client.
type archiveOutcome struct {
	// fileID is the archive file linked to the mutation, 0 when nothing was
	// archived. It is not part of the response.
	fileID int

	linked  bool
	renamed bool
	moved   bool
	// name is the name the file carries after the booking.
	name             string
	targetFolderID   int
	targetFolderPath string
	// warning is Dutch text for the client when the archive step failed while
	// the booking itself succeeded, like paymentWarning.
	warning string
}

// addTo writes the outcome into a submit response: archived, linked, renamed
// and moved are always present; archiveName, targetFolderId,
// targetFolderPath and archiveWarning only when they have a value.
func (o archiveOutcome) addTo(resp gin.H) {
	resp["archived"] = o.fileID > 0
	resp["linked"] = o.linked
	resp["renamed"] = o.renamed
	resp["moved"] = o.moved
	if o.name != "" {
		resp["archiveName"] = o.name
	}
	if o.targetFolderID > 0 {
		resp["targetFolderId"] = o.targetFolderID
	}
	if o.targetFolderPath != "" {
		resp["targetFolderPath"] = o.targetFolderPath
	}
	if o.warning != "" {
		resp["archiveWarning"] = o.warning
	}
}

// stageArchiveFile makes sure the booking has an archive file to link. A
// mail-folder document already has its id. An upload goes into the folder the
// user picked, or into Facturen/<jaar>/<maand> when none was picked and R2 is
// configured (the legacy flow); without either, nothing is archived, which is
// an explicit opt-out for self-hosted clients. The upload is best-effort: a
// failure is logged and leaves fileID 0.
func (h *InvoiceHandler) stageArchiveFile(client *eboekhouden.Client, src archiveSource, pickedFolderID int, datum time.Time, name string) archiveOutcome {
	if src.fromMailFolder() {
		return archiveOutcome{fileID: src.fileID, name: src.file.Naam}
	}
	if len(src.data) == 0 {
		return archiveOutcome{}
	}
	folderID := pickedFolderID
	if folderID == 0 && h.r2 != nil {
		id, err := h.findOrCreateArchiveFolder(client, datum)
		if err != nil {
			log.Printf("Archive folder error: %s", errClass(err))
		} else {
			folderID = id
		}
	}
	if folderID <= 0 {
		return archiveOutcome{}
	}
	id, finalName := uploadAndResolveArchiveFile(client, src.data, folderID, name)
	if id == 0 {
		return archiveOutcome{}
	}
	return archiveOutcome{fileID: id, name: finalName, targetFolderID: folderID}
}

// uploadAndResolveArchiveFile uploads data into folderID under name, made
// unique against the folder's current files first (what overwrite:false does
// on a duplicate name is unknown, so the collision is avoided instead), and
// returns the id e-boekhouden assigned together with the final name. The
// upload endpoint answers with the folder id, not the file id, so the folder
// is listed again and the file found by its exact name. Any failure is
// logged and yields 0.
func uploadAndResolveArchiveFile(client *eboekhouden.Client, data []byte, folderID int, name string) (int, string) {
	if len(data) == 0 || folderID <= 0 {
		return 0, ""
	}
	existing, err := client.ListArchiveFiles(folderID)
	if err != nil {
		log.Printf("Archive file list error before upload: %s", errClass(err))
		return 0, ""
	}
	finalName := uniqueName(name, fileNameSet(existing))
	payload, _ := json.Marshal(map[string]any{
		"fileName":  finalName,
		"data":      base64.StdEncoding.EncodeToString(data),
		"overwrite": false,
		"folderId":  folderID,
	})
	if _, err := client.UploadArchiveFile(payload); err != nil {
		log.Printf("Archive upload error: %s", errClass(err))
		return 0, ""
	}
	files, err := client.ListArchiveFiles(folderID)
	if err != nil {
		log.Printf("Archive file list error after upload: %s", errClass(err))
		return 0, ""
	}
	for _, f := range files {
		if f.Naam == finalName {
			return f.ID, finalName
		}
	}
	log.Printf("Archive upload: file not found in folder %d after upload", folderID)
	return 0, ""
}

// linkArchiveFile links an archive file to a mutation and reports whether
// e-boekhouden accepted the link. Nothing is linked without a file or a
// mutation number.
func linkArchiveFile(client *eboekhouden.Client, fileID, mutNr int) bool {
	if fileID <= 0 || mutNr <= 0 {
		return false
	}
	payload, _ := json.Marshal(map[string]any{
		"koppelId":   mutNr,
		"folders":    []map[string]any{{"id": fileID, "soort": "F"}},
		"koppelType": "MUT",
	})
	if _, err := client.LinkFileToMutation(payload); err != nil {
		log.Printf("File link error for file %d, mutNr %d: %s", fileID, mutNr, errClass(err))
		return false
	}
	return true
}

// completeArchive runs after the mutation is booked and the file link was
// attempted. For an upload it only records the link result. For a
// mail-folder document it renames the file by the convention and moves it out
// of the mail folder, provided the link succeeded: a file that could not be
// linked stays where it is, so the user can still find it. Every failure in
// this phase becomes a warning; the booking itself stands.
//
// What the user ends up with per failure: link failed, or no target folder:
// the file is unchanged in the mail folder. Rename failed: same. Move failed:
// the file carries its new name and is still in the mail folder, linked, so
// it shows as "linked" and can be cleaned up with POST /documents/archive.
// Only when the file really left the mail folder is its Redis state dropped
// (forgetDocument); otherwise the analysis stays so that cleanup names it
// properly.
func (h *InvoiceHandler) completeArchive(ctx context.Context, client *eboekhouden.Client, userID string, src archiveSource, arch archiveOutcome, linked bool, datum time.Time, name string) archiveOutcome {
	arch.linked = linked
	if !src.fromMailFolder() {
		return arch
	}
	if !linked {
		arch.warning = "Het document kon niet aan de boeking worden gekoppeld en blijft in de mailmap staan."
		return arch
	}

	target, folders := src.targetFolderID, src.folders
	if target == 0 {
		id, err := h.findOrCreateArchiveFolder(client, datum)
		if err != nil {
			log.Printf("Submit: archive folder lookup failed for file %d: %s", src.fileID, errClass(err))
			arch.warning = "Doelmap aanmaken mislukt, het bestand staat nog in de mailmap: " + err.Error()
			return arch
		}
		target = id
		// Fresh listing: the lookup may have created folders. Only the
		// breadcrumb and the descendant check depend on it.
		if folders, err = listArchiveFolders(client); err != nil {
			log.Printf("Submit: folder listing failed for file %d: %s", src.fileID, errClass(err))
			folders = nil
		}
	}
	if target == src.mailFolderID || isFolderWithin(folders, target, src.mailFolderID) {
		arch.warning = "De doelmap ligt in de mailmap, het bestand is niet verplaatst. Kies een andere map."
		return arch
	}

	finalName, err := archiveMailDocument(client, src.file, src.mailFolderID, name, target)
	if finalName != "" {
		arch.name = finalName
		arch.renamed = finalName != src.file.Naam
	}
	if err != nil {
		// Error class only: upstream rename/move messages may echo the file name.
		log.Printf("Submit: archiving file %d failed: %s", src.fileID, errClass(err))
		arch.warning = archiveErrorMessage(err)
		return arch
	}
	arch.moved = true
	arch.targetFolderID = target
	arch.targetFolderPath = archiveFolderPath(folders, target)
	h.forgetDocument(ctx, userID, src.fileID)
	return arch
}
