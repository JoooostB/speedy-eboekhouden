package eboekhouden

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestGetFolderHeader(t *testing.T) {
	var last recordedRequest
	srv := jsonServer(t, 200, `{"menhFolderId":90460978,"credits":113,"dataLimit":5368709120,"dataUsed":12345678,`+
		`"directoryInfo":{"directoryName":"Inbox","deleted":false,"fileCount":3,"subDirectoryCount":1,"totalSize":456789}}`, &last)
	c := newTestClient(t, srv)

	got, err := c.GetFolderHeader(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRequest(t, last, "GET", "/v1/api/folder/0/header", "")

	want := &FolderHeader{
		MenhFolderID: 90460978,
		Credits:      113,
		DataLimit:    5368709120,
		DataUsed:     12345678,
		DirectoryInfo: FolderDirectoryInfo{
			DirectoryName:     "Inbox",
			FileCount:         3,
			SubDirectoryCount: 1,
			TotalSize:         456789,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("header = %+v, want %+v", got, want)
	}
}

func TestGetFolderHeaderSessionExpired(t *testing.T) {
	var last recordedRequest
	srv := jsonServer(t, 401, ``, &last)
	c := newTestClient(t, srv)

	if _, err := c.GetFolderHeader(90460978); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	assertRequest(t, last, "GET", "/v1/api/folder/90460978/header", "")
}

func TestSetMailFolder(t *testing.T) {
	t.Run("sends bare number", func(t *testing.T) {
		var last recordedRequest
		srv := jsonServer(t, 200, `true`, &last)
		c := newTestClient(t, srv)

		if err := c.SetMailFolder(90460978); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertRequest(t, last, "POST", "/v1/api/administratie/menhfolder", "90460978")
	})

	t.Run("false is an error", func(t *testing.T) {
		var last recordedRequest
		srv := jsonServer(t, 200, `false`, &last)
		c := newTestClient(t, srv)

		if err := c.SetMailFolder(1); err == nil {
			t.Fatal("expected an error for a false response")
		}
	})
}

func TestListArchiveFilesFiltersFoldersAndDeleted(t *testing.T) {
	var last recordedRequest
	srv := jsonServer(t, 200, `[
		{"koppelCount":0,"id":101,"parentId":9,"soort":"F","naam":"factuur-a.pdf","datum":"2026-10-24T15:43:35.67","size":228310,"isDeleted":false},
		{"koppelCount":0,"id":102,"parentId":9,"soort":"D","naam":"Submap","datum":"2026-10-20T10:00:00","isDeleted":false},
		{"koppelCount":1,"id":103,"parentId":9,"soort":"F","naam":"verwijderd.pdf","datum":"2026-09-01T09:00:00","size":100,"isDeleted":true},
		{"koppelCount":2,"id":104,"parentId":9,"soort":"F","naam":"bon.jpg","datum":"2026-10-23T08:00:00","size":51200,"isDeleted":false}
	]`, &last)
	c := newTestClient(t, srv)

	files, err := c.ListArchiveFiles(9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRequest(t, last, "GET", "/v1/api/folder/9/filelist?includeDelete=false", "")

	want := []ArchiveFile{
		{KoppelCount: 0, ID: 101, ParentID: 9, Soort: "F", Naam: "factuur-a.pdf", Datum: "2026-10-24T15:43:35.67", Size: 228310},
		{KoppelCount: 2, ID: 104, ParentID: 9, Soort: "F", Naam: "bon.jpg", Datum: "2026-10-23T08:00:00", Size: 51200},
	}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("files = %+v, want %+v", files, want)
	}
}

func TestListArchiveFilesEmptyIsNotNil(t *testing.T) {
	var last recordedRequest
	srv := jsonServer(t, 200, `[]`, &last)
	c := newTestClient(t, srv)

	files, err := c.ListArchiveFiles(9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if files == nil || len(files) != 0 {
		t.Errorf("files = %#v, want empty non-nil slice", files)
	}
}

func TestDownloadArchiveFile(t *testing.T) {
	pdf := []byte("%PDF-1.4 fake")
	var last recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = record(t, r)
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "attachment; filename=Kachel.pdf; filename*=UTF-8''Kachel.pdf")
		w.Write(pdf)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)

	data, contentType, filename, err := c.DownloadArchiveFile(90461397, 10<<20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRequest(t, last, "GET", "/v1/api/folder/90461397/download", "")
	if string(data) != string(pdf) {
		t.Errorf("data = %q, want %q", data, pdf)
	}
	if contentType != "application/pdf" {
		t.Errorf("contentType = %q, want application/pdf", contentType)
	}
	if filename != "Kachel.pdf" {
		t.Errorf("filename = %q, want Kachel.pdf", filename)
	}
}

func TestRenameArchiveFile(t *testing.T) {
	t.Run("payload", func(t *testing.T) {
		var last recordedRequest
		srv := jsonServer(t, 200, `true`, &last)
		c := newTestClient(t, srv)

		err := c.RenameArchiveFile(90461397, 90460978, "2026-10-12_Voorbeeld-Leverancier_INV-0042.pdf")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertRequest(t, last, "PUT", "/v1/api/folder/filename",
			`{"folderId":90461397,"parentFolderId":90460978,"name":"2026-10-12_Voorbeeld-Leverancier_INV-0042.pdf"}`)
	})

	t.Run("false is an error", func(t *testing.T) {
		var last recordedRequest
		srv := jsonServer(t, 200, `false`, &last)
		c := newTestClient(t, srv)

		if err := c.RenameArchiveFile(1, 2, "x.pdf"); err == nil {
			t.Fatal("expected an error for a false response")
		}
	})

	t.Run("envelope error", func(t *testing.T) {
		var last recordedRequest
		srv := jsonServer(t, 200, `{"errorType":"validation","message":"Bestandsnaam bestaat al","code":"X"}`, &last)
		c := newTestClient(t, srv)

		err := c.RenameArchiveFile(1, 2, "x.pdf")
		if err == nil || !strings.Contains(err.Error(), "Bestandsnaam bestaat al") {
			t.Fatalf("err = %v, want envelope message", err)
		}
	})
}

func TestMoveArchiveFiles(t *testing.T) {
	longMessage := strings.Repeat("m", 300)

	tests := []struct {
		name      string
		ids       []int
		resp      string
		wantMoved []int
		wantErr   string // substring; "" means success
	}{
		{
			name:      "all moved",
			ids:       []int{42, 43},
			resp:      `{"updatedIds":[42,43],"notFoundIds":[],"errorMessages":[],"successMessages":[]}`,
			wantMoved: []int{42, 43},
		},
		{
			name:      "not found id",
			ids:       []int{42, 43},
			resp:      `{"updatedIds":[42],"notFoundIds":[43],"errorMessages":[],"successMessages":[]}`,
			wantMoved: []int{42},
			wantErr:   "kent bestand-id [43] niet",
		},
		{
			name:      "error message",
			ids:       []int{42},
			resp:      `{"updatedIds":[],"notFoundIds":[],"errorMessages":["Doelmap bestaat niet"],"successMessages":[]}`,
			wantMoved: []int{},
			wantErr:   "e-Boekhouden: Doelmap bestaat niet",
		},
		{
			name:      "non-string error message does not break parsing",
			ids:       []int{42},
			resp:      `{"updatedIds":[],"notFoundIds":[],"errorMessages":[{"code":"X"}],"successMessages":[]}`,
			wantMoved: []int{},
			wantErr:   `e-Boekhouden: {"code":"X"}`,
		},
		{
			name:      "silently skipped id",
			ids:       []int{42, 43},
			resp:      `{"updatedIds":[42],"notFoundIds":[],"errorMessages":[],"successMessages":[]}`,
			wantMoved: []int{42},
			wantErr:   "bestand 43 niet verplaatst",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var last recordedRequest
			srv := jsonServer(t, 200, tc.resp, &last)
			c := newTestClient(t, srv)

			moved, err := c.MoveArchiveFiles(77, tc.ids)
			wantBody, _ := json.Marshal(tc.ids)
			assertRequest(t, last, "POST", "/v1/api/folder/bulk/update/77", string(wantBody))

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
			if !reflect.DeepEqual(moved, tc.wantMoved) {
				t.Errorf("moved = %#v, want %#v", moved, tc.wantMoved)
			}
		})
	}

	t.Run("error message is capped at 200 characters", func(t *testing.T) {
		var last recordedRequest
		srv := jsonServer(t, 200, `{"updatedIds":[],"notFoundIds":[],"errorMessages":["`+longMessage+`"],"successMessages":[]}`, &last)
		c := newTestClient(t, srv)

		_, err := c.MoveArchiveFiles(77, []int{42})
		if err == nil {
			t.Fatal("expected an error")
		}
		if strings.Contains(err.Error(), longMessage) {
			t.Errorf("error contains the uncapped upstream message")
		}
		if want := len("e-Boekhouden: ") + 200; len(err.Error()) != want {
			t.Errorf("len(err) = %d, want %d", len(err.Error()), want)
		}
	})

	t.Run("no ids makes no request", func(t *testing.T) {
		var last recordedRequest
		srv := jsonServer(t, 500, `must not be called`, &last)
		c := newTestClient(t, srv)

		moved, err := c.MoveArchiveFiles(77, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if moved == nil || len(moved) != 0 {
			t.Errorf("moved = %#v, want empty non-nil slice", moved)
		}
		if last.Method != "" {
			t.Errorf("a request was sent: %+v", last)
		}
	})

	t.Run("session expired", func(t *testing.T) {
		var last recordedRequest
		srv := jsonServer(t, 200, `{"errorType":"security","message":"Niet ingelogd","code":"SECURITY_010"}`, &last)
		c := newTestClient(t, srv)

		if _, err := c.MoveArchiveFiles(77, []int{42}); !errors.Is(err, ErrSessionExpired) {
			t.Fatalf("err = %v, want ErrSessionExpired", err)
		}
	})
}

func TestDeleteArchiveFile(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		var last recordedRequest
		srv := jsonServer(t, 200, `true`, &last)
		c := newTestClient(t, srv)

		if err := c.DeleteArchiveFile(90461397); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertRequest(t, last, "DELETE", "/v1/api/folder/90461397", "")
	})

	t.Run("false is an error", func(t *testing.T) {
		var last recordedRequest
		srv := jsonServer(t, 200, `false`, &last)
		c := newTestClient(t, srv)

		if err := c.DeleteArchiveFile(1); err == nil {
			t.Fatal("expected an error for a false response")
		}
	})

	t.Run("session expired", func(t *testing.T) {
		var last recordedRequest
		srv := jsonServer(t, 401, ``, &last)
		c := newTestClient(t, srv)

		if err := c.DeleteArchiveFile(1); !errors.Is(err, ErrSessionExpired) {
			t.Fatalf("err = %v, want ErrSessionExpired", err)
		}
	})
}
