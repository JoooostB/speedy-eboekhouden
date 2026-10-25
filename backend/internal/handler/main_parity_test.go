package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// Characterisation against main (a524a1a). The mutation payloads that
// BatchProcess (soort 1, 2, 5, 6, 7), SubmitFull (soort 1 + soort 4 payment)
// and SubmitReceipt (soort 6) send to e-boekhouden must stay byte for byte
// what main sent for the same input; the mainXxx functions below are main's
// inline code, copied verbatim apart from the function wrapper, and serve as
// the reference. Soort 3/4 in BatchProcess changed on purpose (the
// factuurbetaling shape) and is covered by TestBuildBatchMutatiePayload.
//
// All data is fictitious.

// --- main's reference code ---------------------------------------------------

func mainNormalizeBTWForSoort(code string, soort int) string {
	isIncome := soort == 3 || soort == 5
	isExpense := soort == 4 || soort == 6
	if !isIncome && !isExpense {
		return code
	}
	switch code {
	case "HOOG_INK_21":
		if isIncome {
			return "HOOG_VERK_21"
		}
	case "HOOG_VERK_21":
		if isExpense {
			return "HOOG_INK_21"
		}
	case "LAAG_INK_9":
		if isIncome {
			return "LAAG_VERK_9"
		}
	case "LAAG_VERK_9":
		if isExpense {
			return "LAAG_INK_9"
		}
	}
	return code
}

func mainBtwRateFromCode(code string) float64 {
	switch code {
	case "HOOG_INK_21", "HOOG_VERK_21":
		return 21
	case "LAAG_INK_9", "LAAG_VERK_9":
		return 9
	}
	return 0
}

func mainRoundCents(v float64) float64 {
	if v >= 0 {
		return float64(int64(v*100+0.5)) / 100
	}
	return float64(int64(v*100-0.5)) / 100
}

func mainTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func mainNilIfZero(i int) any {
	if i == 0 {
		return nil
	}
	return i
}

// mainBatchPayload is the body main's BatchProcess built per item
// (inbox.go, the loop body after the tegenrekening and date lookup).
func mainBatchPayload(item inboxBatchItem, mutDatum string, tegenRekeningId int) []byte {
	btwCode := mainNormalizeBTWForSoort(item.BTWCode, item.Soort)
	inEx := "EX"
	if item.Soort == 3 || item.Soort == 5 {
		inEx = "IN"
	}
	rate := mainBtwRateFromCode(btwCode)
	bedragIncl := item.Bedrag
	bedragExcl := item.Bedrag
	btwBedrag := 0.0
	if rate > 0 {
		btwBedrag = mainRoundCents(bedragIncl * rate / (100 + rate))
		bedragExcl = mainRoundCents(bedragIncl - btwBedrag)
	}
	regel := map[string]any{
		"index":           0,
		"bedrag":          bedragExcl,
		"btw":             btwBedrag,
		"btwCode":         btwCode,
		"tegenRekening":   tegenRekeningId,
		"bedragInclusief": bedragIncl,
		"bedragExclusief": bedragExcl,
	}
	if item.RelatieId != 0 {
		regel["relatieId"] = item.RelatieId
	}
	if item.Factuurnummer != "" {
		regel["factuur"] = item.Factuurnummer
	}
	mutPayload, _ := json.Marshal(map[string]any{
		"mutatie": map[string]any{
			"rekening":     item.GrootboekId,
			"datum":        mutDatum,
			"soort":        item.Soort,
			"inEx":         inEx,
			"omschrijving": item.Omschrijving,
		},
		"mutatieRegels": []map[string]any{regel},
		"importId":      item.ID,
	})
	return mutPayload
}

// mainSubmitFullReq holds the SubmitFull request fields main's payloads read,
// after main's defaults (inEx EX, btwCode HOOG_INK_21, crediteuren lookup).
type mainSubmitFullReq struct {
	Datum, Leverancier, Factuurnummer, Omschrijving string
	BedragExcl, BedragIncl, BTWBedrag               float64
	BTWCode, InEx                                   string
	RelatieId, TegenRekeningId, RekeningId          int
	ImportId                                        int
}

