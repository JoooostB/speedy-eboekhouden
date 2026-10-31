package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// All data is fictitious ("Voorbeeld Leverancier"). The 39,75 invoice is the
// shape the e-boekhouden web UI sent for a two-regel "Factuur ontvangen":
// 35,80 and 3,95 at 9%, both rounded per regel (2,96 + 0,33).

// regelBTWTable is shared with the frontend (regels.test.ts, same rows, in
// cents): the HAR values, the cent amounts, and the amounts whose net value
// lies closest to a half cent on either side at 9 % and 21 % (121 and 109
// are odd, so an exact half never occurs). Both sides must agree on every
// row.
var regelBTWTable = []struct {
	inclCents int64
	code      string
	exclCents int64
	btwCents  int64
}{
	{3580, "LAAG_INK_9", 3284, 296},
	{395, "LAAG_INK_9", 362, 33},
	// Live UI check in "Incl btw" mode: 10,11 at 9 % shows btw 0,83 / excl 9,28.
	{1011, "LAAG_INK_9", 928, 83},
	{12100, "HOOG_INK_21", 10000, 2100},
	{1, "HOOG_INK_21", 1, 0},
	{2, "HOOG_INK_21", 2, 0},
	{1, "LAAG_INK_9", 1, 0},
	{6, "LAAG_INK_9", 6, 0},
	{115, "LAAG_INK_9", 106, 9},
	{224, "LAAG_INK_9", 206, 18},
	{99729, "LAAG_INK_9", 91494, 8235},
	{72, "HOOG_INK_21", 60, 12},
	{193, "HOOG_INK_21", 160, 33},
	{314, "HOOG_INK_21", 260, 54},
	{99753, "HOOG_INK_21", 82440, 17313},
	{1250, "GEEN", 1250, 0},
	{1250, "VERL_INK", 1250, 0},
}

func TestRegelBTWRoundsPerRegelLikeEBoekhouden(t *testing.T) {
	for _, tc := range regelBTWTable {
		excl, btw := regelBTW(float64(tc.inclCents)/100, tc.code)
		if cents(excl) != tc.exclCents || cents(btw) != tc.btwCents {
			t.Errorf("regelBTW(%d, %s) = %d/%d cents, want %d/%d", tc.inclCents, tc.code, cents(excl), cents(btw), tc.exclCents, tc.btwCents)
		}
		if cents(excl)+cents(btw) != tc.inclCents {
			t.Errorf("regelBTW(%d, %s): excl + btw != incl", tc.inclCents, tc.code)
		}
	}
	// The integer rule agrees with the float rule the single-line paths use
	// (roundCents of incl / (1 + rate)) for every amount up to € 20.000.
	for _, rate := range []float64{21, 9} {
		for c := int64(1); c <= 2_000_000; c++ {
			if got, want := regelExclCents(c, rate), cents(roundCents(float64(c)/100/(1+rate/100))); got != want {
				t.Fatalf("regelExclCents(%d, %v) = %d, float rule %d", c, rate, got, want)
			}
		}
	}
}

