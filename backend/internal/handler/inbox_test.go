package handler

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
)

func TestBuildFactuurbetalingRegel(t *testing.T) {
	tests := []struct {
		name   string
		bedrag float64
		want   map[string]any
	}{
		{
			name:   "positive amount",
			bedrag: 21.78,
			want:   map[string]any{"index": 0, "bedrag": 21.78, "btw": 0, "btwCode": "GEEN", "relatieId": 4711, "factuur": "INV-0042"},
		},
		{
			name:   "negative bank amount is sent absolute",
			bedrag: -21.78,
			want:   map[string]any{"index": 0, "bedrag": 21.78, "btw": 0, "btwCode": "GEEN", "relatieId": 4711, "factuur": "INV-0042"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildFactuurbetalingRegel(tc.bedrag, 4711, "INV-0042")
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestBuildBatchMutatiePayload(t *testing.T) {
	base := inboxBatchItem{
		ID:            900,
		GrootboekId:   12,
		Grootboekcode: "4000",
		BTWCode:       "HOOG_INK_21",
		Omschrijving:  "Betaling",
		Bedrag:        121,
		RelatieId:     4711,
		Factuurnummer: "INV-0042",
	}
	with := func(f func(*inboxBatchItem)) inboxBatchItem {
		item := base
		f(&item)
		return item
	}

	tests := []struct {
		name        string
		item        inboxBatchItem
		wantErr     error
		wantMutatie map[string]any
		wantRegel   map[string]any
	}{
		{
			name:        "soort 4 factuurbetaling verstuurd",
			item:        with(func(i *inboxBatchItem) { i.Soort = 4 }),
			wantMutatie: map[string]any{"rekening": 12, "datum": "2026-10-12", "soort": 4, "omschrijving": "Betaling"},
			wantRegel:   map[string]any{"index": 0, "bedrag": 121.0, "btw": 0, "btwCode": "GEEN", "relatieId": 4711, "factuur": "INV-0042"},
		},
		{
			name:        "soort 3 factuurbetaling ontvangen",
			item:        with(func(i *inboxBatchItem) { i.Soort = 3 }),
			wantMutatie: map[string]any{"rekening": 12, "datum": "2026-10-12", "soort": 3, "omschrijving": "Betaling"},
			wantRegel:   map[string]any{"index": 0, "bedrag": 121.0, "btw": 0, "btwCode": "GEEN", "relatieId": 4711, "factuur": "INV-0042"},
		},
		{
			name:    "soort 4 without relatie",
			item:    with(func(i *inboxBatchItem) { i.Soort = 4; i.RelatieId = 0 }),
			wantErr: errFactuurbetalingIncomplete,
		},
		{
			name:    "soort 3 without factuurnummer",
			item:    with(func(i *inboxBatchItem) { i.Soort = 3; i.Factuurnummer = "" }),
			wantErr: errFactuurbetalingIncomplete,
		},
		{
			name:    "soort 4 with blank factuurnummer",
			item:    with(func(i *inboxBatchItem) { i.Soort = 4; i.Factuurnummer = "  " }),
			wantErr: errFactuurbetalingIncomplete,
		},
		{
			name:        "soort 6 keeps inEx, tegenRekening and the BTW split",
			item:        with(func(i *inboxBatchItem) { i.Soort = 6; i.RelatieId = 0; i.Factuurnummer = "" }),
			wantMutatie: map[string]any{"rekening": 12, "datum": "2026-10-12", "soort": 6, "inEx": "EX", "omschrijving": "Betaling"},
			wantRegel: map[string]any{
				"index": 0, "bedrag": 100.0, "btw": 21.0, "btwCode": "HOOG_INK_21", "tegenRekening": 77,
				"bedragInclusief": 121.0, "bedragExclusief": 100.0,
			},
		},
		{
			name:        "soort 5 is IN with the BTW code flipped to VERK",
			item:        with(func(i *inboxBatchItem) { i.Soort = 5; i.RelatieId = 0; i.Factuurnummer = "" }),
			wantMutatie: map[string]any{"rekening": 12, "datum": "2026-10-12", "soort": 5, "inEx": "IN", "omschrijving": "Betaling"},
			wantRegel: map[string]any{
				"index": 0, "bedrag": 100.0, "btw": 21.0, "btwCode": "HOOG_VERK_21", "tegenRekening": 77,
				"bedragInclusief": 121.0, "bedragExclusief": 100.0,
			},
		},
		{
			name:        "soort 6 passes optional relatie and factuur through",
			item:        with(func(i *inboxBatchItem) { i.Soort = 6; i.BTWCode = "GEEN" }),
			wantMutatie: map[string]any{"rekening": 12, "datum": "2026-10-12", "soort": 6, "inEx": "EX", "omschrijving": "Betaling"},
			wantRegel: map[string]any{
				"index": 0, "bedrag": 121.0, "btw": 0.0, "btwCode": "GEEN", "tegenRekening": 77,
				"bedragInclusief": 121.0, "bedragExclusief": 121.0, "relatieId": 4711, "factuur": "INV-0042",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildBatchMutatiePayload(tc.item, "2026-10-12", 77)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if got != nil {
					t.Fatalf("payload = %#v, want nil on error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["importId"] != tc.item.ID {
				t.Errorf("importId = %v, want %d", got["importId"], tc.item.ID)
			}
			if !reflect.DeepEqual(got["mutatie"], tc.wantMutatie) {
				t.Errorf("mutatie = %#v, want %#v", got["mutatie"], tc.wantMutatie)
			}
			regels, _ := got["mutatieRegels"].([]map[string]any)
			if len(regels) != 1 {
				t.Fatalf("mutatieRegels = %#v, want one regel", got["mutatieRegels"])
			}
			if !reflect.DeepEqual(regels[0], tc.wantRegel) {
				t.Errorf("regel = %#v, want %#v", regels[0], tc.wantRegel)
			}
			// The payload must survive JSON encoding as CreateMutatie sends it.
			if _, err := json.Marshal(got); err != nil {
				t.Errorf("marshal: %v", err)
			}
		})
	}
}

// post builds an open post the way ParseOpenPostGrid does.
func post(bedrijf, factuur, mutDatum string, teOntvangen float64) eboekhouden.OpenPost {
	return eboekhouden.OpenPost{
		Bedrijf:     bedrijf,
		MutFactuur:  factuur,
		MutDatum:    mutDatum,
		TeOntvangen: teOntvangen,
		RelatieID:   len(bedrijf), // any stable non-zero id
		MutNr:       1000 + len(factuur),
	}
}

func TestMatchOpenPosts(t *testing.T) {
	const d = "2026-10-12T00:00:00"
	acme := post("Acme B.V.", "INV-0042", "2026-10-01", 21.78)
	globex := post("Globex Inc.", "GX-7", "2026-10-03", 21.78)
	hooli := post("Hooli", "H-1", "2026-09-20", 500)

	type want struct {
		factuur string
		nameHit bool
	}
	tests := []struct {
		name  string
		lines []bankLine
		posts openPosts
		want  map[int]want // line id → expected post; absent ids must be unmatched
	}{
		{
			name:  "exact amount, single candidate",
			lines: []bankLine{{ID: 1, Datum: d, Bedrag: -21.78, Omschrijving: "SEPA overboeking"}},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{acme}},
			want:  map[int]want{1: {"INV-0042", false}},
		},
		{
			name:  "single candidate with the company in the description is a name hit",
			lines: []bankLine{{ID: 1, Datum: d, Bedrag: -21.78, Omschrijving: "Acme factuur oktober"}},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{acme}},
			want:  map[int]want{1: {"INV-0042", true}},
		},
		{
			name:  "legal form alone is not a name hit",
			lines: []bankLine{{ID: 1, Datum: d, Bedrag: -21.78, Omschrijving: "Betaling aan een B.V."}},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{acme}},
			want:  map[int]want{1: {"INV-0042", false}},
		},
		{
			name: "one cent either way still matches, two cents does not",
			lines: []bankLine{
				{ID: 1, Datum: d, Bedrag: -10.01}, // +1 ct on A
				{ID: 2, Datum: d, Bedrag: -19.99}, // -1 ct on B
				{ID: 3, Datum: d, Bedrag: -30.02}, // +2 ct on C: out
				{ID: 4, Datum: d, Bedrag: -39.98}, // -2 ct on D: out
			},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{
				post("A", "A1", "2026-10-01", 10),
				post("B", "B1", "2026-10-01", 20),
				post("C", "C1", "2026-10-01", 30),
				post("D", "D1", "2026-10-01", 40),
			}},
			want: map[int]want{1: {"A1", false}, 2: {"B1", false}},
		},
		{
			name: "outgoing lines pay crediteur posts, incoming lines settle debiteur posts",
			lines: []bankLine{
				{ID: 1, Datum: d, Bedrag: -21.78},
				{ID: 2, Datum: d, Bedrag: 500},
			},
			posts: openPosts{
				Crediteur: []eboekhouden.OpenPost{acme},
				Debiteur:  []eboekhouden.OpenPost{hooli},
			},
			want: map[int]want{1: {"INV-0042", false}, 2: {"H-1", false}},
		},
		{
			name: "an outgoing line never matches a debiteur post and vice versa",
			lines: []bankLine{
				{ID: 1, Datum: d, Bedrag: -500},
				{ID: 2, Datum: d, Bedrag: 21.78},
			},
			posts: openPosts{
				Crediteur: []eboekhouden.OpenPost{acme},
				Debiteur:  []eboekhouden.OpenPost{hooli},
			},
			want: map[int]want{},
		},
		{
			name:  "zero amount line is ignored",
			lines: []bankLine{{ID: 1, Datum: d, Bedrag: 0}},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{post("Zero", "Z", "2026-10-01", 0)}},
			want:  map[int]want{},
		},
		{
			name:  "ambiguous amount resolved by name",
			lines: []bankLine{{ID: 1, Datum: d, Bedrag: -21.78, Omschrijving: "Globex subscription"}},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{acme, globex}},
			want:  map[int]want{1: {"GX-7", true}},
		},
		{
			name:  "ambiguous amount without a name hit gives no match",
			lines: []bankLine{{ID: 1, Datum: d, Bedrag: -21.78, Omschrijving: "SEPA overboeking"}},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{acme, globex}},
			want:  map[int]want{},
		},
		{
			name:  "ambiguous amount with two name hits gives no match",
			lines: []bankLine{{ID: 1, Datum: d, Bedrag: -21.78, Omschrijving: "Acme via Globex"}},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{acme, globex}},
			want:  map[int]want{},
		},
		{
			name: "two lines for one post: the name hit wins",
			lines: []bankLine{
				{ID: 1, Datum: "2026-10-02T00:00:00", Bedrag: -21.78, Omschrijving: "SEPA"},
				{ID: 2, Datum: "2026-10-20T00:00:00", Bedrag: -21.78, Omschrijving: "Acme"},
			},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{acme}},
			want:  map[int]want{2: {"INV-0042", true}},
		},
		{
			name: "two lines for one post: closest on or after the post date wins",
			lines: []bankLine{
				{ID: 1, Datum: "2026-10-20T00:00:00", Bedrag: -21.78},
				{ID: 2, Datum: "2026-10-05T00:00:00", Bedrag: -21.78},
			},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{acme}}, // 2026-10-01
			want:  map[int]want{2: {"INV-0042", false}},
		},
		{
			name: "two lines for one post: a line after the post date beats a closer line before it",
			lines: []bankLine{
				{ID: 1, Datum: "2026-09-30T00:00:00", Bedrag: -21.78},
				{ID: 2, Datum: "2026-10-15T00:00:00", Bedrag: -21.78},
			},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{acme}}, // 2026-10-01
			want:  map[int]want{2: {"INV-0042", false}},
		},
		{
			name: "two lines for one post: both before the post date, the closer one wins",
			lines: []bankLine{
				{ID: 1, Datum: "2026-09-01T00:00:00", Bedrag: -21.78},
				{ID: 2, Datum: "2026-09-30T00:00:00", Bedrag: -21.78},
			},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{acme}},
			want:  map[int]want{2: {"INV-0042", false}},
		},
		{
			name: "two lines for one post: same date, lowest id wins",
			lines: []bankLine{
				{ID: 9, Datum: d, Bedrag: -21.78},
				{ID: 3, Datum: d, Bedrag: -21.78},
			},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{acme}},
			want:  map[int]want{3: {"INV-0042", false}},
		},
		{
			name: "two lines for one post: unreadable post date falls back to lowest id",
			lines: []bankLine{
				{ID: 9, Datum: "2026-10-01T00:00:00", Bedrag: -21.78},
				{ID: 3, Datum: "2026-12-01T00:00:00", Bedrag: -21.78},
			},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{post("Acme", "X", "", 21.78)}},
			want:  map[int]want{3: {"X", false}},
		},
		{
			name: "zero and negative teOntvangen are ignored",
			lines: []bankLine{
				{ID: 1, Datum: d, Bedrag: -0.0001},
				{ID: 2, Datum: d, Bedrag: -5},
			},
			posts: openPosts{Crediteur: []eboekhouden.OpenPost{
				post("Paid", "P", "2026-10-01", 0),
				post("Credit", "C", "2026-10-01", -5),
			}},
			want: map[int]want{},
		},
		{
			name:  "no posts at all",
			lines: []bankLine{{ID: 1, Datum: d, Bedrag: -21.78}},
			want:  map[int]want{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOpenPosts(tc.lines, tc.posts)
			if len(got) != len(tc.want) {
				t.Errorf("matched ids = %v, want %v", keysOf(got), keysOfWant(tc.want))
			}
			for id, w := range tc.want {
				m, ok := got[id]
				if !ok {
					t.Errorf("line %d: no match, want %s", id, w.factuur)
					continue
				}
				if m.Post.MutFactuur != w.factuur || m.NameHit != w.nameHit {
					t.Errorf("line %d: got (%s, nameHit=%v), want (%s, nameHit=%v)", id, m.Post.MutFactuur, m.NameHit, w.factuur, w.nameHit)
				}
			}
		})
	}
}