// mainSubmitFullInvoicePayload is main's SubmitFull step 2 (soort 1).
func mainSubmitFullInvoicePayload(req mainSubmitFullReq) []byte {
	invoiceMutPayload, _ := json.Marshal(map[string]any{
		"mutatie": map[string]any{
			"rekening":     req.RekeningId,
			"relatieId":    req.RelatieId,
			"datum":        req.Datum,
			"termijn":      30,
			"factuur":      req.Factuurnummer,
			"soort":        1,
			"inEx":         req.InEx,
			"omschrijving": mainTruncate(req.Omschrijving, 200),
		},
		"mutatieRegels": []map[string]any{{
			"index":           0,
			"bedrag":          req.BedragExcl,
			"tegenRekening":   req.TegenRekeningId,
			"bedragExclusief": req.BedragExcl,
			"bedragInclusief": req.BedragIncl,
			"btwCode":         req.BTWCode,
			"btw":             req.BTWBedrag,
		}},
	})
	return invoiceMutPayload
}

// mainSubmitFullPaymentPayload is main's SubmitFull step 3 (soort 4).
func mainSubmitFullPaymentPayload(req mainSubmitFullReq, bankAccountId int, bankLineOmschrijving string) []byte {
	paymentOmschrijving := bankLineOmschrijving
	if paymentOmschrijving == "" {
		paymentOmschrijving = "Betaling " + req.Factuurnummer + " " + req.Leverancier
	}
	paymentPayload, _ := json.Marshal(map[string]any{
		"mutatie": map[string]any{
			"rekening":     bankAccountId,
			"datum":        req.Datum,
			"soort":        4,
			"omschrijving": mainTruncate(paymentOmschrijving, 200),
		},
		"mutatieRegels": []map[string]any{{
			"index":     0,
			"bedrag":    req.BedragIncl,
			"btw":       0,
			"btwCode":   "GEEN",
			"relatieId": req.RelatieId,
			"factuur":   req.Factuurnummer,
		}},
		"importId": req.ImportId,
	})
	return paymentPayload
}

type mainExtraLine struct {
	BedragExcl, BedragIncl, BTWBedrag float64
	BTWCode                           string
	TegenRekeningId                   int
	Omschrijving                      string
}

type mainReceiptReq struct {
	Datum, Leverancier, Omschrijving  string
	BedragExcl, BedragIncl, BTWBedrag float64
	BTWCode                           string
	TegenRekeningId, ImportId         int
	ExtraLines                        []mainExtraLine
}

// mainReceiptPayload is main's SubmitReceipt: the defaults applied to the
// request (btwCode, derived excl, extra-line defaults) and step 2 (soort 6).
func mainReceiptPayload(req mainReceiptReq, bankAccountId int) []byte {
	if req.BTWCode == "" {
		req.BTWCode = "HOOG_INK_21"
	}
	if req.BedragExcl == 0 && req.BedragIncl > 0 {
		req.BedragExcl = req.BedragIncl - req.BTWBedrag
	}
	for i := range req.ExtraLines {
		el := &req.ExtraLines[i]
		if el.TegenRekeningId == 0 {
			el.TegenRekeningId = req.TegenRekeningId
		}
		if el.BTWCode == "" {
			el.BTWCode = "GEEN"
		}
		if el.BedragExcl == 0 {
			el.BedragExcl = el.BedragIncl - el.BTWBedrag
		}
	}
	desc := req.Omschrijving
	if req.Leverancier != "" {
		if desc == "" {
			desc = req.Leverancier
		} else {
			desc = req.Leverancier + " - " + desc
		}
	}
	regels := []map[string]any{{
		"index":           0,
		"bedrag":          req.BedragExcl,
		"bedragExclusief": req.BedragExcl,
		"bedragInclusief": req.BedragIncl,
		"btw":             req.BTWBedrag,
		"btwCode":         req.BTWCode,
		"tegenRekening":   req.TegenRekeningId,
	}}
	for i, el := range req.ExtraLines {
		regel := map[string]any{
			"index":           i + 1,
			"bedrag":          el.BedragExcl,
			"bedragExclusief": el.BedragExcl,
			"bedragInclusief": el.BedragIncl,
			"btw":             el.BTWBedrag,
			"btwCode":         el.BTWCode,
			"tegenRekening":   el.TegenRekeningId,
		}
		if el.Omschrijving != "" {
			regel["omschrijving"] = mainTruncate(el.Omschrijving, 200)
		}
		regels = append(regels, regel)
	}
	mutPayload, _ := json.Marshal(map[string]any{
		"mutatie": map[string]any{
			"rekening":     bankAccountId,
			"datum":        req.Datum,
			"soort":        6,
			"inEx":         "EX",
			"omschrijving": mainTruncate(desc, 200),
		},
		"mutatieRegels": regels,
		"importId":      mainNilIfZero(req.ImportId),
	})
	return mutPayload
}

