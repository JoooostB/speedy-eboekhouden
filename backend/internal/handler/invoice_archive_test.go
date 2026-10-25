package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// testFolders is the archive tree used by the folder helpers' tests:
//
//	Inbox (100, the mail folder)
//	  Sub (101)
//	    Deep (102)
//	Facturen (200)
//	  2026 (201)
//	    10 Oktober (202)
//	Oud (300, deleted)
//	Lus A (400) <-> Lus B (401)   a corrupted parent cycle
//	Wees (500)                    parent 999 does not exist
var testFolders = []archiveFolder{
	{ID: 100, Naam: "Inbox", ParentID: 0},
	{ID: 101, Naam: "Sub", ParentID: 100},
	{ID: 102, Naam: "Deep", ParentID: 101},
	{ID: 200, Naam: "Facturen", ParentID: 0},
	{ID: 201, Naam: "2026", ParentID: 200},
	{ID: 202, Naam: "10 Oktober", ParentID: 201},
	{ID: 300, Naam: "Oud", ParentID: 0, IsDeleted: true},
	{ID: 400, Naam: "Lus A", ParentID: 401},
	{ID: 401, Naam: "Lus B", ParentID: 400},
	{ID: 500, Naam: "Wees", ParentID: 999},
}

func TestValidateArchiveSource(t *testing.T) {
	tests := []struct {
		name                 string
		fileID               int
		pdfBase64, uploadKey string
		wantErr              string
	}{
		{"nothing", 0, "", "", ""},
		{"base64 only", 0, "b64", "", ""},
		{"upload key only", 0, "", "key", ""},
		{"base64 and upload key", 0, "b64", "key", ""},
		{"archive file only", 5, "", "", ""},
		{"archive file and base64", 5, "b64", "", "Kies één bron voor het document."},
		{"archive file and upload key", 5, "", "key", "Kies één bron voor het document."},
		{"archive file and both", 5, "b64", "key", "Kies één bron voor het document."},
		{"negative id", -1, "", "", "Ongeldig document-id."},
		{"negative id beats double source", -1, "b64", "", "Ongeldig document-id."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateArchiveSource(tc.fileID, tc.pdfBase64, tc.uploadKey)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var bad *badRequestError
			if !errors.As(err, &bad) {
				t.Fatalf("err = %v, want *badRequestError", err)
			}
			if bad.msg != tc.wantErr {
				t.Errorf("message = %q, want %q", bad.msg, tc.wantErr)
			}
		})
	}
}

func TestIsFolderWithin(t *testing.T) {
	tests := []struct {
		name               string
		folderID, ancestor int
		want               bool
	}{
		{"direct child", 101, 100, true},
		{"deep descendant", 102, 100, true},
		{"unrelated tree", 202, 100, false},
		{"the folder itself", 100, 100, true},
		{"missing folder", 999, 100, false},
		{"root is never within", 0, 100, false},
		{"orphan with missing parent", 500, 100, false},
		{"cycle terminates", 400, 100, false},
		{"cycle member is within the other", 400, 401, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFolderWithin(testFolders, tc.folderID, tc.ancestor); got != tc.want {
				t.Errorf("isFolderWithin(%d, %d) = %v, want %v", tc.folderID, tc.ancestor, got, tc.want)
			}
		})
	}
	if isFolderWithin(nil, 101, 100) {
		t.Error("nil listing must not place anything within")
	}
}

func TestValidateTargetFolder(t *testing.T) {
	const mail = 100
	tests := []struct {
		name   string
		target int
		want   error
	}{
		{"ok", 202, nil},
		{"root folder ok", 200, nil},
		{"mail folder", mail, errTargetIsMailFolder},
		{"direct child of mail folder", 101, errTargetInMailFolder},
		{"deep descendant of mail folder", 102, errTargetInMailFolder},
		{"deleted", 300, errTargetFolderDeleted},
		{"missing", 999, errTargetFolderMissing},
		{"zero", 0, errTargetFolderMissing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateTargetFolder(testFolders, tc.target, mail); !errors.Is(got, tc.want) {
				t.Errorf("validateTargetFolder(%d) = %v, want %v", tc.target, got, tc.want)
			}
		})
	}
}

func TestArchiveFolderPath(t *testing.T) {
	tests := map[int]string{
		202: "Facturen / 2026 / 10 Oktober",
		201: "Facturen / 2026",
		200: "Facturen",
		102: "Inbox / Sub / Deep",
		300: "Oud",
		500: "Wees", // missing parent ends the walk
		400: "Lus B / Lus A",
		999: "",
		0:   "",
	}
	for id, want := range tests {
		if got := archiveFolderPath(testFolders, id); got != want {
			t.Errorf("archiveFolderPath(%d) = %q, want %q", id, got, want)
		}
	}
	if got := archiveFolderPath(nil, 202); got != "" {
		t.Errorf("nil listing gave %q", got)
	}
}