func TestNormalizeRegelsSplitsTheHARInvoice(t *testing.T) {
	in := []regelInput{
		{TegenRekeningId: 4500, BTWCode: "LAAG_INK_9", BedragIncl: 35.80},
		{TegenRekeningId: 4700, BTWCode: "LAAG_INK_9", BedragIncl: 3.95},
	}
	regels, err := normalizeRegels(in, 39.75, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(mutatieRegelMaps(regels, false))
	want := `[{"bedrag":35.8,"bedragExclusief":32.84,"bedragInclusief":35.8,"btw":2.96,"btwCode":"LAAG_INK_9","index":0,"tegenRekening":4500},` +
		`{"bedrag":3.95,"bedragExclusief":3.62,"bedragInclusief":3.95,"btw":0.33,"btwCode":"LAAG_INK_9","index":1,"tegenRekening":4700}]`
	if string(got) != want {
		t.Errorf("regels =\n %s\nwant\n %s", got, want)
	}
}

func TestNormalizeRegelsRestaurantReceipt(t *testing.T) {
	// Alcohol at 21%, food at 9%, tip without BTW; soort 6 keeps _INK_ codes,
	// an empty code is GEEN and the omschrijving travels for a receipt.
	in := []regelInput{
		{Omschrijving: "Drank", TegenRekeningId: 4300, BTWCode: "HOOG_VERK_21", BedragIncl: 24.20},
		{Omschrijving: "Eten", TegenRekeningId: 4300, BTWCode: "LAAG_INK_9", BedragIncl: 65.40},
		{Omschrijving: "Fooi", TegenRekeningId: 4300, BedragIncl: 10},
	}
	regels, err := normalizeRegels(in, 99.60, 6, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(mutatieRegelMaps(regels, true))
	want := `[{"bedrag":24.2,"bedragExclusief":20,"bedragInclusief":24.2,"btw":4.2,"btwCode":"HOOG_INK_21","index":0,"omschrijving":"Drank","tegenRekening":4300},` +
		`{"bedrag":65.4,"bedragExclusief":60,"bedragInclusief":65.4,"btw":5.4,"btwCode":"LAAG_INK_9","index":1,"omschrijving":"Eten","tegenRekening":4300},` +
		`{"bedrag":10,"bedragExclusief":10,"bedragInclusief":10,"btw":0,"btwCode":"GEEN","index":2,"omschrijving":"Fooi","tegenRekening":4300}]`
	if string(got) != want {
		t.Errorf("regels =\n %s\nwant\n %s", got, want)
	}
}

func TestNormalizeRegelsTolerance(t *testing.T) {
	in := []regelInput{
		{TegenRekeningId: 4500, BTWCode: "LAAG_INK_9", BedragIncl: 35.80},
		{TegenRekeningId: 4700, BTWCode: "LAAG_INK_9", BedragIncl: 3.95},
	}
	if _, err := normalizeRegels(in, 39.74, 1, nil); err != nil {
		t.Errorf("one cent off must pass, got %v", err)
	}
	if _, err := normalizeRegels(in, 39.76, 1, nil); err != nil {
		t.Errorf("one cent off must pass, got %v", err)
	}
	_, err := normalizeRegels(in, 39.77, 1, nil)
	if err == nil || err.Error() != "De regels tellen op tot € 39,75, het totaal is € 39,77." {
		t.Errorf("two cents off: %v", err)
	}
}

func TestNormalizeRegelsRefusals(t *testing.T) {
	ok := regelInput{TegenRekeningId: 4000, BTWCode: "GEEN", BedragIncl: 1}
	eleven := make([]regelInput, 11)
	for i := range eleven {
		eleven[i] = ok
	}
	cases := []struct {
		name  string
		in    []regelInput
		total float64
		want  string
	}{
		{"none", nil, 1, "Een boeking heeft minstens één regel nodig."},
		{"eleven", eleven, 11, "Een boeking kan maximaal 10 regels hebben."},
		{"negative line", []regelInput{ok, {TegenRekeningId: 4000, BedragIncl: -1}}, 1, "Regel 2: het bedrag moet groter zijn dan nul."},
		{"zero line", []regelInput{ok, {TegenRekeningId: 4000, BedragIncl: 0}}, 1, "Regel 2: het bedrag moet groter zijn dan nul."},
		{"no tegenrekening", []regelInput{ok, {BedragIncl: 1}}, 2, "Regel 2: kies een grootboekrekening."},
		{"code without resolver", []regelInput{{Grootboekcode: "4000", BedragIncl: 1}}, 1, "Regel 1: kies een grootboekrekening."},
		{"total zero", []regelInput{ok}, 0, "Het totaal moet groter zijn dan nul."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeRegels(tc.in, tc.total, 6, nil)
			if err == nil || err.Error() != tc.want {
				t.Errorf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestNormalizeRegelsResolvesGrootboekcode(t *testing.T) {
	resolve := func(code string) (int, bool) {
		if code == "4000" {
			return 4000, true
		}
		return 0, false
	}
	regels, err := normalizeRegels([]regelInput{
		{Grootboekcode: "4000", BTWCode: "HOOG_INK_21", BedragIncl: 60.5},
		{TegenRekeningId: 4100, BTWCode: "GEEN", BedragIncl: 10},
	}, 70.5, 5, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if regels[0].TegenRekening != 4000 || regels[1].TegenRekening != 4100 {
		t.Errorf("tegenrekeningen = %d, %d", regels[0].TegenRekening, regels[1].TegenRekening)
	}
	// soort 5 is income: the purchase code is flipped like a single line.
	if regels[0].BTWCode != "HOOG_VERK_21" {
		t.Errorf("btwCode = %s, want HOOG_VERK_21", regels[0].BTWCode)
	}
	_, err = normalizeRegels([]regelInput{{Grootboekcode: "9999", BedragIncl: 1}}, 1, 5, resolve)
	if err == nil || err.Error() != "Regel 1: grootboekrekening 9999 bestaat niet in je administratie." {
		t.Errorf("unknown code: %v", err)
	}
}

// mutatieBodies returns the bodies of every POST /v1/api/mutatie, in order.
func (f *fakeEBoekhouden) mutatieBodies() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if c.method == http.MethodPost && c.path == "/v1/api/mutatie" {
			out = append(out, c.body)
		}
	}
	return out
}

func TestSubmitFullWithRegelsBooksNRegelsAndOnePaymentLine(t *testing.T) {
	fake, h, client := submitTestEnv(t)

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{
		"leverancier": "Voorbeeld Leverancier", "factuurnummer": "VB-2026-017",
		"bedragExcl": 36.46, "bedragIncl": 39.75, "btwBedrag": 3.29, "btwCode": "LAAG_INK_9",
		"tegenRekeningId": 0, "importId": 77,
		"regels": []map[string]any{
			{"tegenRekeningId": 4500, "btwCode": "LAAG_INK_9", "bedragIncl": 35.80, "omschrijving": "Cadeau"},
			{"tegenRekeningId": 4700, "btwCode": "LAAG_INK_9", "bedragIncl": 3.95, "omschrijving": "Verzendkosten"},
		},
	}))
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	bodies := fake.mutatieBodies()
	if len(bodies) != 2 {
		t.Fatalf("got %d mutaties, want invoice + payment: %v", len(bodies), bodies)
	}
	wantInvoice := `{"mutatie":{"datum":"2026-10-12","factuur":"VB-2026-017","inEx":"IN","omschrijving":"Claude API","rekening":1600,"relatieId":55,"soort":1,"termijn":30},` +
		`"mutatieRegels":[{"bedrag":35.8,"bedragExclusief":32.84,"bedragInclusief":35.8,"btw":2.96,"btwCode":"LAAG_INK_9","index":0,"tegenRekening":4500},` +
		`{"bedrag":3.95,"bedragExclusief":3.62,"bedragInclusief":3.95,"btw":0.33,"btwCode":"LAAG_INK_9","index":1,"tegenRekening":4700}]}`
	if bodies[0] != wantInvoice {
		t.Errorf("invoice mutatie =\n %s\nwant\n %s", bodies[0], wantInvoice)
	}
	var payment struct {
		MutatieRegels []map[string]any `json:"mutatieRegels"`
	}
	if err := json.Unmarshal([]byte(bodies[1]), &payment); err != nil {
		t.Fatal(err)
	}
	if len(payment.MutatieRegels) != 1 {
		t.Errorf("payment has %d regels, want 1", len(payment.MutatieRegels))
	}
	if _, has := payment.MutatieRegels[0]["tegenRekening"]; has {
		t.Errorf("payment regel carries a tegenRekening: %v", payment.MutatieRegels[0])
	}
}

// TestSubmitFullWithRegelsSendsInclBtwShape pins the "Incl btw" shape of
// the web UI on the live-checked 10,11 at 9 %: inEx "IN", bedrag = the
// gross amount, excl 9,28 and btw 0,83 (in "EX" mode the server could
// recompute btw as 0,84 and leave a cent open on the invoice).
func TestSubmitFullWithRegelsSendsInclBtwShape(t *testing.T) {
	fake, h, client := submitTestEnv(t)

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{
		"bedragExcl": 9.28, "bedragIncl": 10.11, "btwBedrag": 0.83, "btwCode": "LAAG_INK_9", "inEx": "EX", "tegenRekeningId": 0,
		"regels": []map[string]any{{"tegenRekeningId": 4500, "btwCode": "LAAG_INK_9", "bedragIncl": 10.11}},
	}))
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	want := `{"mutatie":{"datum":"2026-10-12","factuur":"INV-0042","inEx":"IN","omschrijving":"Claude API","rekening":1600,"relatieId":55,"soort":1,"termijn":30},` +
		`"mutatieRegels":[{"bedrag":10.11,"bedragExclusief":9.28,"bedragInclusief":10.11,"btw":0.83,"btwCode":"LAAG_INK_9","index":0,"tegenRekening":4500}]}`
	if bodies := fake.mutatieBodies(); len(bodies) != 1 || bodies[0] != want {
		t.Errorf("mutatie =\n %v\nwant\n %s", bodies, want)
	}
}

func TestSubmitFullWithRegelsRefusedBeforeBooking(t *testing.T) {
	fake, h, client := submitTestEnv(t)

	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{
		"bedragIncl": 39.75, "bedragExcl": 36.46, "btwBedrag": 3.29, "tegenRekeningId": 0,
		"archiveFileId": 501,
		"regels": []map[string]any{
			{"tegenRekeningId": 4500, "btwCode": "LAAG_INK_9", "bedragIncl": 35.80},
			{"tegenRekeningId": 4700, "btwCode": "LAAG_INK_9", "bedragIncl": 1.95},
		},
	}))
	if status != http.StatusBadRequest || resp["error"] != "De regels tellen op tot € 37,75, het totaal is € 39,75." {
		t.Errorf("status %d, response %v", status, resp)
	}
	if writes := fake.writes(); len(writes) != 0 {
		t.Errorf("nothing may be booked or staged, got %v", writes)
	}
}

