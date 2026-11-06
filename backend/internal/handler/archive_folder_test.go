package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
)

// createdFolders lists the folder creations the fake saw, as "parent/name".
func (f *fakeEBoekhouden) createdFolders(t *testing.T) []string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if c.method != http.MethodPost || c.path != "/v1/api/folder/directory" {
			continue
		}
		var req struct {
			ParentFolderID int    `json:"parentFolderId"`
			Name           string `json:"name"`
		}
		if err := json.Unmarshal([]byte(c.body), &req); err != nil {
			t.Fatalf("create payload %s: %v", c.body, err)
		}
		out = append(out, fmt.Sprintf("%d/%s", req.ParentFolderID, req.Name))
	}
	return out
}

// rootYearFolders is an archive whose year folders sit directly at the root,
// without Facturen:
//
//	Inbox (100, the mail folder)
//	2025 (800)
//	  01 Januari (810)
//	2026 (801)
//	  10 Oktober (802)
var rootYearFolders = []archiveFolder{
	{ID: 100, Naam: "Inbox", ParentID: 0},
	{ID: 800, Naam: "2025", ParentID: 0},
	{ID: 810, Naam: "01 Januari", ParentID: 800},
	{ID: 801, Naam: "2026", ParentID: 0},
	{ID: 802, Naam: "10 Oktober", ParentID: 801},
}

// withoutFolder returns folders without the folder id.
func withoutFolder(folders []archiveFolder, id int) []archiveFolder {
	var out []archiveFolder
	for _, f := range folders {
		if f.ID != id {
			out = append(out, f)
		}
	}
	return out
}

func TestResolveArchiveBase(t *testing.T) {
	tests := []struct {
		name       string
		folders    []archiveFolder
		wantBase   int
		wantCreate bool
	}{
		{"empty archive", nil, 0, true},
		{"Facturen at the root", testFolders, 200, false},
		{"legacy Basismap/Facturen", []archiveFolder{{ID: 50, Naam: "Basismap"}, {ID: 51, Naam: "Facturen", ParentID: 50}}, 51, false},
		{"year folders at the root", rootYearFolders, 0, false},
		{"only a mail folder", []archiveFolder{{ID: 100, Naam: "Inbox"}}, 0, true},
		{"a year below the root does not count", []archiveFolder{{ID: 1, Naam: "Oud"}, {ID: 2, Naam: "2024", ParentID: 1}}, 0, true},
		{"a folder that only starts with a year does not count", []archiveFolder{{ID: 1, Naam: "2024 oud"}}, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base, create := resolveArchiveBase(tc.folders)
			if base != tc.wantBase || create != tc.wantCreate {
				t.Errorf("got (%d, %v), want (%d, %v)", base, create, tc.wantBase, tc.wantCreate)
			}
		})
	}
}

func TestFindOrCreateArchiveFolder(t *testing.T) {
	date := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	// The fake hands out ids from 701 on.
	tests := []struct {
		name        string
		folders     []archiveFolder
		want        int
		wantCreates []string
	}{
		{"existing structure", testFolders, 202, nil},
		{
			"deleted Facturen is not reused",
			[]archiveFolder{
				{ID: 200, Naam: "Facturen", ParentID: 0, IsDeleted: true},
				{ID: 201, Naam: "2026", ParentID: 200},
				{ID: 202, Naam: "10 Oktober", ParentID: 201},
			},
			703, []string{"0/Facturen", "701/2026", "702/10 Oktober"},
		},
		{
			"deleted year folder is not reused",
			[]archiveFolder{
				{ID: 200, Naam: "Facturen", ParentID: 0},
				{ID: 201, Naam: "2026", ParentID: 200, IsDeleted: true},
				{ID: 202, Naam: "10 Oktober", ParentID: 201},
			},
			702, []string{"200/2026", "701/10 Oktober"},
		},
		{
			"deleted month folder is not reused",
			[]archiveFolder{
				{ID: 200, Naam: "Facturen", ParentID: 0},
				{ID: 201, Naam: "2026", ParentID: 200},
				{ID: 202, Naam: "10 Oktober", ParentID: 201, IsDeleted: true},
			},
			701, []string{"201/10 Oktober"},
		},
		{
			"a live folder next to a deleted one of the same name",
			[]archiveFolder{
				{ID: 199, Naam: "Facturen", ParentID: 0, IsDeleted: true},
				{ID: 200, Naam: "Facturen", ParentID: 0},
				{ID: 201, Naam: "2026", ParentID: 200},
				{ID: 202, Naam: "10 Oktober", ParentID: 201},
			},
			202, nil,
		},
		{
			"legacy Facturen under Basismap",
			[]archiveFolder{
				{ID: 50, Naam: "Basismap", ParentID: 0},
				{ID: 51, Naam: "Facturen", ParentID: 50},
				{ID: 52, Naam: "2026", ParentID: 51},
				{ID: 53, Naam: "10 Oktober", ParentID: 52},
			},
			53, nil,
		},
		{"year folders at the root", rootYearFolders, 802, nil},
		{
			"root years, month missing",
			withoutFolder(rootYearFolders, 802),
			701, []string{"801/10 Oktober"},
		},
		{
			"root years, year missing",
			withoutFolder(withoutFolder(rootYearFolders, 802), 801),
			702, []string{"0/2026", "701/10 Oktober"},
		},
		{
			"Facturen wins over year folders at the root",
			append(append([]archiveFolder(nil), testFolders...), archiveFolder{ID: 801, Naam: "2026", ParentID: 0}),
			202, nil,
		},
		{
			"a deleted Facturen next to year folders at the root",
			append([]archiveFolder{{ID: 200, Naam: "Facturen", ParentID: 0, IsDeleted: true}}, rootYearFolders...),
			802, nil,
		},
		{
			"a deleted year at the root does not count",
			[]archiveFolder{{ID: 800, Naam: "2025", ParentID: 0, IsDeleted: true}},
			703, []string{"0/Facturen", "701/2026", "702/10 Oktober"},
		},
		{
			"names differ in case and whitespace",
			[]archiveFolder{
				{ID: 200, Naam: " facturen ", ParentID: 0},
				{ID: 201, Naam: "2026 ", ParentID: 200},
				{ID: 202, Naam: "10 OKTOBER", ParentID: 201},
			},
			202, nil,
		},
		{
			"root year names differ in whitespace",
			[]archiveFolder{
				{ID: 801, Naam: " 2026", ParentID: 0},
				{ID: 802, Naam: "10 oktober ", ParentID: 801},
			},
			802, nil,
		},
		{
			"deleted legacy Facturen is not reused",
			[]archiveFolder{
				{ID: 50, Naam: "Basismap", ParentID: 0},
				{ID: 51, Naam: "Facturen", ParentID: 50, IsDeleted: true},
			},
			703, []string{"0/Facturen", "701/2026", "702/10 Oktober"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake, h, client := submitTestEnv(t)
			fake.folders = append([]archiveFolder(nil), tc.folders...)

			got, err := h.findOrCreateArchiveFolder(client, date)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("folder = %d, want %d", got, tc.want)
			}
			if creates := fake.createdFolders(t); !reflect.DeepEqual(creates, tc.wantCreates) {
				t.Errorf("created %v, want %v", creates, tc.wantCreates)
			}
		})
	}
}

