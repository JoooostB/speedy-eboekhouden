package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
)

// The inbox overlays (open posts, mail-folder documents) call endpoints the
// bank-line inbox never needed before. Whatever those answer, including the
// signals that normally mean "session expired", the overlay is just empty:
// e-boekhouden may answer a missing right or plan module that way, and that
// must not lock the user out of an inbox that works.
func TestOverlayFetchesAreBestEffort(t *testing.T) {
	answers := []struct {
		name   string
		status int
		body   string
	}{
		{"401", http.StatusUnauthorized, `{}`},
		{"security envelope", http.StatusOK, `{"errorType":"security","message":"Geen toegang","code":"SECURITY_010"}`},
		{"server error", http.StatusInternalServerError, `oops`},
	}
	for _, a := range answers {
		t.Run(a.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(a.status)
				w.Write([]byte(a.body))
			}))
			defer srv.Close()
			client, err := eboekhouden.NewClientForTesting(srv.URL, "test-token")
			if err != nil {
				t.Fatal(err)
			}

			posts := fetchOpenPosts(client)
			if len(posts.Crediteur) != 0 || len(posts.Debiteur) != 0 {
				t.Errorf("fetchOpenPosts = %+v, want no posts", posts)
			}

			lines := []bankLine{{ID: 1, Bedrag: -50}}
			if got := mailDocumentMatches(context.Background(), nil, client, "user-1", lines); len(got) != 0 {
				t.Errorf("mailDocumentMatches = %v, want no annotations", got)
			}
		})
	}
}