func TestSubmitFullDocType(t *testing.T) {
	tests := map[string]string{
		"":             claude.DocumentTypeFactuur,
		"factuur":      claude.DocumentTypeFactuur,
		"creditnota":   claude.DocumentTypeCreditnota,
		" Creditnota ": claude.DocumentTypeCreditnota,
		"bonnetje":     claude.DocumentTypeFactuur, // SubmitFull books invoices, never receipts
		"overig":       claude.DocumentTypeFactuur,
	}
	for in, want := range tests {
		if got := submitFullDocType(in); got != want {
			t.Errorf("submitFullDocType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestArchiveSourceName(t *testing.T) {
	mailFile := func(name string) archiveSource {
		return archiveSource{
			fileID:    501,
			file:      eboekhouden.ArchiveFile{ID: 501, Naam: name, Datum: "2026-10-24T15:43:35.67"},
			mediaType: archiveMediaType(name),
		}
	}
	tests := []struct {
		name string
		src  archiveSource
		in   archiveNameInput
		want string
	}{
		{
			name: "SubmitFull invoice from mail folder pdf",
			src:  mailFile("Voorbeeld-Bijlage.pdf"),
			in:   archiveNameInput{"Anthropic, PBC", "INV-0042", "2026-10-12", submitFullDocType("")},
			want: "2026-10-12_Anthropic_INV-0042.pdf",
		},
		{
			name: "SubmitFull credit note without number",
			src:  mailFile("credit.PDF"),
			in:   archiveNameInput{"Anthropic", "", "2026-10-12", submitFullDocType("creditnota")},
			want: "2026-10-12_Anthropic_creditnota.pdf",
		},
		{
			name: "SubmitFull invoice without number",
			src:  mailFile("scan.pdf"),
			in:   archiveNameInput{"Anthropic", "", "2026-10-12", submitFullDocType("bonnetje")},
			want: "2026-10-12_Anthropic_document.pdf",
		},
		{
			name: "SubmitReceipt from mail folder jpg",
			src:  mailFile("bon.JPG"),
			in:   archiveNameInput{"Albert Heijn", "", "2026-10-03", claude.DocumentTypeBonnetje},
			want: "2026-10-03_Albert-Heijn_bonnetje.jpg",
		},
		{
			name: "mail folder file with unsupported extension keeps it",
			src:  mailFile("factuur.xml"),
			in:   archiveNameInput{"Acme", "7", "2026-10-03", submitFullDocType("")},
			want: "2026-10-03_Acme_7.xml",
		},
		{
			name: "mail folder file falls back to its received date",
			src:  mailFile("Voorbeeld-Bijlage.pdf"),
			in:   archiveNameInput{"Anthropic", "INV-0042", "", submitFullDocType("")},
			want: "2026-10-24_Anthropic_INV-0042.pdf",
		},
		{
			name: "upload png takes the sniffed type",
			src:  archiveSource{data: []byte("png bytes"), mediaType: "image/png"},
			in:   archiveNameInput{"Albert Heijn", "", "2026-10-03", claude.DocumentTypeBonnetje},
			want: "2026-10-03_Albert-Heijn_bonnetje.png",
		},
		{
			name: "upload has no received date to fall back to",
			src:  archiveSource{data: []byte("pdf"), mediaType: "application/pdf"},
			in:   archiveNameInput{"Acme", "1", "gisteren", submitFullDocType("")},
			want: "0000-00-00_Acme_1.pdf",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.src.archiveName(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// jpegBytes is a minimal body http.DetectContentType reads as image/jpeg.
var jpegBytes = append([]byte{0xff, 0xd8, 0xff, 0xe0}, make([]byte, 32)...)

func TestUploadArchiveName(t *testing.T) {
	h := &InvoiceHandler{}
	ctx := context.Background()

	t.Run("jpg body is archived as .jpg and dodges a conflict", func(t *testing.T) {
		src, err := h.prepareUploadSource(ctx, archiveSourceInput{pdfBase64: base64.StdEncoding.EncodeToString(jpegBytes)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if src.mediaType != "image/jpeg" || !bytes.Equal(src.data, jpegBytes) {
			t.Fatalf("source = %+v", src)
		}
		name := src.archiveName(archiveNameInput{"Anthropic", "INV-0042", "2026-10-12", claude.DocumentTypeFactuur})
		if name != "2026-10-12_Anthropic_INV-0042.jpg" {
			t.Errorf("name = %q", name)
		}
		existing := []eboekhouden.ArchiveFile{{Naam: "2026-10-12_anthropic_inv-0042.JPG"}, {Naam: "other.pdf"}}
		if got := uniqueName(name, fileNameSet(existing)); got != "2026-10-12_Anthropic_INV-0042_2.jpg" {
			t.Errorf("unique name = %q", got)
		}
	})
	t.Run("unsupported bytes are refused", func(t *testing.T) {
		_, err := h.prepareUploadSource(ctx, archiveSourceInput{pdfBase64: base64.StdEncoding.EncodeToString([]byte("hello world"))})
		if !errors.Is(err, claude.ErrUnsupportedMediaType) {
			t.Errorf("err = %v, want ErrUnsupportedMediaType", err)
		}
	})
	t.Run("undecodable base64 means no archive, not an error", func(t *testing.T) {
		src, err := h.prepareUploadSource(ctx, archiveSourceInput{pdfBase64: "%%%"})
		if err != nil || src.fromMailFolder() || len(src.data) != 0 {
			t.Errorf("got (%+v, %v)", src, err)
		}
	})
	t.Run("upload key without R2 means no archive", func(t *testing.T) {
		src, err := h.prepareUploadSource(ctx, archiveSourceInput{uploadKey: "uploads/x/document.pdf"})
		if err != nil || len(src.data) != 0 {
			t.Errorf("got (%+v, %v)", src, err)
		}
	})
}

func TestArchiveOutcomeAddTo(t *testing.T) {
	tests := []struct {
		name string
		out  archiveOutcome
		want gin.H
	}{
		{
			name: "nothing archived",
			out:  archiveOutcome{},
			want: gin.H{"archived": false, "linked": false, "renamed": false, "moved": false},
		},
		{
			name: "upload linked",
			out:  archiveOutcome{fileID: 700, linked: true, name: "2026-10-12_Anthropic_INV-0042.jpg", targetFolderID: 202},
			want: gin.H{"archived": true, "linked": true, "renamed": false, "moved": false,
				"archiveName": "2026-10-12_Anthropic_INV-0042.jpg", "targetFolderId": 202},
		},
		{
			name: "upload not linked",
			out:  archiveOutcome{fileID: 700, name: "x.pdf", targetFolderID: 202},
			want: gin.H{"archived": true, "linked": false, "renamed": false, "moved": false, "archiveName": "x.pdf", "targetFolderId": 202},
		},
		{
			name: "mail document fully archived",
			out: archiveOutcome{fileID: 501, linked: true, renamed: true, moved: true, name: "2026-10-12_Anthropic_INV-0042.pdf",
				targetFolderID: 202, targetFolderPath: "Facturen / 2026 / 10 Oktober"},
			want: gin.H{"archived": true, "linked": true, "renamed": true, "moved": true,
				"archiveName": "2026-10-12_Anthropic_INV-0042.pdf", "targetFolderId": 202, "targetFolderPath": "Facturen / 2026 / 10 Oktober"},
		},
		{
			name: "mail document already well named, only moved",
			out:  archiveOutcome{fileID: 501, linked: true, moved: true, name: "2026-10-12_Anthropic_INV-0042.pdf", targetFolderID: 202, targetFolderPath: "Facturen"},
			want: gin.H{"archived": true, "linked": true, "renamed": false, "moved": true,
				"archiveName": "2026-10-12_Anthropic_INV-0042.pdf", "targetFolderId": 202, "targetFolderPath": "Facturen"},
		},
		{
			name: "link failed",
			out:  archiveOutcome{fileID: 501, name: "Voorbeeld-Bijlage.pdf", warning: "De boeking is gelukt, maar het document is er niet aan gekoppeld. Het staat nog in de mailmap. Koppel het zelf in e-Boekhouden."},
			want: gin.H{"archived": true, "linked": false, "renamed": false, "moved": false, "archiveName": "Voorbeeld-Bijlage.pdf",
				"archiveWarning": "De boeking is gelukt, maar het document is er niet aan gekoppeld. Het staat nog in de mailmap. Koppel het zelf in e-Boekhouden."},
		},
		{
			name: "rename failed",
			out:  archiveOutcome{fileID: 501, linked: true, name: "Voorbeeld-Bijlage.pdf", warning: "Het document staat ongewijzigd in de mailmap: x"},
			want: gin.H{"archived": true, "linked": true, "renamed": false, "moved": false, "archiveName": "Voorbeeld-Bijlage.pdf", "archiveWarning": "Het document staat ongewijzigd in de mailmap: x"},
		},
		{
			name: "move failed after rename",
			out:  archiveOutcome{fileID: 501, linked: true, renamed: true, name: "2026-10-12_Anthropic_INV-0042.pdf", warning: "Het document is hernoemd, maar staat nog in de mailmap: x"},
			want: gin.H{"archived": true, "linked": true, "renamed": true, "moved": false, "archiveName": "2026-10-12_Anthropic_INV-0042.pdf",
				"archiveWarning": "Het document is hernoemd, maar staat nog in de mailmap: x"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := gin.H{"mutNr": 4711}
			tc.out.addTo(resp)
			tc.want["mutNr"] = 4711
			if !reflect.DeepEqual(resp, tc.want) {
				t.Errorf("response = %v, want %v", resp, tc.want)
			}
		})
	}
}

// --- Handler tests against a fake e-boekhouden -------------------------------

// fakeEBoekhouden is a stateful stand-in for the secure20 API covering the
// calls SubmitFull and SubmitReceipt make: folder header, directory list,
// file lists, upload, rename, bulk move, mutation, file link and ledger
// accounts. It records every call and can be told to fail the link, the
// rename or the move.
type fakeEBoekhouden struct {
	t       *testing.T
	mu      sync.Mutex
	calls   []fakeCall
	menh    int
	folders []archiveFolder
	files   map[int][]eboekhouden.ArchiveFile
	nextID  int
	mutNr   int

	linkStatus int    // 0 answers 200
	renameBody string // "" answers true
	moveBody   string // "" moves the files
	createBody string // "" creates the folder and answers it

	mutatieBody   string // "" books with mutNr
	mutatieHangup bool   // drops the connection instead of answering
}

type fakeCall struct {
	method, path, body string
}

var (
	filelistPathRe   = regexp.MustCompile(`^/v1/api/folder/(\d+)/filelist`)
	bulkUpdatePathRe = regexp.MustCompile(`^/v1/api/folder/bulk/update/(\d+)$`)
	deleteFilePathRe = regexp.MustCompile(`^/v1/api/folder/(\d+)$`)
)

// newFakeEBoekhouden sets up the testFolders tree with folder 100 ("Inbox")
// as the mail folder holding a PDF invoice (501) and a JPG receipt (502), and
// a file in Facturen/2026/10 Oktober (202) whose name collides with the
// invoice's archive name.
func newFakeEBoekhouden(t *testing.T) *fakeEBoekhouden {
	folders := make([]archiveFolder, len(testFolders))
	copy(folders, testFolders)
	return &fakeEBoekhouden{
		t:       t,
		menh:    100,
		folders: folders,
		files: map[int][]eboekhouden.ArchiveFile{
			100: {
				{ID: 501, ParentID: 100, Soort: "F", Naam: "Voorbeeld-Bijlage.pdf", Datum: "2026-10-24T15:43:35.67", Size: 228310},
				{ID: 502, ParentID: 100, Soort: "F", Naam: "bon.JPG", Datum: "2026-10-23T09:00:00", Size: 90000},
			},
			202: {
				{ID: 600, ParentID: 202, Soort: "F", Naam: "2026-10-12_Anthropic_INV-0042.pdf", Datum: "2026-10-01T10:00:00", Size: 1000, KoppelCount: 1},
			},
		},
		nextID: 700,
		mutNr:  4711,
	}
}

func (f *fakeEBoekhouden) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := readAll(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{r.Method, r.URL.Path, string(body)})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	write := func(v any) {
		b, _ := json.Marshal(v)
		w.Write(b)
	}
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/header"):
		write(map[string]any{"menhFolderId": f.menh, "credits": 1})
	case r.Method == http.MethodGet && path == "/v1/api/folder/directory":
		write(f.folders)
	case r.Method == http.MethodPost && path == "/v1/api/folder/directory":
		if f.createBody != "" {
			w.Write([]byte(f.createBody))
			return
		}
		var req struct {
			ParentFolderID int    `json:"parentFolderId"`
			Name           string `json:"name"`
		}
		json.Unmarshal(body, &req)
		f.nextID++
		folder := archiveFolder{ID: f.nextID, Naam: req.Name, ParentID: req.ParentFolderID}
		f.folders = append(f.folders, folder)
		write(folder)
	case r.Method == http.MethodGet && filelistPathRe.MatchString(path):
		id, _ := strconv.Atoi(filelistPathRe.FindStringSubmatch(path)[1])
		files := f.files[id]
		if files == nil {
			files = []eboekhouden.ArchiveFile{}
		}
		write(files)
	case r.Method == http.MethodPost && path == "/v1/api/folder/upload":
		var req struct {
			FileName string `json:"fileName"`
			FolderID int    `json:"folderId"`
			Data     string `json:"data"`
		}
		json.Unmarshal(body, &req)
		f.nextID++
		f.files[req.FolderID] = append(f.files[req.FolderID], eboekhouden.ArchiveFile{
			ID: f.nextID, ParentID: req.FolderID, Soort: "F", Naam: req.FileName, Datum: "2026-10-24T16:00:00", Size: int64(len(req.Data)),
		})
		write(req.FolderID)
	case r.Method == http.MethodPut && path == "/v1/api/folder/filename":
		if f.renameBody != "" {
			w.Write([]byte(f.renameBody))
			return
		}
		var req struct {
			FolderID       int    `json:"folderId"`
			ParentFolderID int    `json:"parentFolderId"`
			Name           string `json:"name"`
		}
		json.Unmarshal(body, &req)
		files := f.files[req.ParentFolderID]
		for i := range files {
			if files[i].ID == req.FolderID {
				files[i].Naam = req.Name
				write(true)
				return
			}
		}
		write(false) // wrong parent or unknown file: e-boekhouden says no
	case r.Method == http.MethodPost && bulkUpdatePathRe.MatchString(path):
		if f.moveBody != "" {
			w.Write([]byte(f.moveBody))
			return
		}
		target, _ := strconv.Atoi(bulkUpdatePathRe.FindStringSubmatch(path)[1])
		var ids []int
		json.Unmarshal(body, &ids)
		updated, notFound := []int{}, []int{}
		for _, id := range ids {
			moved := false
			for folderID, files := range f.files {
				for i, file := range files {
					if file.ID == id {
						f.files[folderID] = append(files[:i:i], files[i+1:]...)
						file.ParentID = target
						f.files[target] = append(f.files[target], file)
						moved = true
						break
					}
				}
				if moved {
					break
				}
			}
			if moved {
				updated = append(updated, id)
			} else {
				notFound = append(notFound, id)
			}
		}
		write(map[string]any{"updatedIds": updated, "notFoundIds": notFound, "errorMessages": []string{}, "successMessages": []string{}})
	case r.Method == http.MethodPost && path == "/v1/api/mutatie":
		if f.mutatieHangup {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		if f.mutatieBody != "" {
			w.Write([]byte(f.mutatieBody))
			return
		}
		write(map[string]any{"mutNr": f.mutNr, "mutId": 9})
	case r.Method == http.MethodDelete && deleteFilePathRe.MatchString(path):
		id, _ := strconv.Atoi(deleteFilePathRe.FindStringSubmatch(path)[1])
		for folderID, files := range f.files {
			for i, file := range files {
				if file.ID == id {
					f.files[folderID] = append(files[:i:i], files[i+1:]...)
					write(true)
					return
				}
			}
		}
		write(false)
	case r.Method == http.MethodPost && path == "/v1/api/folderkoppel/koppel/item":
		if f.linkStatus != 0 {
			w.WriteHeader(f.linkStatus)
			w.Write([]byte(`"link refused"`))
			return
		}
		write(map[string]any{})
	case r.Method == http.MethodGet && path == "/v1/api/grootboekrekening/selectlist/actieveGBR":
		write([]map[string]any{
			{"id": 1300, "code": "1300", "omschrijving": "Bank", "rekeningCategorie": "BANK"},
			{"id": 1600, "code": "1600", "omschrijving": "Crediteuren", "rekeningCategorie": "CRED"},
		})
	default:
		f.t.Errorf("fake e-boekhouden: unexpected %s %s", r.Method, path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func readAll(r *http.Request) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r.Body)
	return buf.Bytes(), err
}

// writes returns the recorded calls that change something in e-boekhouden,
// as "METHOD path" strings, in order.
func (f *fakeEBoekhouden) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if c.method != http.MethodGet {
			out = append(out, c.method+" "+c.path)
		}
	}
	return out
}

// call returns the first recorded call with the given method and path.
func (f *fakeEBoekhouden) call(method, path string) (fakeCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.method == method && c.path == path {
			return c, true
		}
	}
	return fakeCall{}, false
}

// submitTestEnv starts the fake, builds a client pointed at it and a handler
// without Redis, R2 or a database.
func submitTestEnv(t *testing.T) (*fakeEBoekhouden, *InvoiceHandler, *eboekhouden.Client) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	fake := newFakeEBoekhouden(t)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client, err := eboekhouden.NewClientForTesting(srv.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	return fake, &InvoiceHandler{}, client
}

// callSubmit runs a submit handler with body as the JSON request and a
// connected session, and decodes the JSON response.
func callSubmit(t *testing.T, handler gin.HandlerFunc, client *eboekhouden.Client, body map[string]any) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(session.ContextKey, &session.SessionData{UserID: "user-1"})
	c.Set(session.ClientContextKey, client)
	handler(c)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %s", w.Body.String())
	}
	return w.Code, resp
}

func fullRequest(extra map[string]any) map[string]any {
	req := map[string]any{
		"datum": "2026-10-12", "leverancier": "Anthropic, PBC", "factuurnummer": "INV-0042", "omschrijving": "Claude API",
		"bedragExcl": 100.0, "bedragIncl": 121.0, "btwBedrag": 21.0, "btwCode": "HOOG_INK_21", "inEx": "EX",
		"relatieId": 55, "tegenRekeningId": 4000, "rekeningId": 1600, "pdfHash": "abc",
	}
	for k, v := range extra {
		req[k] = v
	}
	return req
}

func receiptRequest(extra map[string]any) map[string]any {
	req := map[string]any{
		"datum": "2026-10-03", "leverancier": "Albert Heijn", "omschrijving": "Boodschappen",
		"bedragExcl": 11.47, "bedragIncl": 12.5, "btwBedrag": 1.03, "btwCode": "LAAG_INK_9",
		"tegenRekeningId": 4100, "bankAccountId": 1300,
	}
	for k, v := range extra {
		req[k] = v
	}
	return req
}

func TestSubmitFullMailDocument(t *testing.T) {
	fake, h, client := submitTestEnv(t)

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{"archiveFileId": 501, "targetFolderId": 202}))
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	want := map[string]any{
		"mutNr": 4711.0, "mutId": 9.0, "paymentMutNr": 0.0,
		"archived": true, "linked": true, "renamed": true, "moved": true,
		"archiveName":    "2026-10-12_Anthropic_INV-0042_2.pdf", // _2: the target already holds the plain name
		"targetFolderId": 202.0, "targetFolderPath": "Facturen / 2026 / 10 Oktober",
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %v\nwant       %v", resp, want)
	}

	// Booking first, link, then rename while still in the mail folder, then the move. No upload.
	wantWrites := []string{
		"POST /v1/api/mutatie",
		"POST /v1/api/folderkoppel/koppel/item",
		"PUT /v1/api/folder/filename",
		"POST /v1/api/folder/bulk/update/202",
	}
	if got := fake.writes(); !reflect.DeepEqual(got, wantWrites) {
		t.Errorf("writes = %v, want %v", got, wantWrites)
	}
	if c, _ := fake.call(http.MethodPut, "/v1/api/folder/filename"); c.body != `{"folderId":501,"parentFolderId":100,"name":"2026-10-12_Anthropic_INV-0042_2.pdf"}` {
		t.Errorf("rename payload = %s", c.body)
	}
	if c, _ := fake.call(http.MethodPost, "/v1/api/folder/bulk/update/202"); c.body != `[501]` {
		t.Errorf("move payload = %s", c.body)
	}
	if c, _ := fake.call(http.MethodPost, "/v1/api/folderkoppel/koppel/item"); c.body != `{"folders":[{"id":501,"soort":"F"}],"koppelId":4711,"koppelType":"MUT"}` {
		t.Errorf("link payload = %s", c.body)
	}
	// The file left the mail folder and kept its id.
	if len(fake.files[100]) != 1 || fake.files[100][0].ID != 502 {
		t.Errorf("mail folder now holds %v", fake.files[100])
	}
	if n := len(fake.files[202]); n != 2 || fake.files[202][1].ID != 501 || fake.files[202][1].Naam != "2026-10-12_Anthropic_INV-0042_2.pdf" {
		t.Errorf("target folder now holds %v", fake.files[202])
	}
}

func TestSubmitFullMailDocumentAutoFolder(t *testing.T) {
	fake, h, client := submitTestEnv(t)

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{"archiveFileId": 501}))
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	// 2026-10-12 resolves to the existing Facturen/2026/10 Oktober.
	if resp["moved"] != true || resp["targetFolderId"] != 202.0 || resp["targetFolderPath"] != "Facturen / 2026 / 10 Oktober" {
		t.Errorf("response = %v", resp)
	}
	if _, created := fake.call(http.MethodPost, "/v1/api/folder/directory"); created {
		t.Error("no folder should have been created")
	}
}