func TestFindOrCreateArchiveFolderRefusesACreateWithoutID(t *testing.T) {
	date := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	for _, body := range []string{`{}`, `{"id":0,"naam":"Facturen"}`, `true`} {
		t.Run(body, func(t *testing.T) {
			fake, h, client := submitTestEnv(t)
			fake.folders = nil
			fake.createBody = body

			got, err := h.findOrCreateArchiveFolder(client, date)
			if !errors.Is(err, errFolderCreateNoID) || got != 0 {
				t.Fatalf("got (%d, %v), want errFolderCreateNoID", got, err)
			}
			// Without an id the next level must not be created at the root.
			if creates := fake.createdFolders(t); !reflect.DeepEqual(creates, []string{"0/Facturen"}) {
				t.Errorf("created %v, want only the first attempt", creates)
			}
		})
	}
}

// A refused folder creation says which level failed and still unwraps to
// e-boekhouden's error.
func TestFindOrCreateArchiveFolderNamesTheRefusedLevel(t *testing.T) {
	date := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		folders   []archiveFolder
		wantLevel string
	}{
		{"fresh archive", nil, folderLevelFacturen},
		{"root years without the year", withoutFolder(withoutFolder(rootYearFolders, 802), 801), folderLevelJaar},
		{"root year without the month", withoutFolder(rootYearFolders, 802), folderLevelMaand},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake, h, client := submitTestEnv(t)
			fake.folders = append([]archiveFolder(nil), tc.folders...)
			fake.createStatus = http.StatusBadRequest

			got, err := h.findOrCreateArchiveFolder(client, date)
			if got != 0 || err == nil {
				t.Fatalf("got (%d, %v), want an error", got, err)
			}
			if level := archiveFolderLevel(err); level != tc.wantLevel {
				t.Errorf("level = %q, want %q", level, tc.wantLevel)
			}
			var api *eboekhouden.APIError
			if !errors.As(err, &api) {
				t.Errorf("err = %v, want it to wrap the APIError", err)
			}
		})
	}

	t.Run("expired session survives the wrapping", func(t *testing.T) {
		fake, h, client := submitTestEnv(t)
		fake.folders = nil
		fake.expireAt = "POST /v1/api/folder/directory"
		_, err := h.findOrCreateArchiveFolder(client, date)
		if !errors.Is(err, eboekhouden.ErrSessionExpired) || archiveFolderLevel(err) != folderLevelFacturen {
			t.Errorf("err = %v, want ErrSessionExpired at level facturen", err)
		}
	})

	t.Run("a failed listing", func(t *testing.T) {
		fake, h, client := submitTestEnv(t)
		fake.directoryStatus = http.StatusInternalServerError
		_, err := h.findOrCreateArchiveFolder(client, date)
		if err == nil || archiveFolderLevel(err) != "listing" {
			t.Errorf("err = %v, want a listing error", err)
		}
		if creates := fake.createdFolders(t); len(creates) != 0 {
			t.Errorf("created %v after a failed listing", creates)
		}
	})
}
