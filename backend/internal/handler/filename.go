package handler

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"golang.org/x/text/unicode/norm"
)

// Archive file-name contract (see the plan, "Naamconventie archiefbestanden"):
//
//	{datum}_{leverancier}_{kenmerk}[_{n}].{ext}
//	2026-10-12_Anthropic_INV-0042.pdf
//	2026-10-03_Albert-Heijn_bonnetje.jpg
//
// Only [A-Za-z0-9._-] may appear, "_" is used exclusively as the field
// separator (so a split on "_" parses the name), the date comes first so the
// list sorts chronologically and the name never starts with "-" or ".", and
// the same input always yields the same name.
const (
	archiveNameMaxSupplier     = 40
	archiveNameMaxReference    = 30
	archiveNameMaxExtension    = 10
	archiveNameMaxTotal        = 100
	archiveNameUnknownSupplier = "onbekend"
	// archiveNameUnknownDate is used when datum cannot be parsed. Callers are
	// expected to fall back to the file's received date (and then to today)
	// before calling, so this placeholder only guards against bad input; it
	// keeps the ISO shape so sorting and parsing still work.
	archiveNameUnknownDate = "0000-00-00"
)

// archiveFileName builds the archive name for a document. datum is the
// invoice date (or the received date when there is none) in ISO form, with
// or without a time part. docType is one of the claude.DocumentType* values
// and only decides the fallback reference for a document without a number.
// mediaType is a media type (application/pdf, image/jpeg, image/png); for
// files outside the analyser's reach a bare extension such as "xml" is
// accepted as well so the original type is kept.
func archiveFileName(leverancier, kenmerk, datum, docType, mediaType string) string {
	date := archiveNameUnknownDate
	if t, ok := parseISODate(datum); ok {
		date = t.Format("2006-01-02")
	}

	supplier := archiveNameField(stripLegalForms(transliterate(leverancier)), archiveNameMaxSupplier)
	if supplier == "" {
		supplier = archiveNameUnknownSupplier
	}

	ref := archiveNameField(transliterate(kenmerk), archiveNameMaxReference)
	if ref == "" {
		switch strings.ToLower(strings.TrimSpace(docType)) {
		case claude.DocumentTypeBonnetje:
			ref = "bonnetje"
		case claude.DocumentTypeCreditnota:
			ref = "creditnota"
		default:
			ref = "document"
		}
	}

	ext := archiveNameExtension(mediaType)

	name := date + "_" + supplier + "_" + ref + "." + ext
	// The field limits keep the name well under the total limit; this guard
	// only exists so the contract holds even if those limits change.
	if over := len(name) - archiveNameMaxTotal; over > 0 {
		supplier = archiveNameField(supplier, max(1, len(supplier)-over))
		name = date + "_" + supplier + "_" + ref + "." + ext
	}
	if over := len(name) - archiveNameMaxTotal; over > 0 {
		ref = archiveNameField(ref, max(1, len(ref)-over))
		name = date + "_" + supplier + "_" + ref + "." + ext
	}
	return name
}

// transliterations covers Latin letters that NFKD does not decompose into a
// base letter plus combining marks.
var transliterations = map[rune]string{
	'ß': "ss", 'æ': "ae", 'Æ': "AE", 'ø': "o", 'Ø': "O", 'œ': "oe", 'Œ': "OE",
	'ł': "l", 'Ł': "L", 'đ': "d", 'Đ': "D", 'ð': "d", 'Ð': "D", 'þ': "th", 'Þ': "Th",
}