func TestSubmitFullMailDocumentAutoFolderInsideMailFolder(t *testing.T) {
	fake, h, client := submitTestEnv(t)
	// The user made "Facturen" the mail folder; Facturen/2026/10 Oktober lies inside it.
	fake.menh = 200
	fake.files[200] = fake.files[100]
	delete(fake.files, 100)

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{"archiveFileId": 501}))
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	if resp["linked"] != true || resp["renamed"] != false || resp["moved"] != false {
		t.Errorf("response = %v", resp)
	}
	if w, _ := resp["archiveWarning"].(string); !strings.HasPrefix(w, "Het document is geboekt en gekoppeld, maar niet verplaatst: de doelmap ligt in de mailmap") {
		t.Errorf("archiveWarning = %q", w)
	}
	for _, c := range fake.writes() {
		if strings.Contains(c, "filename") || strings.Contains(c, "bulk/update") {
			t.Errorf("unexpected write %s", c)
		}
	}
}

func TestSubmitFullMailDocumentRefusedBeforeBooking(t *testing.T) {
	tests := []struct {
		name    string
		extra   map[string]any
		wantErr string
	}{
		{"target is the mail folder", map[string]any{"archiveFileId": 501, "targetFolderId": 100}, "De mailmap kan geen doelmap zijn. Kies een andere map."},
		{"target is inside the mail folder", map[string]any{"archiveFileId": 501, "targetFolderId": 102}, "Een submap van de mailmap kan geen doelmap zijn. Kies een andere map."},
		{"target is deleted", map[string]any{"archiveFileId": 501, "targetFolderId": 300}, "De doelmap is verwijderd. Kies een andere map."},
		{"target does not exist", map[string]any{"archiveFileId": 501, "targetFolderId": 999}, "De doelmap bestaat niet. Kies een andere map."},
		{"file not in the mail folder", map[string]any{"archiveFileId": 600, "targetFolderId": 202}, "Dit document staat niet in de mailmap. Misschien is het verplaatst of verwijderd."},
		{"file unknown", map[string]any{"archiveFileId": 999}, "Dit document staat niet in de mailmap. Misschien is het verplaatst of verwijderd."},
		{"two sources", map[string]any{"archiveFileId": 501, "pdfBase64": "AAAA"}, "Kies één bron voor het document."},
		{"negative id", map[string]any{"archiveFileId": -3}, "Ongeldig document-id."},
		{"creditnota from the mail folder", map[string]any{"archiveFileId": 501, "documentType": "creditnota"}, errCreditnotaNotSupported},
		{"creditnota upload", map[string]any{"pdfBase64": "JVBERi0=", "documentType": " Creditnota "}, errCreditnotaNotSupported},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake, h, client := submitTestEnv(t)
			status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(tc.extra))
			if status != http.StatusBadRequest {
				t.Fatalf("status %d: %v", status, resp)
			}
			if resp["error"] != tc.wantErr {
				t.Errorf("error = %v, want %q", resp["error"], tc.wantErr)
			}
			if writes := fake.writes(); len(writes) != 0 {
				t.Errorf("nothing may be booked, got %v", writes)
			}
		})
	}
}

