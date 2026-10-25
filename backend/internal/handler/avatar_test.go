package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// avatarFakes plays R2 and the users table and records every call in order,
// so the tests can check what happens first.
type avatarFakes struct {
	ops       []string
	avatarKey string
	setErr    error
}

func (f *avatarFakes) Upload(_ context.Context, key string, _ []byte, _ string) error {
	f.ops = append(f.ops, "upload "+key)
	return nil
}

func (f *avatarFakes) Delete(_ context.Context, key string) error {
	f.ops = append(f.ops, "delete "+key)
	return nil
}

func (f *avatarFakes) PublicObjectURL(key string) string { return "https://cdn.voorbeeld.nl/" + key }

func (f *avatarFakes) GetUserByID(_ context.Context, id string) (*database.User, error) {
	return &database.User{ID: id, AvatarKey: f.avatarKey}, nil
}

func (f *avatarFakes) SetAvatarKey(_ context.Context, _ string, key string) error {
	if f.setErr != nil {
		f.ops = append(f.ops, "set failed")
		return f.setErr
	}
	f.ops = append(f.ops, "set "+key)
	f.avatarKey = key
	return nil
}

// newKey is the key of the object the upload wrote, from the recorded calls.
func (f *avatarFakes) newKey(t *testing.T) string {
	t.Helper()
	for _, op := range f.ops {
		if k, ok := strings.CutPrefix(op, "upload "); ok {
			return k
		}
	}
	t.Fatalf("no upload in %v", f.ops)
	return ""
}

func callAvatar(t *testing.T, handler gin.HandlerFunc, method string, withFile bool) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	contentType := ""
	if withFile {
		mw := multipart.NewWriter(&body)
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition", `form-data; name="avatar"; filename="foto.png"`)
		hdr.Set("Content-Type", "image/png")
		part, err := mw.CreatePart(hdr)
		if err != nil {
			t.Fatal(err)
		}
		part.Write([]byte("\x89PNG\r\n\x1a\n"))
		mw.Close()
		contentType = mw.FormDataContentType()
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/api/v1/avatar", &body)
	if contentType != "" {
		c.Request.Header.Set("Content-Type", contentType)
	}
	c.Set(session.ContextKey, &session.SessionData{UserID: "user-1"})
	handler(c)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %s", w.Body.String())
	}
	return w.Code, resp
}

func TestAvatarUploadSavesBeforeDeletingTheOldPhoto(t *testing.T) {
	f := &avatarFakes{avatarKey: "avatars/oud.png"}
	h := &AvatarHandler{r2: f, db: f}

	status, resp := callAvatar(t, h.Upload, http.MethodPost, true)
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	key := f.newKey(t)
	want := []string{"upload " + key, "set " + key, "delete avatars/oud.png"}
	if !reflect.DeepEqual(f.ops, want) {
		t.Errorf("calls = %v, want %v", f.ops, want)
	}
	if resp["avatarKey"] != key || resp["avatarUrl"] != "https://cdn.voorbeeld.nl/"+key {
		t.Errorf("response = %v", resp)
	}
}

func TestAvatarUploadKeepsTheOldPhotoWhenSavingFails(t *testing.T) {
	f := &avatarFakes{avatarKey: "avatars/oud.png", setErr: errors.New("database unavailable")}
	h := &AvatarHandler{r2: f, db: f}

	status, resp := callAvatar(t, h.Upload, http.MethodPost, true)
	if status != http.StatusInternalServerError || resp["error"] != "De foto kon niet worden opgeslagen. Probeer het opnieuw." {
		t.Fatalf("status %d: %v", status, resp)
	}
	key := f.newKey(t)
	// The new object goes again; the old one, still in the profile, stays.
	want := []string{"upload " + key, "set failed", "delete " + key}
	if !reflect.DeepEqual(f.ops, want) {
		t.Errorf("calls = %v, want %v", f.ops, want)
	}
	if f.avatarKey != "avatars/oud.png" {
		t.Errorf("profile points at %q", f.avatarKey)
	}
}

func TestAvatarUploadWithoutPreviousPhoto(t *testing.T) {
	f := &avatarFakes{}
	h := &AvatarHandler{r2: f, db: f}

	if status, resp := callAvatar(t, h.Upload, http.MethodPost, true); status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	key := f.newKey(t)
	if want := []string{"upload " + key, "set " + key}; !reflect.DeepEqual(f.ops, want) {
		t.Errorf("calls = %v, want %v", f.ops, want)
	}
}

func TestAvatarDeleteClearsTheProfileFirst(t *testing.T) {
	f := &avatarFakes{avatarKey: "avatars/oud.png"}
	h := &AvatarHandler{r2: f, db: f}

	if status, resp := callAvatar(t, h.Delete, http.MethodDelete, false); status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	if want := []string{"set ", "delete avatars/oud.png"}; !reflect.DeepEqual(f.ops, want) {
		t.Errorf("calls = %v, want %v", f.ops, want)
	}
}

func TestAvatarDeleteKeepsThePhotoWhenClearingFails(t *testing.T) {
	f := &avatarFakes{avatarKey: "avatars/oud.png", setErr: errors.New("database unavailable")}
	h := &AvatarHandler{r2: f, db: f}

	status, resp := callAvatar(t, h.Delete, http.MethodDelete, false)
	if status != http.StatusInternalServerError || resp["error"] != "De foto kon niet worden verwijderd. Probeer het opnieuw." {
		t.Fatalf("status %d: %v", status, resp)
	}
	if want := []string{"set failed"}; !reflect.DeepEqual(f.ops, want) {
		t.Errorf("calls = %v, want %v (the object must stay)", f.ops, want)
	}
}

func TestNewAvatarHandlerWithoutR2(t *testing.T) {
	h := NewAvatarHandler(nil, nil)
	if h.r2 != nil {
		t.Fatal("a nil R2 client must leave the store nil, not a typed nil")
	}
	status, resp := callAvatar(t, h.Upload, http.MethodPost, true)
	if status != http.StatusServiceUnavailable {
		t.Errorf("status %d: %v", status, resp)
	}
}
