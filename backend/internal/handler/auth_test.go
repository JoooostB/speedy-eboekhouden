package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// callDisconnect runs Disconnect for sess on a handler without a session
// store or database (both nil-guarded) and decodes the response.
func callDisconnect(t *testing.T, h *EBoekhoudenAuthHandler, sess *session.SessionData) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/eboekhouden/disconnect", nil)
	c.Set(session.ContextKey, sess)
	h.Disconnect(c)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %s", w.Body.String())
	}
	return w.Code, resp
}

func TestDisconnectLogsOutPurgesAndReports(t *testing.T) {
	fake := newFakeScanner(2, append(userKeys(testUserA), userKeys(testUserB)...)...)
	var gotToken string
	h := &EBoekhoudenAuthHandler{
		redis:    fake,
		logoutEB: func(token string) bool { gotToken = token; return true },
	}
	h.pendingMFA.Store("sess-1", &pendingMFAEntry{})
	sess := &session.SessionData{ID: "sess-1", UserID: testUserA, EBoekhoudenToken: "tok-123"}

	code, resp := callDisconnect(t, h, sess)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", code, resp)
	}
	if gotToken != "tok-123" {
		t.Errorf("logout called with token %q, want tok-123", gotToken)
	}
	if resp["status"] != "ok" || resp["ebLoggedOut"] != true {
		t.Errorf("response = %v, want status ok and ebLoggedOut true", resp)
	}
	if n, _ := resp["purgedKeys"].(float64); int(n) != len(userKeys(testUserA)) {
		t.Errorf("purgedKeys = %v, want %d", resp["purgedKeys"], len(userKeys(testUserA)))
	}
	if sess.EBoekhoudenToken != "" || sess.MFAPending {
		t.Errorf("session still carries token=%q mfaPending=%v", sess.EBoekhoudenToken, sess.MFAPending)
	}
	if _, ok := h.pendingMFA.Load("sess-1"); ok {
		t.Error("pending MFA entry was not dropped")
	}
	// The other user's keys are untouched.
	if live := fake.live(); len(live) != len(userKeys(testUserB)) {
		t.Errorf("live keys after purge = %v, want only user B's", live)
	}
}

func TestDisconnectHonestWhenLogoutNotConfirmed(t *testing.T) {
	h := &EBoekhoudenAuthHandler{
		redis:    newFakeScanner(0, userKeys(testUserA)...),
		logoutEB: func(string) bool { return false },
	}
	sess := &session.SessionData{ID: "sess-2", UserID: testUserA, EBoekhoudenToken: "tok"}

	code, resp := callDisconnect(t, h, sess)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if resp["ebLoggedOut"] != false {
		t.Errorf("ebLoggedOut = %v, want false", resp["ebLoggedOut"])
	}
	// The token is cleared and the caches are purged regardless.
	if sess.EBoekhoudenToken != "" {
		t.Error("token not cleared")
	}
	if n, _ := resp["purgedKeys"].(float64); int(n) != len(userKeys(testUserA)) {
		t.Errorf("purgedKeys = %v, want %d", resp["purgedKeys"], len(userKeys(testUserA)))
	}
}

// A stalled e-boekhouden must not hold up "Verbinding verbreken": after the
// logout timeout the token is cleared anyway and the answer is honest.
func TestDisconnectDoesNotWaitForAStalledLogout(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := &EBoekhoudenAuthHandler{
		redis:         newFakeScanner(0, userKeys(testUserA)...),
		logoutEB:      func(string) bool { <-release; return true },
		logoutTimeout: 20 * time.Millisecond,
	}
	sess := &session.SessionData{ID: "sess-5", UserID: testUserA, EBoekhoudenToken: "tok"}

	start := time.Now()
	code, resp := callDisconnect(t, h, sess)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Disconnect waited %v for a stalled logout", elapsed)
	}
	if code != http.StatusOK || resp["ebLoggedOut"] != false {
		t.Errorf("status = %d, response = %v, want 200 with ebLoggedOut false", code, resp)
	}
	if sess.EBoekhoudenToken != "" {
		t.Error("token not cleared")
	}
	if n, _ := resp["purgedKeys"].(float64); int(n) != len(userKeys(testUserA)) {
		t.Errorf("purgedKeys = %v, want %d", resp["purgedKeys"], len(userKeys(testUserA)))
	}
}

func TestDisconnectWithoutTokenSkipsLogout(t *testing.T) {
	called := false
	h := &EBoekhoudenAuthHandler{logoutEB: func(string) bool { called = true; return true }}
	sess := &session.SessionData{ID: "sess-3", UserID: testUserA}

	code, resp := callDisconnect(t, h, sess)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if called {
		t.Error("logout must not run without a token")
	}
	if resp["ebLoggedOut"] != false || resp["purgedKeys"] != float64(0) {
		t.Errorf("response = %v, want ebLoggedOut false and purgedKeys 0 without Redis", resp)
	}
}

func TestDisconnectPendingMFASkipsLogout(t *testing.T) {
	called := false
	h := &EBoekhoudenAuthHandler{logoutEB: func(string) bool { called = true; return true }}
	// A pending MFA session has no usable token yet; its in-memory client
	// is dropped instead.
	h.pendingMFA.Store("sess-4", &pendingMFAEntry{})
	sess := &session.SessionData{ID: "sess-4", UserID: testUserA, EBoekhoudenToken: "half", MFAPending: true}

	callDisconnect(t, h, sess)
	if called {
		t.Error("logout must not run while MFA is pending")
	}
	if _, ok := h.pendingMFA.Load("sess-4"); ok {
		t.Error("pending MFA entry was not dropped")
	}
	if sess.MFAPending || sess.EBoekhoudenToken != "" {
		t.Error("session not reset")
	}
}

func TestDisconnectRequiresSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	(&EBoekhoudenAuthHandler{}).Disconnect(c)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}