// mainLinkPayload is main's file-link body (SubmitFull step 4, SubmitReceipt step 3).
func mainLinkPayload(fileID, mutNr int) []byte {
	linkPayload, _ := json.Marshal(map[string]any{
		"koppelId":   mutNr,
		"folders":    []map[string]any{{"id": fileID, "soort": "F"}},
		"koppelType": "MUT",
	})
	return linkPayload
}

// mainMatchedBankLine is main's bank-line match in Analyze: the first
// outgoing line in grid order within €0,02 (EUR) or within the FX tolerance.
func mainMatchedBankLine(invoice *claude.InvoiceData, rows []map[string]any) map[string]any {
	if invoice.BedragInclBTW <= 0 {
		return nil
	}
	eurEstimate, fxConverted := convertToEUR(invoice.BedragInclBTW, invoice.Currency)
	for _, row := range rows {
		bedrag, _ := toFloat(row["mutBedrag"])
		if bedrag >= 0 {
			continue
		}
		absBedrag := abs(bedrag)
		var hit bool
		if !fxConverted {
			hit = abs(absBedrag-invoice.BedragInclBTW) < 0.02
		} else {
			diff := abs(absBedrag - eurEstimate)
			hit = diff/eurEstimate < currencyMatchTolerance
		}
		if hit {
			id, _ := toInt(row["id"])
			return map[string]any{
				"id":                id,
				"datum":             toString(row["mutDatum"]),
				"bedrag":            bedrag,
				"omschrijving":      toString(row["mutOmschrijving"]),
				"currencyConverted": fxConverted,
				"invoiceCurrency":   strings.ToUpper(invoice.Currency),
				"invoiceAmount":     invoice.BedragInclBTW,
			}
		}
	}
	return nil
}

// --- pure payload parity -----------------------------------------------------