func TestSubmitFullMailDocumentNoMailFolder(t *testing.T) {
	fake, h, client := submitTestEnv(t)
	fake.menh = 0

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{"archiveFileId": 501}))
	if status != http.StatusBadRequest || resp["error"] != "Dit document staat niet in de mailmap. Misschien is het verplaatst of verwijderd." {
		t.Errorf("status %d, response %v", status, resp)
	}
	if writes := fake.writes(); len(writes) != 0 {
		t.Errorf("nothing may be booked, got %v", writes)
	}
}

func TestSubmitFullMailDocumentLinkFails(t *testing.T) {
	fake, h, client := submitTestEnv(t)
	fake.linkStatus = http.StatusInternalServerError

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{"archiveFileId": 501, "targetFolderId": 202}))
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	if resp["mutNr"] != 4711.0 || resp["archived"] != true || resp["linked"] != false || resp["renamed"] != false || resp["moved"] != false {
		t.Errorf("response = %v", resp)
	}
	if resp["archiveName"] != "Voorbeeld-Bijlage.pdf" {
		t.Errorf("archiveName = %v, want the unchanged name", resp["archiveName"])
	}
	if w, _ := resp["archiveWarning"].(string); !strings.Contains(w, "het document is er niet aan gekoppeld") {
		t.Errorf("archiveWarning = %q", w)
	}
	wantWrites := []string{"POST /v1/api/mutatie", "POST /v1/api/folderkoppel/koppel/item"}
	if got := fake.writes(); !reflect.DeepEqual(got, wantWrites) {
		t.Errorf("writes = %v, want %v (no rename, no move)", got, wantWrites)
	}
	if len(fake.files[100]) != 2 || fake.files[100][0].Naam != "Voorbeeld-Bijlage.pdf" {
		t.Errorf("mail folder = %v, want the file untouched", fake.files[100])
	}
}

