package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"
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