func keysOf(m map[int]openPostMatch) []int {
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func keysOfWant[V any](m map[int]V) []int {
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func TestMatchOpenPostsIsDeterministic(t *testing.T) {
	lines := []bankLine{
		{ID: 5, Datum: "2026-10-20T00:00:00", Bedrag: -21.78, Omschrijving: "Acme"},
		{ID: 2, Datum: "2026-10-02T00:00:00", Bedrag: -21.78, Omschrijving: "SEPA"},
		{ID: 7, Datum: "2026-10-02T00:00:00", Bedrag: -99, Omschrijving: "Globex"},
		{ID: 8, Datum: "2026-10-03T00:00:00", Bedrag: -99, Omschrijving: "Globex again"},
		{ID: 9, Datum: "2026-10-03T00:00:00", Bedrag: 500, Omschrijving: "Hooli"},
	}
	posts := openPosts{
		Crediteur: []eboekhouden.OpenPost{
			post("Acme B.V.", "INV-0042", "2026-10-01", 21.78),
			post("Globex Inc.", "GX-7", "2026-10-03", 99),
		},
		Debiteur: []eboekhouden.OpenPost{post("Hooli", "H-1", "2026-09-20", 500)},
	}
	want := matchOpenPosts(lines, posts)
	if len(want) != 3 {
		t.Fatalf("expected 3 matches, got %v", keysOf(want))
	}

	reversedLines := slices.Clone(lines)
	slices.Reverse(reversedLines)
	reversedPosts := openPosts{
		Crediteur: slices.Clone(posts.Crediteur),
		Debiteur:  slices.Clone(posts.Debiteur),
	}
	slices.Reverse(reversedPosts.Crediteur)

	for i := 0; i < 20; i++ {
		if got := matchOpenPosts(lines, posts); !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d differs: %#v vs %#v", i, got, want)
		}
		if got := matchOpenPosts(reversedLines, reversedPosts); !reflect.DeepEqual(got, want) {
			t.Fatalf("reversed input differs: %#v vs %#v", got, want)
		}
	}
}

func TestOpenPostIndicator(t *testing.T) {
	tests := []struct {
		name string
		post eboekhouden.OpenPost
		want string
	}{
		{"full", post("Acme B.V.", "INV-0042", "2026-10-01T00:00:00", 1), "Acme B.V. · INV-0042 · geboekt 1 okt"},
		{"no invoice number", post("Acme", "", "2026-03-12", 1), "Acme · geboekt 12 mrt"},
		{"no date", post("Acme", "X", "", 1), "Acme · X"},
		{"no company", post("", "X", "2026-10-01", 1), "Onbekende relatie · X · geboekt 1 okt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := openPostIndicator(tc.post); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestApplyInboxOverlays(t *testing.T) {
	base := []inboxClassifiedRow{
		{ID: 1, Bedrag: -21.78, Omschrijving: "Acme oktober", Category: "review", Confidence: 0.6, Grootboekcode: "4000", BTWCode: "HOOG_INK_21", Soort: "GeldUitgegeven", AIOmschrijving: "Software", Indicator: "AI", Learned: true},
		{ID: 2, Bedrag: -50, Omschrijving: "Coolblue", Category: "review", Confidence: 0.5, Grootboekcode: "4100", Soort: "GeldUitgegeven"},
		{ID: 3, Bedrag: 500, Omschrijving: "Hooli", Category: "manual"},
		{ID: 4, Bedrag: -12, Omschrijving: "Koffie", Category: "auto", Confidence: 0.95, Grootboekcode: "4200", Soort: "GeldUitgegeven"},
	}
	snapshot, _ := json.Marshal(base)

	acme := post("Acme B.V.", "INV-0042", "2026-10-01", 21.78)
	hooli := post("Hooli", "H-1", "2026-09-20", 500)
	posts := map[int]openPostMatch{
		1: {Post: acme, NameHit: true},
		3: {Post: hooli, NameHit: false},
	}
	docs := map[int]inboxDocumentMatch{
		1: {FileID: 777, Title: "Acme · 1 okt 2026 · € 21,78"}, // loses to the open post
		2: {FileID: 888, Title: "Coolblue · 3 okt 2026 · € 50,00"},
	}

	rows := applyInboxOverlays(base, posts, docs)

	if after, _ := json.Marshal(base); string(after) != string(snapshot) {
		t.Fatalf("base was modified:\n%s\n%s", snapshot, after)
	}
	if len(rows) != len(base) {
		t.Fatalf("len = %d, want %d", len(rows), len(base))
	}

	// Line 1: open post beats document match and the learned/Claude base.
	r := rows[0]
	if !r.OpenPostMatch || r.DocumentMatch != nil {
		t.Errorf("line 1: openPostMatch=%v documentMatch=%v, want open post only", r.OpenPostMatch, r.DocumentMatch)
	}
	if r.Category != "auto" || r.NeedsInvoice || r.Confidence != 1.0 || r.Learned {
		t.Errorf("line 1: category=%s needsInvoice=%v confidence=%v learned=%v", r.Category, r.NeedsInvoice, r.Confidence, r.Learned)
	}
	if r.Soort != "FactuurbetalingVerstuurd" || r.BTWCode != "GEEN" || r.Grootboekcode != "" {
		t.Errorf("line 1: soort=%s btw=%s grootboek=%q", r.Soort, r.BTWCode, r.Grootboekcode)
	}
	if r.RelatieID != acme.RelatieID || r.RelatieNaam != "Acme B.V." || r.Factuurnummer != "INV-0042" {
		t.Errorf("line 1: relatie=%d/%q factuur=%q", r.RelatieID, r.RelatieNaam, r.Factuurnummer)
	}
	if r.AIOmschrijving != "Acme oktober" || r.Indicator != "Acme B.V. · INV-0042 · geboekt 1 okt" {
		t.Errorf("line 1: aiOmschrijving=%q indicator=%q", r.AIOmschrijving, r.Indicator)
	}

	// Line 2: document match turns the review line into an invoice line and
	// keeps the classification as prefill.
	r = rows[1]
	if r.DocumentMatch == nil || r.DocumentMatch.FileID != 888 || r.DocumentMatch.Title != "Coolblue · 3 okt 2026 · € 50,00" {
		t.Errorf("line 2: documentMatch=%+v", r.DocumentMatch)
	}
	if r.Category != "invoice" || !r.NeedsInvoice || r.OpenPostMatch {
		t.Errorf("line 2: category=%s needsInvoice=%v openPostMatch=%v", r.Category, r.NeedsInvoice, r.OpenPostMatch)
	}
	if r.Grootboekcode != "4100" || r.Confidence != 0.5 || r.Soort != "GeldUitgegeven" {
		t.Errorf("line 2: prefill lost: %+v", r)
	}

	// Line 3: incoming line on a debiteur post without a name hit: suggested,
	// but for review, never approvable in bulk.
	r = rows[2]
	if !r.OpenPostMatch || r.Soort != "FactuurbetalingOntvangen" || r.Confidence != 0.9 || r.Category != "review" {
		t.Errorf("line 3: %+v", r)
	}

	// Line 4: untouched.
	if !reflect.DeepEqual(rows[3], base[3]) {
		t.Errorf("line 4 changed: %+v", rows[3])
	}

	// Summary is recounted after the overlay.
	wantSummary := map[string]int{"auto": 2, "review": 1, "invoice": 1, "manual": 0}
	if got := summarizeInbox(rows); !reflect.DeepEqual(got, wantSummary) {
		t.Errorf("summary = %v, want %v", got, wantSummary)
	}
	wantBaseSummary := map[string]int{"auto": 1, "review": 2, "invoice": 0, "manual": 1}
	if got := summarizeInbox(base); !reflect.DeepEqual(got, wantBaseSummary) {
		t.Errorf("base summary = %v, want %v", got, wantBaseSummary)
	}
}

// An open post with the same amount but without the company in the bank
// description must not overrule what the user's confirmed learned memory
// decided: a €50 subscription booked as cost twice stays a cost even when an
// unrelated €50 invoice is open. With a name hit the open post still wins.
func TestApplyInboxOverlaysKeepsLearnedRowOnAmountOnlyMatch(t *testing.T) {
	learnedRow := inboxClassifiedRow{
		ID: 1, Bedrag: -50, Omschrijving: "Voorbeeld Abonnement oktober", Category: "auto", Confidence: 1.0,
		Grootboekcode: "4300", BTWCode: "HOOG_INK_21", Soort: "GeldUitgegeven", Indicator: "Eerder geboekt", Learned: true,
	}
	other := post("Andere Leverancier B.V.", "AL-7", "2026-10-01", 50)

	rows := applyInboxOverlays([]inboxClassifiedRow{learnedRow}, map[int]openPostMatch{1: {Post: other, NameHit: false}}, nil)
	if !reflect.DeepEqual(rows[0], learnedRow) {
		t.Errorf("amount-only match overruled the learned row: %+v", rows[0])
	}

	rows = applyInboxOverlays([]inboxClassifiedRow{learnedRow}, map[int]openPostMatch{1: {Post: other, NameHit: true}}, nil)
	if !rows[0].OpenPostMatch || rows[0].Category != "auto" || rows[0].Learned {
		t.Errorf("name hit should still win over the learned row: %+v", rows[0])
	}
}

func TestApplyInboxOverlaysWithoutMatches(t *testing.T) {
	base := []inboxClassifiedRow{{ID: 1, Category: "review"}, {ID: 2, Category: "manual"}}
	rows := applyInboxOverlays(base, nil, nil)
	if !reflect.DeepEqual(rows, base) {
		t.Errorf("rows changed without overlays: %+v", rows)
	}
	if rows == nil {
		t.Error("rows must not be nil")
	}
}

func TestInboxClassifiedRowJSONOmitsOverlayFieldsWhenUnset(t *testing.T) {
	b, err := json.Marshal(inboxClassifiedRow{ID: 1, Category: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"relatieId", "relatieNaam", "factuurnummer", "openPostMatch", "documentMatch", "verification", "paymentGroupId"} {
		if json.Valid(b) && containsJSONKey(b, key) {
			t.Errorf("unset %s is present in %s", key, b)
		}
	}
	b, _ = json.Marshal(withOpenPostMatch(inboxClassifiedRow{ID: 1, Bedrag: -1}, openPostMatch{Post: post("A", "F", "", 1)}))
	for _, key := range []string{"relatieId", "relatieNaam", "factuurnummer", "openPostMatch"} {
		if !containsJSONKey(b, key) {
			t.Errorf("%s missing in %s", key, b)
		}
	}
}

func containsJSONKey(b []byte, key string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return false
	}
	_, ok := m[key]
	return ok
}

func TestDocumentMatchesByBankLine(t *testing.T) {
	docs := []mailDocument{
		{ID: 1, Title: "Acme · 1 okt 2026 · € 21,78", Status: docStatusMatched, matchedLineID: 100},
		{ID: 2, Title: "Linked", Status: docStatusLinked, matchedLineID: 200},       // already booked
		{ID: 3, Title: "Ignored", Status: docStatusIgnored, matchedLineID: 300},     // hidden
		{ID: 4, Title: "Duplicate", Status: docStatusDuplicate, matchedLineID: 400}, // booked before
		{ID: 5, Title: "Unmatched", Status: docStatusUnmatched},
		{ID: 6, Title: "New", Status: docStatusNew},
		{ID: 7, Title: "Receipt", Status: docStatusMatched, matchedLineID: 700},
	}
	got := documentMatchesByBankLine(docs)
	want := map[int]inboxDocumentMatch{
		100: {FileID: 1, Title: "Acme · 1 okt 2026 · € 21,78"},
		700: {FileID: 7, Title: "Receipt"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBankLinesRoundTrip(t *testing.T) {
	rows := []map[string]any{
		bankRow(1, "2026-10-12T00:00:00", -21.78, "Acme"),
		bankRow(2, "2026-10-13T00:00:00", 500, "Hooli"),
	}
	lines := bankLinesFromRows(rows)
	want := []bankLine{
		{ID: 1, Datum: "2026-10-12T00:00:00", Bedrag: -21.78, Omschrijving: "Acme"},
		{ID: 2, Datum: "2026-10-13T00:00:00", Bedrag: 500, Omschrijving: "Hooli"},
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("bankLinesFromRows = %+v, want %+v", lines, want)
	}
	if again := bankLinesFromRows(gridRowsFromBankLines(lines)); !reflect.DeepEqual(again, want) {
		t.Errorf("grid round trip = %+v, want %+v", again, want)
	}
	classified := []inboxClassifiedRow{
		{ID: 1, Datum: "2026-10-12T00:00:00", Bedrag: -21.78, Omschrijving: "Acme", Category: "auto"},
		{ID: 2, Datum: "2026-10-13T00:00:00", Bedrag: 500, Omschrijving: "Hooli", Category: "manual"},
	}
	if got := bankLinesFromClassified(classified); !reflect.DeepEqual(got, want) {
		t.Errorf("bankLinesFromClassified = %+v, want %+v", got, want)
	}
	// The synthesized grid rows feed rankBankLineMatches unchanged.
	inv := testInvoice("factuur", 21.78, "EUR", "2026-10-10", "Acme")
	if ids := candidateIDs(rankBankLineMatches(inv, gridRowsFromBankLines(lines), nil)); !reflect.DeepEqual(ids, []int{1}) {
		t.Errorf("rankBankLineMatches on synthesized rows = %v, want [1]", ids)
	}
}

func TestInboxClassifyBaseReadsLegacyCache(t *testing.T) {
	// A cache entry written before the overlays existed has the same shape.
	legacy := `{"classifications":[{"id":1,"datum":"2026-10-12T00:00:00","bedrag":-21.78,"omschrijving":"Acme","rekening":"","grootboekId":5,"category":"review","needsInvoice":false,"confidence":0.6,"grootboekcode":"4000","btwCode":"HOOG_INK_21","soort":"GeldUitgegeven","aiOmschrijving":"Software","indicator":"AI","learned":false}],"totalCount":1,"summary":{"auto":0,"review":1,"invoice":0,"manual":0}}`
	var b inboxClassifyBase
	if err := json.Unmarshal([]byte(legacy), &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Classifications) != 1 || b.TotalCount != 1 || b.Summary["review"] != 1 {
		t.Errorf("parsed %+v", b)
	}
	r := b.Classifications[0]
	if r.OpenPostMatch || r.DocumentMatch != nil || r.RelatieID != 0 || r.Factuurnummer != "" {
		t.Errorf("legacy row has overlay fields set: %+v", r)
	}
}

func TestIsFactuurbetaling(t *testing.T) {
	for soort, want := range map[int]bool{1: false, 2: false, 3: true, 4: true, 5: false, 6: false, 7: false, 0: false} {
		if got := isFactuurbetaling(soort); got != want {
			t.Errorf("isFactuurbetaling(%d) = %v, want %v", soort, got, want)
		}
	}
}