func TestSubmitFullMailDocumentRenameFails(t *testing.T) {
	fake, h, client := submitTestEnv(t)
	fake.renameBody = "false"

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{"archiveFileId": 501, "targetFolderId": 202}))
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	if resp["linked"] != true || resp["renamed"] != false || resp["moved"] != false || resp["archiveName"] != "Voorbeeld-Bijlage.pdf" {
		t.Errorf("response = %v", resp)
	}
	if w, _ := resp["archiveWarning"].(string); !strings.HasPrefix(w, "Het document staat ongewijzigd in de mailmap: ") {
		t.Errorf("archiveWarning = %q", w)
	}
	if _, moved := fake.call(http.MethodPost, "/v1/api/folder/bulk/update/202"); moved {
		t.Error("a file that could not be renamed must not be moved")
	}
}

func TestSubmitFullMailDocumentMoveFails(t *testing.T) {
	fake, h, client := submitTestEnv(t)
	fake.moveBody = `{"updatedIds":[],"notFoundIds":[501],"errorMessages":[],"successMessages":[]}`

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{"archiveFileId": 501, "targetFolderId": 202}))
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	if resp["linked"] != true || resp["renamed"] != true || resp["moved"] != false || resp["archiveName"] != "2026-10-12_Anthropic_INV-0042_2.pdf" {
		t.Errorf("response = %v", resp)
	}
	if _, ok := resp["targetFolderId"]; ok {
		t.Error("targetFolderId must be absent when the file was not moved")
	}
	if w, _ := resp["archiveWarning"].(string); !strings.HasPrefix(w, "Het document is hernoemd, maar staat nog in de mailmap: ") {
		t.Errorf("archiveWarning = %q", w)
	}
	// The file sits in the mail folder under its new name.
	if fake.files[100][0].Naam != "2026-10-12_Anthropic_INV-0042_2.pdf" {
		t.Errorf("mail folder = %v", fake.files[100])
	}
}

