package handler

import (
	"strings"
	"testing"
)

func TestPasskeyName(t *testing.T) {
	tooLong := "De naam mag maximaal 64 tekens lang zijn."
	tests := []struct {
		name, raw, want, problem string
	}{
		{"trimmed", "  MacBook van Zoë  ", "MacBook van Zoë", ""},
		{"empty", "   ", "", "Geef de passkey een naam."},
		{"64 ASCII characters", strings.Repeat("a", 64), strings.Repeat("a", 64), ""},
		{"65 ASCII characters", strings.Repeat("a", 65), "", tooLong},
		// 64 characters, 128 bytes: the byte count used to refuse this.
		{"64 accented characters", strings.Repeat("ë", 64), strings.Repeat("ë", 64), ""},
		{"64 emoji", strings.Repeat("🔑", 64), strings.Repeat("🔑", 64), ""},
		{"65 accented characters", strings.Repeat("é", 65), "", tooLong},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, problem := passkeyName(tc.raw)
			if got != tc.want || problem != tc.problem {
				t.Errorf("passkeyName(%q) = (%q, %q), want (%q, %q)", tc.raw, got, problem, tc.want, tc.problem)
			}
		})
	}
}