func TestSubmitFullWithoutRegelsStillNeedsATegenrekening(t *testing.T) {
	fake, h, client := submitTestEnv(t)
	status, resp := callSubmit(t, h.SubmitFull, client, fullRequest(map[string]any{"tegenRekeningId": 0}))
	if status != http.StatusBadRequest || resp["error"] != "Kies een grootboekrekening." {
		t.Errorf("status %d, response %v", status, resp)
	}
	if writes := fake.writes(); len(writes) != 0 {
		t.Errorf("nothing may be booked, got %v", writes)
	}
}

func TestSubmitReceiptWithRegelsBooksAgainstTheBankLine(t *testing.T) {
	fake, h, client := submitTestEnv(t)

	// Bank line 77 is -121 in the fake's import grid.
	status, resp := callSubmit(t, h.SubmitReceipt, client, receiptRequest(map[string]any{
		"leverancier": "Voorbeeld Restaurant", "omschrijving": "Diner",
		"bedragExcl": 110.74, "bedragIncl": 121, "btwBedrag": 10.26, "tegenRekeningId": 0, "importId": 77,
		"regels": []map[string]any{
			{"omschrijving": "Drank", "tegenRekeningId": 4300, "btwCode": "HOOG_INK_21", "bedragIncl": 24.20},
			{"omschrijving": "Eten", "tegenRekeningId": 4300, "btwCode": "LAAG_INK_9", "bedragIncl": 86.80},
			{"omschrijving": "Fooi", "tegenRekeningId": 4310, "btwCode": "GEEN", "bedragIncl": 10},
		},
	}))
	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, resp)
	}
	bodies := fake.mutatieBodies()
	if len(bodies) != 1 {
		t.Fatalf("got %d mutaties, want 1", len(bodies))
	}
	want := `{"importId":77,"mutatie":{"datum":"2026-10-03","inEx":"IN","omschrijving":"Voorbeeld Restaurant - Diner","rekening":1300,"soort":6},` +
		`"mutatieRegels":[{"bedrag":24.2,"bedragExclusief":20,"bedragInclusief":24.2,"btw":4.2,"btwCode":"HOOG_INK_21","index":0,"omschrijving":"Drank","tegenRekening":4300},` +
		`{"bedrag":86.8,"bedragExclusief":79.63,"bedragInclusief":86.8,"btw":7.17,"btwCode":"LAAG_INK_9","index":1,"omschrijving":"Eten","tegenRekening":4300},` +
		`{"bedrag":10,"bedragExclusief":10,"bedragInclusief":10,"btw":0,"btwCode":"GEEN","index":2,"omschrijving":"Fooi","tegenRekening":4310}]}`
	if bodies[0] != want {
		t.Errorf("mutatie =\n %s\nwant\n %s", bodies[0], want)
	}
}