func TestSubmitFullUploadUsesNamingConvention(t *testing.T) {
	fake, h, client := submitTestEnv(t)

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{
		"pdfBase64": base64.StdEncoding.EncodeToString(jpegBytes),
		"filename":  "IMG_4711 (scan).pdf", // ignored: the convention wins
		"folderId":  202,
	}))
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	want := map[string]any{
		"mutNr": 4711.0, "mutId": 9.0, "paymentMutNr": 0.0,
		"archived": true, "linked": true, "renamed": false, "moved": false,
		"archiveName": "2026-10-12_Anthropic_INV-0042.jpg", "targetFolderId": 202.0,
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %v\nwant       %v", resp, want)
	}
	wantWrites := []string{"POST /v1/api/folder/upload", "POST /v1/api/mutatie", "POST /v1/api/folderkoppel/koppel/item"}
	if got := fake.writes(); !reflect.DeepEqual(got, wantWrites) {
		t.Errorf("writes = %v, want %v", got, wantWrites)
	}
	upload, _ := fake.call(http.MethodPost, "/v1/api/folder/upload")
	var payload struct {
		FileName  string `json:"fileName"`
		FolderID  int    `json:"folderId"`
		Overwrite bool   `json:"overwrite"`
	}
	json.Unmarshal([]byte(upload.body), &payload)
	if payload.FileName != "2026-10-12_Anthropic_INV-0042.jpg" || payload.FolderID != 202 || payload.Overwrite {
		t.Errorf("upload payload = %+v", payload)
	}
	// The link used the id e-boekhouden assigned to the new file.
	link, _ := fake.call(http.MethodPost, "/v1/api/folderkoppel/koppel/item")
	if !strings.Contains(link.body, `{"id":701,"soort":"F"}`) {
		t.Errorf("link payload = %s", link.body)
	}
}

