package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// The session cookie is re-issued on every authenticated request with these
// attributes; login uses the same helper, so a refresh never changes them.
func TestSetCookieAttributes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	SetCookie(c, "sess-123", 30*time.Minute, CookieOptions{Domain: "voorbeeld.test", Secure: true})

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	ck := cookies[0]
	if ck.Name != CookieName || ck.Value != "sess-123" || ck.Path != "/" || ck.Domain != "voorbeeld.test" {
		t.Errorf("cookie = %+v", ck)
	}
	if ck.MaxAge != 1800 || !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteLaxMode {
		t.Errorf("maxAge=%d httpOnly=%v secure=%v sameSite=%v, want 1800/true/true/Lax", ck.MaxAge, ck.HttpOnly, ck.Secure, ck.SameSite)
	}
}