func TestBatchPayloadMatchesMainForEverySoortExceptFactuurbetaling(t *testing.T) {
	soorten := []int{1, 2, 5, 6, 7, 0, 9}
	btwCodes := []string{"HOOG_INK_21", "HOOG_VERK_21", "LAAG_INK_9", "LAAG_VERK_9", "GEEN", "VERL_INK", "VERL_INK_L9", "BU_EU_INK", "BI_EU_INK", ""}
	amounts := []float64{121, -121, 0.01, -0.01, 12.34, 45.67, 1234.565, 999.995, 100.005, 0, 60.5, 7.99}
	relaties := []struct {
		id      int
		factuur string
	}{{0, ""}, {4711, ""}, {0, "VB-2026-001"}, {4711, "VB-2026-001"}}
	omschrijvingen := []string{"Voorbeeld Leverancier - abonnement", ""}
	tegenRekeningen := []int{0, 4000}

	n := 0
	for _, soort := range soorten {
		for _, code := range btwCodes {
			for _, amount := range amounts {
				for _, rel := range relaties {
					for _, oms := range omschrijvingen {
						for _, tr := range tegenRekeningen {
							item := inboxBatchItem{
								ID: 9001, GrootboekId: 1300, Soort: soort, Grootboekcode: "4000",
								BTWCode: code, Omschrijving: oms, Bedrag: amount,
								RelatieId: rel.id, Factuurnummer: rel.factuur,
							}
							want := mainBatchPayload(item, "2026-10-01", tr)
							got, err := buildBatchMutatiePayload(item, "2026-10-01", tr)
							if err != nil {
								t.Fatalf("%+v: unexpected error %v", item, err)
							}
							gotJSON, _ := json.Marshal(got)
							if !bytes.Equal(gotJSON, want) {
								t.Fatalf("payload differs from main for %+v (tegenRekening %d)\n got  %s\n main %s", item, tr, gotJSON, want)
							}
							n++
						}
					}
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no cases ran")
	}
}

func TestMatchedBankLineMatchesMainWhenOneLineQualifies(t *testing.T) {
	row := func(id int, datum string, bedrag float64, oms string) map[string]any {
		return map[string]any{"id": float64(id), "mutDatum": datum, "mutBedrag": bedrag, "mutOmschrijving": oms}
	}
	cases := []struct {
		name    string
		invoice claude.InvoiceData
		rows    []map[string]any
	}{
		{
			name:    "EUR exact",
			invoice: claude.InvoiceData{Leverancier: "Voorbeeld Leverancier B.V.", Datum: "2026-10-01", BedragInclBTW: 121, Currency: "EUR", DocumentType: "factuur"},
			rows:    []map[string]any{row(1, "2026-10-03T00:00:00", 121, "incoming"), row(2, "2026-10-03T00:00:00", -121, "Voorbeeld Leverancier"), row(3, "2026-10-04T00:00:00", -50, "other")},
		},
		{
			name:    "EUR one cent off",
			invoice: claude.InvoiceData{Leverancier: "Voorbeeld Leverancier", Datum: "2026-10-01", BedragInclBTW: 59.99, Currency: "", DocumentType: "factuur"},
			rows:    []map[string]any{row(7, "2026-10-02T00:00:00", -60, "kaartbetaling")},
		},
		{
			name:    "receipt",
			invoice: claude.InvoiceData{Leverancier: "Voorbeeld Bakkerij", Datum: "2026-10-05", BedragInclBTW: 12.5, Currency: "EUR", DocumentType: "bonnetje", IsReceipt: true},
			rows:    []map[string]any{row(8, "2026-10-05T00:00:00", -12.5, "Voorbeeld Bakkerij pinbetaling")},
		},
		{
			name:    "CHF via FX",
			invoice: claude.InvoiceData{Leverancier: "Voorbeeld Laadpaal AG", Datum: "2026-10-01", BedragInclBTW: 32.8, Currency: "chf", DocumentType: "factuur"},
			rows:    []map[string]any{row(9, "2026-10-02T00:00:00", -34.9, "Voorbeeld Laadpaal")},
		},
		{
			name:    "no qualifying line",
			invoice: claude.InvoiceData{Leverancier: "Voorbeeld Leverancier", Datum: "2026-10-01", BedragInclBTW: 99, Currency: "EUR", DocumentType: "factuur"},
			rows:    []map[string]any{row(10, "2026-10-02T00:00:00", -98.5, "x"), row(11, "2026-10-02T00:00:00", 99, "incoming")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := tc.invoice
			want := mainMatchedBankLine(&inv, tc.rows)
			var got map[string]any
			if c := rankBankLineMatches(&inv, tc.rows, nil); len(c) > 0 {
				got = c[0].legacyJSON()
			}
			wantJSON, _ := json.Marshal(want)
			gotJSON, _ := json.Marshal(got)
			if !bytes.Equal(gotJSON, wantJSON) {
				t.Errorf("matchedBankLine differs from main\n got  %s\n main %s", gotJSON, wantJSON)
			}
		})
	}
}

// --- handler parity against a fake e-boekhouden ------------------------------

// parityFake is a minimal secure20 stand-in for the existing booking flows:
// ledger accounts, the import grid, mutations, file lists, upload and link.
// It records the bodies of every write.
type parityFake struct {
	t        *testing.T
	mu       sync.Mutex
	ledger   []map[string]any
	grid     [][]any
	files    map[int][]eboekhouden.ArchiveFile
	nextID   int
	nextMut  int
	mutaties []string
	uploads  []string
	links    []string
}

var parityFilelistRe = regexp.MustCompile(`^/v1/api/folder/(\d+)/filelist$`)

func newParityFake(t *testing.T) (*parityFake, *eboekhouden.Client) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := &parityFake{
		t: t,
		ledger: []map[string]any{
			{"id": 1300, "code": "1100", "omschrijving": "Bank", "rekeningCategorie": "BANK"},
			{"id": 1310, "code": "1000", "omschrijving": "Kas", "rekeningCategorie": "KAS"},
			{"id": 1600, "code": "1600", "omschrijving": "Crediteuren", "rekeningCategorie": "CRED"},
			{"id": 4000, "code": "4000", "omschrijving": "Voorbeeldkosten", "rekeningCategorie": "KOST"},
			{"id": 4100, "code": "4100", "omschrijving": "Representatie", "rekeningCategorie": "KOST"},
			{"id": 8000, "code": "8000", "omschrijving": "Omzet", "rekeningCategorie": "OMZ"},
		},
		// Columns as ParseImportGrid reads them: id, datum, rekening, mutDatum,
		// mutBedrag, mutOmschrijving, mutFactuur, grootboekId, opmerking,
		// hasFiles, verwerkFailureReason.
		grid: [][]any{
			{101, "2026-10-01T00:00:00", "NL00TEST0000000001", "2026-10-01T00:00:00", -60.5, "Voorbeeld Leverancier abonnement oktober", "", 1300, "", false, ""},
			{102, "2026-10-02T00:00:00", "NL00TEST0000000002", "2026-10-02T00:00:00", 250.0, "Voorbeeld Klant betaling VB-2026-001", "", 1300, "", false, ""},
			{103, "2026-10-03T00:00:00", "NL00TEST0000000003", "2026-10-03T00:00:00", -121.0, "", "", 1300, "", false, ""},
			{104, "2026-10-04T00:00:00", "NL00TEST0000000004", "2026-10-04T00:00:00", -45.0, "Voorbeeld Eetcafe pinbetaling", "", 1310, "", false, ""},
		},
		files:   map[int][]eboekhouden.ArchiveFile{},
		nextID:  700,
		nextMut: 5000,
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	client, err := eboekhouden.NewClientForTesting(srv.URL, "parity-token")
	if err != nil {
		t.Fatal(err)
	}
	return f, client
}

func (f *parityFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	buf.ReadFrom(r.Body)
	body := buf.String()
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	write := func(v any) {
		b, _ := json.Marshal(v)
		w.Write(b)
	}
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/v1/api/grootboekrekening/selectlist/actieveGBR":
		write(f.ledger)
	case r.Method == http.MethodGet && path == "/v1/api/import/gridtable":
		write(map[string]any{"rowCount": len(f.grid), "data": f.grid})
	case r.Method == http.MethodPost && path == "/v1/api/mutatie":
		f.mutaties = append(f.mutaties, body)
		f.nextMut++
		write(map[string]any{"mutNr": f.nextMut, "mutId": f.nextMut + 1000})
	case r.Method == http.MethodGet && parityFilelistRe.MatchString(path):
		id, _ := strconv.Atoi(parityFilelistRe.FindStringSubmatch(path)[1])
		files := f.files[id]
		if files == nil {
			files = []eboekhouden.ArchiveFile{}
		}
		write(files)
	case r.Method == http.MethodPost && path == "/v1/api/folder/upload":
		f.uploads = append(f.uploads, body)
		var req struct {
			FileName string `json:"fileName"`
			FolderID int    `json:"folderId"`
		}
		json.Unmarshal([]byte(body), &req)
		f.nextID++
		f.files[req.FolderID] = append(f.files[req.FolderID], eboekhouden.ArchiveFile{
			ID: f.nextID, ParentID: req.FolderID, Soort: "F", Naam: req.FileName, Datum: "2026-10-24T12:00:00", Size: 10,
		})
		write(req.FolderID)
	case r.Method == http.MethodPost && path == "/v1/api/folderkoppel/koppel/item":
		f.links = append(f.links, body)
		write(map[string]any{})
	default:
		f.t.Errorf("parity fake: unexpected %s %s", r.Method, path)
		w.WriteHeader(http.StatusNotFound)
	}
}

// callWithClient runs handler with body as JSON, the e-boekhouden client in
// the context and, when withSession, a Speedy session. It returns the status
// and the raw response body.
func callWithClient(t *testing.T, handler gin.HandlerFunc, client *eboekhouden.Client, withSession bool, body any) (int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	c.Request.Header.Set("Content-Type", "application/json")
	if withSession {
		c.Set(session.ContextKey, &session.SessionData{UserID: "parity-user"})
	}
	c.Set(session.ClientContextKey, client)
	handler(c)
	return w.Code, w.Body.String()
}

func TestBatchProcessSendsMainPayloads(t *testing.T) {
	fake, client := newParityFake(t)
	h := &InboxHandler{} // no session below, so no Redis or database is touched

	// What the frontend sends (toInboxProcessItem: bedrag is absolute).
	items := []inboxBatchItem{
		{ID: 101, GrootboekId: 1300, Soort: 6, Grootboekcode: "4000", BTWCode: "HOOG_INK_21", Omschrijving: "Voorbeeld Leverancier abonnement", Bedrag: 60.5},
		{ID: 102, GrootboekId: 1300, Soort: 5, Grootboekcode: "8000", BTWCode: "HOOG_INK_21", Omschrijving: "Voorbeeld Klant", Bedrag: 250},
		{ID: 103, GrootboekId: 1300, Soort: 6, Grootboekcode: "4000", BTWCode: "LAAG_VERK_9", Omschrijving: "", Bedrag: 121, RelatieId: 4711},
		{ID: 104, GrootboekId: 1310, Soort: 1, Grootboekcode: "4100", BTWCode: "GEEN", Omschrijving: "Voorbeeld Eetcafe", Bedrag: 45, Factuurnummer: "VB-2026-002"},
		// Not in the grid (date falls back to today) and an unknown code (tegenRekening 0).
		{ID: 199, GrootboekId: 1300, Soort: 7, Grootboekcode: "9999", BTWCode: "", Omschrijving: "Memoriaal", Bedrag: 10},
	}
	status, body := callWithClient(t, h.BatchProcess, client, false, map[string]any{"items": items})
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, body)
	}

	today := time.Now().Format("2006-01-02")
	wantDates := []string{"2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04", today}
	wantTegen := []int{4000, 8000, 4000, 4100, 0}
	if len(fake.mutaties) != len(items) {
		t.Fatalf("got %d mutaties, want %d", len(fake.mutaties), len(items))
	}
	for i, item := range items {
		want := string(mainBatchPayload(item, wantDates[i], wantTegen[i]))
		if fake.mutaties[i] != want {
			t.Errorf("item %d payload differs from main\n got  %s\n main %s", item.ID, fake.mutaties[i], want)
		}
	}

	wantResp := `{"results":[` +
		`{"id":101,"status":"ok","mutNr":"5001"},` +
		`{"id":102,"status":"ok","mutNr":"5002"},` +
		`{"id":103,"status":"ok","mutNr":"5003"},` +
		`{"id":104,"status":"ok","mutNr":"5004"},` +
		`{"id":199,"status":"ok","mutNr":"5005"}]}`
	if body != wantResp {
		t.Errorf("response = %s\nwant       %s", body, wantResp)
	}
}