// transliterate maps accented Latin text to ASCII letters: NFKD decomposition
// with the combining marks removed ("Café" → "Cafe", "ﬁ" → "fi"), plus the
// table above. Anything still non-ASCII afterwards is left for
// archiveNameField to turn into a separator.
func transliterate(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if repl, ok := transliterations[r]; ok {
			b.WriteString(repl)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// legalFormKey reduces a name token to the lowercase letters and digits it
// contains, so "B.V.", "BV" and "b.v.," all become "bv" and can be looked up
// in legalFormTokens (shared with the bank-line matcher).
func legalFormKey(tok string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(tok) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// stripLegalForms removes legal-form suffixes from the end of a company name:
// "Coolblue B.V." → "Coolblue", "Amazon EU S.à r.l." → "Amazon EU". Only
// trailing tokens are removed, so a legal form in the middle of a name stays.
// Tokens without letters or digits (a stray "&" or ",") count as removable
// too, and a two-token legal form ("S.a r.l." → "sarl") is recognised as one.
func stripLegalForms(name string) string {
	tokens := strings.Fields(name)
	for len(tokens) > 0 {
		last := legalFormKey(tokens[len(tokens)-1])
		if last == "" || legalFormTokens[last] {
			tokens = tokens[:len(tokens)-1]
			continue
		}
		if len(tokens) >= 2 && legalFormTokens[legalFormKey(tokens[len(tokens)-2])+last] {
			tokens = tokens[:len(tokens)-2]
			continue
		}
		break
	}
	return strings.Join(tokens, " ")
}

// archiveNameField cleans one name field: letters and digits are kept, every
// other character becomes "-", runs of "-" collapse into one, the result is
// cut to maxLen and never starts or ends with "-". Case is preserved.
func archiveNameField(s string, maxLen int) string {
	var b strings.Builder
	dash := true // suppress a leading dash
	for _, r := range s {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if isAlnum {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	out := b.String()
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	return strings.Trim(out, "-")
}

// archiveNameExtension derives the lowercase extension from a media type
// (pdf, jpg, png). Other values are read as a bare extension or an unknown
// media type's subtype, cleaned to [a-z0-9] and capped; "jpeg" always becomes
// "jpg". An empty result falls back to "bin".
func archiveNameExtension(mediaType string) string {
	if ext := documentExtension(mediaType); ext != "" {
		return ext
	}
	ext := strings.ToLower(strings.TrimSpace(mediaType))
	if i := strings.LastIndex(ext, "/"); i >= 0 {
		ext = ext[i+1:]
	}
	ext = strings.TrimPrefix(ext, ".")
	var b strings.Builder
	for _, r := range ext {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	ext = b.String()
	if ext == "jpeg" {
		ext = "jpg"
	}
	if len(ext) > archiveNameMaxExtension {
		ext = ext[:archiveNameMaxExtension]
	}
	if ext == "" {
		return "bin"
	}
	return ext
}

// uniqueName returns name unchanged when it is not in existing, otherwise the
// first of name_2, name_3, … (the counter goes before the extension) that is
// free. Names are compared case-insensitively; existing may hold either
// spelling (see fileNameSet).
func uniqueName(name string, existing map[string]bool) string {
	taken := func(n string) bool {
		return existing[n] || existing[strings.ToLower(n)]
	}
	if !taken(name) {
		return name
	}
	base, ext := name, ""
	if i := strings.LastIndex(name, "."); i > 0 {
		base, ext = name[:i], name[i:]
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s_%d%s", base, n, ext)
		if !taken(candidate) {
			return candidate
		}
	}
}

// fileNameSet returns the lowercased names of files, for uniqueName.
func fileNameSet(files []eboekhouden.ArchiveFile) map[string]bool {
	set := make(map[string]bool, len(files))
	for _, f := range files {
		set[strings.ToLower(f.Naam)] = true
	}
	return set
}

// archiveRenameError reports that archiveMailDocument failed before or during
// the rename: the file is untouched and still in the mail folder under its
// old name.
type archiveRenameError struct{ err error }

func (e *archiveRenameError) Error() string { return "renaming archive file: " + e.err.Error() }
func (e *archiveRenameError) Unwrap() error { return e.err }

// archiveMoveError reports that the rename succeeded but the move did not: the
// file carries its new name and is still in the mail folder.
type archiveMoveError struct{ err error }

func (e *archiveMoveError) Error() string { return "moving archive file: " + e.err.Error() }
func (e *archiveMoveError) Unwrap() error { return e.err }

// archiveMailDocument gives a mail-folder file its archive name and moves it
// to targetFolderID. The name is made unique against the target folder's
// current files (uniqueName), the rename happens while the file is still in
// mailFolderID (the rename endpoint needs the current parent), and the move
// keeps the file id so links to mutations stay intact.
//
// The returned name is the one the file ends up with. Errors are typed so a
// caller can report partial success: *archiveRenameError means nothing
// changed, *archiveMoveError means the file was renamed but not moved. Both
// unwrap to the underlying error, so eboekhouden.ErrSessionExpired is still
// detectable with errors.Is.
func archiveMailDocument(client *eboekhouden.Client, file eboekhouden.ArchiveFile, mailFolderID int, name string, targetFolderID int) (string, error) {
	if targetFolderID == mailFolderID {
		return "", &archiveRenameError{err: fmt.Errorf("target folder %d is the mail folder", targetFolderID)}
	}
	existing, err := client.ListArchiveFiles(targetFolderID)
	if err != nil {
		return "", &archiveRenameError{err: fmt.Errorf("listing target folder: %w", err)}
	}
	finalName := uniqueName(name, fileNameSet(existing))
	if finalName != file.Naam {
		if err := client.RenameArchiveFile(file.ID, mailFolderID, finalName); err != nil {
			return "", &archiveRenameError{err: err}
		}
	}
	if _, err := client.MoveArchiveFiles(targetFolderID, []int{file.ID}); err != nil {
		return finalName, &archiveMoveError{err: err}
	}
	return finalName, nil
}