func TestSubmitReceiptRegelsMustMatchTheBankLine(t *testing.T) {
	fake, h, client := submitTestEnv(t)

	// The receipt says 100 but bank line 77 is 121: the lines must add up
	// to the bank line, and nothing is staged or booked when they do not.
	status, resp := callSubmit(t, h.SubmitReceipt, client, receiptRequest(map[string]any{
		"bedragIncl": 100, "bedragExcl": 100, "btwBedrag": 0, "tegenRekeningId": 0, "importId": 77, "archiveFileId": 502,
		"regels": []map[string]any{
			{"tegenRekeningId": 4300, "btwCode": "GEEN", "bedragIncl": 60},
			{"tegenRekeningId": 4310, "btwCode": "GEEN", "bedragIncl": 40},
		},
	}))
	if status != http.StatusBadRequest || resp["error"] != "De regels tellen op tot € 100,00, het totaal is € 121,00." {
		t.Errorf("status %d, response %v", status, resp)
	}
	if writes := fake.writes(); len(writes) != 0 {
		t.Errorf("nothing may be booked, got %v", writes)
	}
}

func TestSubmitReceiptRefusesRegelsNextToExtraLines(t *testing.T) {
	fake, h, client := submitTestEnv(t)
	status, resp := callSubmit(t, h.SubmitReceipt, client, receiptRequest(map[string]any{
		"importId":   77,
		"regels":     []map[string]any{{"tegenRekeningId": 4300, "btwCode": "GEEN", "bedragIncl": 121}},
		"extraLines": []map[string]any{{"bedragIncl": 5, "btwCode": "GEEN"}},
	}))
	if status != http.StatusBadRequest || !strings.Contains(resp["error"].(string), "niet allebei") {
		t.Errorf("status %d, response %v", status, resp)
	}
	if writes := fake.writes(); len(writes) != 0 {
		t.Errorf("nothing may be booked, got %v", writes)
	}
}

// TestBatchPayloadSoort5SendsInWithNetBedrag documents the current shape of
// a Geld ontvangen booking: inEx "IN" on the mutation while bedrag is the
// net amount (the web UI pairs "IN" with the gross amount). With a BTW code
// this is a difference to look into; the test only pins what is sent today.
func TestBatchPayloadSoort5SendsInWithNetBedrag(t *testing.T) {
	item := inboxBatchItem{ID: 9001, GrootboekId: 1300, Soort: 5, Grootboekcode: "8000", BTWCode: "HOOG_VERK_21", Omschrijving: "Omzet", Bedrag: 121}
	got, err := buildBatchMutatiePayload(item, "2026-10-01", 8000)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	want := `{"importId":9001,"mutatie":{"datum":"2026-10-01","inEx":"IN","omschrijving":"Omzet","rekening":1300,"soort":5},` +
		`"mutatieRegels":[{"bedrag":100,"bedragExclusief":100,"bedragInclusief":121,"btw":21,"btwCode":"HOOG_VERK_21","index":0,"tegenRekening":8000}]}`
	if string(b) != want {
		t.Errorf("payload =\n %s\nwant\n %s", b, want)
	}
}