// pdfFixture is the start of a PDF: enough for content sniffing.
var pdfFixture = []byte("%PDF-1.4\n% Voorbeeld factuur\n1 0 obj\n<< >>\nendobj\n")

// assertMainKeys checks that every key main returned is present with main's
// value, and that any extra key is one the branch documents.
func assertMainKeys(t *testing.T, body string, want map[string]any, allowedExtra ...string) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("response is not JSON: %s", body)
	}
	for k, v := range want {
		if fmt.Sprint(got[k]) != fmt.Sprint(v) {
			t.Errorf("response[%q] = %v, want %v (main)", k, got[k], v)
		}
	}
	allowed := map[string]bool{}
	for _, k := range allowedExtra {
		allowed[k] = true
	}
	for k := range got {
		if _, ok := want[k]; !ok && !allowed[k] {
			t.Errorf("unexpected response key %q = %v", k, got[k])
		}
	}
}

var archiveResponseKeys = []string{"renamed", "moved", "archiveName", "targetFolderId", "targetFolderPath", "archiveWarning"}

func TestSubmitFullSendsMainPayloads(t *testing.T) {
	cases := []struct {
		name     string
		req      map[string]any
		main     mainSubmitFullReq
		bankAcc  int
		bankOms  string
		upload   bool
		wantMain map[string]any
	}{
		{
			name: "upload into a picked folder, paid by a bank line",
			req: map[string]any{
				"datum": "2026-10-01", "leverancier": "Voorbeeld Leverancier B.V.", "factuurnummer": "VB-2026-001",
				"omschrijving": "Abonnement oktober", "bedragExcl": 50.0, "bedragIncl": 60.5, "btwBedrag": 10.5,
				"btwCode": "HOOG_INK_21", "inEx": "EX", "relatieId": 4711, "tegenRekeningId": 4000, "rekeningId": 1600,
				"pdfBase64": base64.StdEncoding.EncodeToString(pdfFixture), "filename": "voorbeeld.pdf",
				"folderId": 900, "importId": 101, "pdfHash": "abc",
			},
			main: mainSubmitFullReq{
				Datum: "2026-10-01", Leverancier: "Voorbeeld Leverancier B.V.", Factuurnummer: "VB-2026-001",
				Omschrijving: "Abonnement oktober", BedragExcl: 50, BedragIncl: 60.5, BTWBedrag: 10.5,
				BTWCode: "HOOG_INK_21", InEx: "EX", RelatieId: 4711, TegenRekeningId: 4000, RekeningId: 1600, ImportId: 101,
			},
			bankAcc: 1300, bankOms: "Voorbeeld Leverancier abonnement oktober", upload: true,
			wantMain: map[string]any{"mutNr": 5001, "mutId": 6001, "paymentMutNr": 5002, "archived": true, "linked": true},
		},
		{
			name: "defaults: crediteuren looked up, inEx EX, btwCode HOOG_INK_21; empty bank description",
			req: map[string]any{
				"datum": "2026-10-03", "leverancier": "Voorbeeld Leverancier", "factuurnummer": "VB-2026-003",
				"omschrijving": strings.Repeat("Lange omschrijving ", 15), "bedragExcl": 100.0, "bedragIncl": 121.0, "btwBedrag": 21.0,
				"relatieId": 4711, "tegenRekeningId": 4000, "importId": 103,
			},
			main: mainSubmitFullReq{
				Datum: "2026-10-03", Leverancier: "Voorbeeld Leverancier", Factuurnummer: "VB-2026-003",
				Omschrijving: strings.Repeat("Lange omschrijving ", 15), BedragExcl: 100, BedragIncl: 121, BTWBedrag: 21,
				BTWCode: "HOOG_INK_21", InEx: "EX", RelatieId: 4711, TegenRekeningId: 4000, RekeningId: 1600, ImportId: 103,
			},
			bankAcc: 1300, bankOms: "",
			wantMain: map[string]any{"mutNr": 5001, "mutId": 6001, "paymentMutNr": 5002, "archived": false, "linked": false},
		},
		{
			name: "no bank line, no document",
			req: map[string]any{
				"datum": "2026-10-05", "leverancier": "Voorbeeld Leverancier", "factuurnummer": "VB-2026-005",
				"omschrijving": "Open post", "bedragExcl": 10.0, "bedragIncl": 10.9, "btwBedrag": 0.9,
				"btwCode": "LAAG_INK_9", "inEx": "IN", "relatieId": 4711, "tegenRekeningId": 4000, "rekeningId": 1600,
			},
			main: mainSubmitFullReq{
				Datum: "2026-10-05", Leverancier: "Voorbeeld Leverancier", Factuurnummer: "VB-2026-005",
				Omschrijving: "Open post", BedragExcl: 10, BedragIncl: 10.9, BTWBedrag: 0.9,
				BTWCode: "LAAG_INK_9", InEx: "IN", RelatieId: 4711, TegenRekeningId: 4000, RekeningId: 1600,
			},
			wantMain: map[string]any{"mutNr": 5001, "mutId": 6001, "paymentMutNr": 0, "archived": false, "linked": false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake, client := newParityFake(t)
			h := &InvoiceHandler{}
			status, body := callWithClient(t, h.SubmitFull, client, true, tc.req)
			if status != http.StatusOK {
				t.Fatalf("status %d: %s", status, body)
			}

			wantMuts := []string{string(mainSubmitFullInvoicePayload(tc.main))}
			if tc.main.ImportId > 0 {
				wantMuts = append(wantMuts, string(mainSubmitFullPaymentPayload(tc.main, tc.bankAcc, tc.bankOms)))
			}
			if len(fake.mutaties) != len(wantMuts) {
				t.Fatalf("got %d mutaties %v, want %d", len(fake.mutaties), fake.mutaties, len(wantMuts))
			}
			for i := range wantMuts {
				if fake.mutaties[i] != wantMuts[i] {
					t.Errorf("mutatie %d differs from main\n got  %s\n main %s", i, fake.mutaties[i], wantMuts[i])
				}
			}

			assertMainKeys(t, body, tc.wantMain, archiveResponseKeys...)

			if !tc.upload {
				if len(fake.uploads) != 0 || len(fake.links) != 0 {
					t.Errorf("uploads %v links %v, want none", fake.uploads, fake.links)
				}
				return
			}
			assertMainUpload(t, fake, body, 900)
		})
	}
}

