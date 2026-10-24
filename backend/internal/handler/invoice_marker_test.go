package handler

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestSubmittedMarkerIsMinimal(t *testing.T) {
	m := invoiceSubmittedMarker{MutNr: 1234, PaymentMutNr: 1235, SubmittedAt: time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)}

	// What goes into Redis.
	stored, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(stored, &fields); err != nil {
		t.Fatal(err)
	}
	if got := sortedKeys(fields); strings.Join(got, ",") != "mutNr,paymentMutNr,submittedAt" {
		t.Errorf("stored marker fields = %v, want mutNr, paymentMutNr, submittedAt", got)
	}

	// What goes to the client.
	out := submittedMarkerJSON(&m)
	if got := sortedKeys(out); strings.Join(got, ",") != "mutNr,paymentMutNr,submittedAt" {
		t.Errorf("response marker fields = %v, want mutNr, paymentMutNr, submittedAt", got)
	}
	if out["mutNr"] != 1234 || out["paymentMutNr"] != 1235 || out["submittedAt"] != "2026-10-03T10:00:00Z" {
		t.Errorf("response marker = %v", out)
	}
}

func TestSubmittedMarkerIgnoresLegacyFields(t *testing.T) {
	legacy := `{"mutNr":1234,"paymentMutNr":0,"leverancier":"Voorbeeld Leverancier","factuur":"INV-0042","bedragIncl":121.5,"submittedAt":"2026-10-03T10:00:00Z"}`
	var m invoiceSubmittedMarker
	if err := json.Unmarshal([]byte(legacy), &m); err != nil {
		t.Fatalf("legacy marker does not parse: %v", err)
	}
	if m.MutNr != 1234 || m.SubmittedAt.IsZero() {
		t.Errorf("legacy marker parsed as %+v", m)
	}
	body, err := json.Marshal(submittedMarkerJSON(&m))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"leverancier", "factuur", "bedragIncl", "Voorbeeld", "INV-0042", "121.5"} {
		if strings.Contains(string(body), leak) {
			t.Errorf("response echoes legacy field %q: %s", leak, body)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