func TestSubmitFullUploadConflictGetsSuffix(t *testing.T) {
	fake, h, client := submitTestEnv(t)
	pdf := []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n1 0 obj")

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{
		"pdfBase64": base64.StdEncoding.EncodeToString(pdf),
		"folderId":  202,
	}))
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	if resp["archiveName"] != "2026-10-12_Anthropic_INV-0042_2.pdf" || resp["linked"] != true {
		t.Errorf("response = %v", resp)
	}
	upload, _ := fake.call(http.MethodPost, "/v1/api/folder/upload")
	if !strings.Contains(upload.body, `"fileName":"2026-10-12_Anthropic_INV-0042_2.pdf"`) {
		t.Errorf("upload payload = %s", upload.body)
	}
}

func TestSubmitFullUploadUnsupportedType(t *testing.T) {
	fake, h, client := submitTestEnv(t)

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{
		"pdfBase64": base64.StdEncoding.EncodeToString([]byte("<!DOCTYPE html><html></html>")),
		"folderId":  202,
	}))
	if status != http.StatusUnsupportedMediaType || resp["error"] != "unsupported_media_type" {
		t.Errorf("status %d, response %v", status, resp)
	}
	if writes := fake.writes(); len(writes) != 0 {
		t.Errorf("nothing may be booked, got %v", writes)
	}
}

func TestSubmitFullWithoutDocument(t *testing.T) {
	fake, h, client := submitTestEnv(t)

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(nil))
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	want := map[string]any{"mutNr": 4711.0, "mutId": 9.0, "paymentMutNr": 0.0, "archived": false, "linked": false, "renamed": false, "moved": false}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %v\nwant       %v", resp, want)
	}
	if got := fake.writes(); !reflect.DeepEqual(got, []string{"POST /v1/api/mutatie"}) {
		t.Errorf("writes = %v", got)
	}
}

func TestSubmitReceiptMailDocument(t *testing.T) {
	fake, h, client := submitTestEnv(t)

	status, resp := callSubmit(t, h.SubmitReceipt, client, receiptRequest(map[string]any{"archiveFileId": 502, "targetFolderId": 202}))
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	want := map[string]any{
		"mutNr": 4711.0, "mutId": 9.0,
		"archived": true, "linked": true, "renamed": true, "moved": true,
		"archiveName": "2026-10-03_Albert-Heijn_bonnetje.jpg", "targetFolderId": 202.0, "targetFolderPath": "Facturen / 2026 / 10 Oktober",
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %v\nwant       %v", resp, want)
	}
	wantWrites := []string{
		"POST /v1/api/mutatie",
		"POST /v1/api/folderkoppel/koppel/item",
		"PUT /v1/api/folder/filename",
		"POST /v1/api/folder/bulk/update/202",
	}
	if got := fake.writes(); !reflect.DeepEqual(got, wantWrites) {
		t.Errorf("writes = %v, want %v", got, wantWrites)
	}
	if c, _ := fake.call(http.MethodPut, "/v1/api/folder/filename"); c.body != `{"folderId":502,"parentFolderId":100,"name":"2026-10-03_Albert-Heijn_bonnetje.jpg"}` {
		t.Errorf("rename payload = %s", c.body)
	}
}