// assertMainUpload checks the archive upload and link of a booking against
// main: same upload fields except fileName (the naming convention replaced
// sanitizeFilename on purpose), and main's link body for the new file.
func assertMainUpload(t *testing.T, fake *parityFake, body string, folderID int) {
	t.Helper()
	if len(fake.uploads) != 1 {
		t.Fatalf("uploads = %v, want 1", fake.uploads)
	}
	var up map[string]any
	json.Unmarshal([]byte(fake.uploads[0]), &up)
	if up["data"] != base64.StdEncoding.EncodeToString(pdfFixture) || up["overwrite"] != false || up["folderId"] != float64(folderID) || len(up) != 4 {
		t.Errorf("upload payload = %v", up)
	}
	var resp map[string]any
	json.Unmarshal([]byte(body), &resp)
	if name, _ := up["fileName"].(string); name == "" || name != resp["archiveName"] {
		t.Errorf("upload fileName %q, response archiveName %v", up["fileName"], resp["archiveName"])
	}
	if len(fake.links) != 1 || fake.links[0] != string(mainLinkPayload(701, 5001)) {
		t.Errorf("links = %v, want main's %s", fake.links, mainLinkPayload(701, 5001))
	}
}

func TestSubmitReceiptSendsMainPayload(t *testing.T) {
	cases := []struct {
		name     string
		req      map[string]any
		main     mainReceiptReq
		bankAcc  int
		upload   bool
		wantMain map[string]any
	}{
		{
			name: "bank line, tip split, uploaded into a picked folder",
			req: map[string]any{
				"datum": "2026-10-04", "leverancier": "Voorbeeld Eetcafe", "omschrijving": "Lunch",
				"bedragExcl": 36.7, "bedragIncl": 40.0, "btwBedrag": 3.3, "btwCode": "LAAG_INK_9", "tegenRekeningId": 4100,
				"pdfBase64": base64.StdEncoding.EncodeToString(pdfFixture), "folderId": 900, "importId": 104,
				"extraLines": []map[string]any{{"bedragIncl": 5.0, "omschrijving": "Fooi"}},
			},
			main: mainReceiptReq{
				Datum: "2026-10-04", Leverancier: "Voorbeeld Eetcafe", Omschrijving: "Lunch",
				BedragExcl: 36.7, BedragIncl: 40, BTWBedrag: 3.3, BTWCode: "LAAG_INK_9", TegenRekeningId: 4100, ImportId: 104,
				ExtraLines: []mainExtraLine{{BedragIncl: 5, Omschrijving: "Fooi"}},
			},
			bankAcc: 1310, upload: true,
			wantMain: map[string]any{"mutNr": 5001, "mutId": 6001, "archived": true, "linked": true},
		},
		{
			name: "explicit bank account, derived excl, default BTW, no description",
			req: map[string]any{
				"datum": "2026-10-06", "leverancier": "", "omschrijving": "",
				"bedragIncl": 12.1, "btwBedrag": 2.1, "tegenRekeningId": 4000, "bankAccountId": 1300,
				"extraLines": []map[string]any{{"bedragIncl": 1.0, "btwCode": "GEEN", "tegenRekeningId": 4100}, {"bedragIncl": 2.18, "btwBedrag": 0.18, "btwCode": "LAAG_INK_9"}},
			},
			main: mainReceiptReq{
				Datum: "2026-10-06", BedragIncl: 12.1, BTWBedrag: 2.1, TegenRekeningId: 4000,
				ExtraLines: []mainExtraLine{{BedragIncl: 1, BTWCode: "GEEN", TegenRekeningId: 4100}, {BedragIncl: 2.18, BTWBedrag: 0.18, BTWCode: "LAAG_INK_9"}},
			},
			bankAcc:  1300,
			wantMain: map[string]any{"mutNr": 5001, "mutId": 6001, "archived": false, "linked": false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake, client := newParityFake(t)
			h := &InvoiceHandler{}
			status, body := callWithClient(t, h.SubmitReceipt, client, true, tc.req)
			if status != http.StatusOK {
				t.Fatalf("status %d: %s", status, body)
			}
			want := string(mainReceiptPayload(tc.main, tc.bankAcc))
			if len(fake.mutaties) != 1 || fake.mutaties[0] != want {
				t.Errorf("mutatie differs from main\n got  %v\n main %s", fake.mutaties, want)
			}
			assertMainKeys(t, body, tc.wantMain, archiveResponseKeys...)
			if tc.upload {
				assertMainUpload(t, fake, body, 900)
			} else if len(fake.uploads) != 0 || len(fake.links) != 0 {
				t.Errorf("uploads %v links %v, want none", fake.uploads, fake.links)
			}
		})
	}
}

// Guard against the reference drifting into a no-op: main's rounding helper
// must still produce the split the payload tests rely on.
func TestMainReferenceRounding(t *testing.T) {
	if got := mainRoundCents(121 * 21 / 121.0); got != 21 {
		t.Errorf("mainRoundCents = %v", got)
	}
	if math.Abs(mainRoundCents(-0.005)+0.01) > 1e-9 {
		t.Errorf("mainRoundCents(-0.005) = %v, want -0.01", mainRoundCents(-0.005))
	}
}
