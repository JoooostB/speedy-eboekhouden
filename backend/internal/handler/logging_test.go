package handler

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
)

func TestErrClass(t *testing.T) {
	api := &eboekhouden.APIError{Path: "/v1/api/mutatie", Status: 200, Code: "VAL_001", Message: "Relatie 'Voorbeeld Leverancier' bestaat al"}

	tests := []struct {
		name     string
		err      error
		want     string
		wantNot  string
		contains bool
	}{
		{"nil", nil, "<nil>", "", false},
		{"session expired", eboekhouden.ErrSessionExpired, "session_expired", "", false},
		{"wrapped session expired", fmt.Errorf("listing: %w", eboekhouden.ErrSessionExpired), "session_expired", "", false},
		{"api error drops the message", api, `code="VAL_001"`, "Voorbeeld", true},
		{"wrapped api error", fmt.Errorf("booking: %w", api), "status=200", "Voorbeeld", true},
		{"internal error keeps its message", errors.New("redis: connection refused"), "redis: connection refused", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := errClass(tc.err)
			if tc.contains {
				if !strings.Contains(got, tc.want) {
					t.Errorf("errClass = %q, want containing %q", got, tc.want)
				}
			} else if got != tc.want {
				t.Errorf("errClass = %q, want %q", got, tc.want)
			}
			if tc.wantNot != "" && strings.Contains(got, tc.wantNot) {
				t.Errorf("errClass = %q leaks %q", got, tc.wantNot)
			}
		})
	}
}