func TestSubmitReceiptRefusesTwoSources(t *testing.T) {
	fake, h, client := submitTestEnv(t)

	status, resp := callSubmit(t, h.SubmitReceipt, client, receiptRequest(map[string]any{"archiveFileId": 502, "uploadKey": "uploads/x/document.jpg"}))
	if status != http.StatusBadRequest || resp["error"] != "Kies één bron voor het document." {
		t.Errorf("status %d, response %v", status, resp)
	}
	if writes := fake.writes(); len(writes) != 0 {
		t.Errorf("nothing may be booked, got %v", writes)
	}
}

// --- POST /documents/archive: target folder rules -----------------------------

func TestArchiveDocumentsExplicitTargetFolder(t *testing.T) {
	tests := []struct {
		name    string
		target  int
		wantErr string // "" means the move is accepted
	}{
		{"ok", 202, ""},
		{"root folder ok", 200, ""},
		{"mail folder", 100, "De mailmap kan geen doelmap zijn. Kies een andere map."},
		{"direct child of mail folder", 101, "Een submap van de mailmap kan geen doelmap zijn. Kies een andere map."},
		{"deep descendant of mail folder", 102, "Een submap van de mailmap kan geen doelmap zijn. Kies een andere map."},
		{"deleted", 300, "De doelmap is verwijderd. Kies een andere map."},
		{"missing", 999, "De doelmap bestaat niet. Kies een andere map."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake, h, client := submitTestEnv(t)
			status, resp := callSubmit(t, h.ArchiveDocuments, client, map[string]any{"fileIds": []int{501}, "targetFolderId": tc.target})
			if tc.wantErr != "" {
				if status != http.StatusBadRequest || resp["error"] != tc.wantErr {
					t.Fatalf("status %d, response %v, want 400 %q", status, resp, tc.wantErr)
				}
				if writes := fake.writes(); len(writes) != 0 {
					t.Errorf("nothing may change, got %v", writes)
				}
				return
			}
			if status != http.StatusOK {
				t.Fatalf("status %d: %v", status, resp)
			}
			moved, _ := resp["moved"].([]any)
			failed, _ := resp["failed"].([]any)
			if len(moved) != 1 || len(failed) != 0 {
				t.Fatalf("moved %v, failed %v", moved, failed)
			}
			if got := moved[0].(map[string]any)["targetFolderId"]; got != float64(tc.target) {
				t.Errorf("targetFolderId = %v, want %d", got, tc.target)
			}
			if _, ok := fake.call(http.MethodPost, "/v1/api/folder/bulk/update/"+strconv.Itoa(tc.target)); !ok {
				t.Errorf("no move to %d, writes %v", tc.target, fake.writes())
			}
		})
	}
}

func TestArchiveDocumentsAutoFolder(t *testing.T) {
	tests := []struct {
		name       string
		mailFolder int
		wantMoved  int
		wantFailed string
	}{
		// The file was received on 2026-10-24 and has no analysis, so it goes to Facturen/2026/10 Oktober (202).
		{"month folder outside the mail folder", 100, 202, ""},
		// With "Facturen" as the mail folder that month folder lies inside it.
		{"month folder inside the mail folder", 200, 0, "Een submap van de mailmap kan geen doelmap zijn. Kies een andere map."},
		// With "10 Oktober" itself as the mail folder the auto target is the mail folder.
		{"month folder is the mail folder", 202, 0, "De mailmap kan geen doelmap zijn. Kies een andere map."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake, h, client := submitTestEnv(t)
			fake.menh = tc.mailFolder
			fake.files[tc.mailFolder] = append(fake.files[tc.mailFolder], fake.files[100]...)
			if tc.mailFolder != 100 {
				delete(fake.files, 100)
			}

			status, resp := callSubmit(t, h.ArchiveDocuments, client, map[string]any{"fileIds": []int{501}})
			if status != http.StatusOK {
				t.Fatalf("status %d: %v", status, resp)
			}
			moved, _ := resp["moved"].([]any)
			failed, _ := resp["failed"].([]any)
			if tc.wantFailed != "" {
				if len(moved) != 0 || len(failed) != 1 {
					t.Fatalf("moved %v, failed %v", moved, failed)
				}
				if got := failed[0].(map[string]any)["error"]; got != tc.wantFailed {
					t.Errorf("error = %v, want %q", got, tc.wantFailed)
				}
				for _, w := range fake.writes() {
					if strings.Contains(w, "filename") || strings.Contains(w, "bulk/update") {
						t.Errorf("unexpected write %s", w)
					}
				}
				return
			}
			if len(moved) != 1 || len(failed) != 0 {
				t.Fatalf("moved %v, failed %v", moved, failed)
			}
			if got := moved[0].(map[string]any)["targetFolderId"]; got != float64(tc.wantMoved) {
				t.Errorf("targetFolderId = %v, want %d", got, tc.wantMoved)
			}
		})
	}
}
