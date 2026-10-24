package eboekhouden

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// openPostGridFixture mirrors the column layout of the real open-post grid
// (33 columns, resolved through colMetadata) with fictitious data. The second
// row is partially paid and has a null description.
const openPostGridFixture = `{
  "dataType": "openpost",
  "colMetadata": [
    {"name":"mutDatum","index":0,"readerIndex":0},
    {"name":"sBedrag","index":1,"readerIndex":1,"total":171.5},
    {"name":"cnt","index":2,"readerIndex":2},
    {"name":"mutFactuur","index":3,"readerIndex":3},
    {"name":"mutTermijn","index":4,"readerIndex":4},
    {"name":"mutNr","index":5,"readerIndex":5},
    {"name":"relatieCode","index":6,"readerIndex":6},
    {"name":"relatieId","index":7,"readerIndex":7},
    {"name":"bedrijf","index":8,"readerIndex":8},
    {"name":"adres","index":9,"readerIndex":9},
    {"name":"postcode","index":10,"readerIndex":10},
    {"name":"plaats","index":11,"readerIndex":11},
    {"name":"aanhef","index":12,"readerIndex":12},
    {"name":"telefoon","index":13,"readerIndex":13},
    {"name":"email","index":14,"readerIndex":14},
    {"name":"gSM","index":15,"readerIndex":15},
    {"name":"sBetaald","index":16,"readerIndex":16,"total":20.0},
    {"name":"teOntvangen","index":17,"readerIndex":17,"total":151.5},
    {"name":"vervaldatum","index":18,"readerIndex":18},
    {"name":"days30","index":19,"readerIndex":19,"total":30.5},
    {"name":"days60","index":20,"readerIndex":20,"total":0},
    {"name":"days90","index":21,"readerIndex":21,"total":0},
    {"name":"lastCor","index":22,"readerIndex":22},
    {"name":"mutOmschrijving","index":23,"readerIndex":23},
    {"name":"corOmschrijving","index":24,"readerIndex":24},
    {"name":"lastCorDatum","index":25,"readerIndex":25},
    {"name":"uitstelDatum","index":26,"readerIndex":26},
    {"name":"uitstelReden","index":27,"readerIndex":27},
    {"name":"fact_ID","index":28,"readerIndex":28},
    {"name":"lastBeDatum","index":29,"readerIndex":29},
    {"name":"datumFollowUp","index":30,"readerIndex":30},
    {"name":"mutSoort","index":31,"readerIndex":31},
    {"name":"followUpId","index":32,"readerIndex":32}
  ],
  "rowCount": 2,
  "sortedBy": "",
  "limit": 500,
  "offset": 0,
  "sortDirection": 1,
  "data": [
    ["2026-10-12T00:00:00", 121.0, 1, "INV-0042", 0, 123456, "L0001", 4711, "Voorbeeld Leverancier B.V.", "Voorbeeldstraat 1", "1234 AB", "Voorbeeldstad", "", "", "", "", 0.0, 121.0, "2026-11-11T00:00:00", null, null, null, null, "Factuur INV-0042", null, null, null, null, 98765, null, null, 1, null],
    ["2026-09-30T00:00:00", 50.5, 1, "2026/0042", 0, 123457, "L0002", 4712, "Testbedrijf Fictief", "", "", "", "", "", "", "", 20.0, 30.5, "2026-10-30T00:00:00", 30.5, null, null, null, null, null, null, null, null, 98766, null, null, 1, null]
  ]
}`

func TestParseOpenPostGrid(t *testing.T) {
	posts, err := ParseOpenPostGrid(json.RawMessage(openPostGridFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []OpenPost{
		{
			MutDatum:        "2026-10-12T00:00:00",
			SBedrag:         121,
			MutFactuur:      "INV-0042",
			MutNr:           123456,
			RelatieCode:     "L0001",
			RelatieID:       4711,
			Bedrijf:         "Voorbeeld Leverancier B.V.",
			SBetaald:        0,
			TeOntvangen:     121,
			Vervaldatum:     "2026-11-11T00:00:00",
			MutOmschrijving: "Factuur INV-0042",
			MutSoort:        1,
		},
		{
			MutDatum:        "2026-09-30T00:00:00",
			SBedrag:         50.5,
			MutFactuur:      "2026/0042",
			MutNr:           123457,
			RelatieCode:     "L0002",
			RelatieID:       4712,
			Bedrijf:         "Testbedrijf Fictief",
			SBetaald:        20,
			TeOntvangen:     30.5,
			Vervaldatum:     "2026-10-30T00:00:00",
			MutOmschrijving: "",
			MutSoort:        1,
		},
	}
	if !reflect.DeepEqual(posts, want) {
		t.Errorf("posts = %+v\nwant   %+v", posts, want)
	}
}

func TestParseOpenPostGridMapsByColumnName(t *testing.T) {
	// A grid with fewer columns in a different order must still map correctly:
	// the parser resolves names through colMetadata, not fixed positions.
	raw := `{"colMetadata":[{"name":"teOntvangen","index":0},{"name":"bedrijf","index":1},{"name":"mutNr","index":2}],` +
		`"rowCount":1,"data":[[12.5,"Voorbeeld Leverancier B.V.",7]]}`
	posts, err := ParseOpenPostGrid(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []OpenPost{{TeOntvangen: 12.5, Bedrijf: "Voorbeeld Leverancier B.V.", MutNr: 7}}
	if !reflect.DeepEqual(posts, want) {
		t.Errorf("posts = %+v, want %+v", posts, want)
	}
}

func TestParseOpenPostGridShortRow(t *testing.T) {
	// A row with fewer cells than colMetadata announces yields zero values
	// instead of a panic.
	raw := `{"colMetadata":[{"name":"bedrijf","index":0},{"name":"mutNr","index":5}],"rowCount":1,"data":[["Testbedrijf Fictief"]]}`
	posts, err := ParseOpenPostGrid(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []OpenPost{{Bedrijf: "Testbedrijf Fictief"}}
	if !reflect.DeepEqual(posts, want) {
		t.Errorf("posts = %+v, want %+v", posts, want)
	}
}

func TestParseOpenPostGridEdgeCases(t *testing.T) {
	t.Run("empty grid", func(t *testing.T) {
		posts, err := ParseOpenPostGrid(json.RawMessage(`{"colMetadata":[],"rowCount":0,"data":[]}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if posts == nil || len(posts) != 0 {
			t.Errorf("posts = %#v, want empty non-nil slice", posts)
		}
	})

	t.Run("data without column metadata", func(t *testing.T) {
		_, err := ParseOpenPostGrid(json.RawMessage(`{"rowCount":1,"data":[[1,2,3]]}`))
		if err == nil || !strings.Contains(err.Error(), "no column metadata") {
			t.Fatalf("err = %v, want no column metadata error", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		if _, err := ParseOpenPostGrid(json.RawMessage(`not json`)); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestGetOpenPostGrid(t *testing.T) {
	tests := []struct {
		soort    string
		wantPath string
	}{
		{OpenPostCrediteur, "/v1/api/openpost/gridtable/openpost?offset=0&limit=500&orderBy=&sortDirection=1&soort=Crediteur"},
		{OpenPostDebiteur, "/v1/api/openpost/gridtable/openpost?offset=0&limit=500&orderBy=&sortDirection=1&soort=Debiteur"},
	}
	for _, tc := range tests {
		t.Run(tc.soort, func(t *testing.T) {
			var last recordedRequest
			srv := jsonServer(t, 200, openPostGridFixture, &last)
			c := newTestClient(t, srv)

			raw, err := c.GetOpenPostGrid(tc.soort)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertRequest(t, last, "GET", tc.wantPath, "")
			if string(raw) != openPostGridFixture {
				t.Errorf("raw response was altered")
			}
			posts, err := ParseOpenPostGrid(raw)
			if err != nil {
				t.Fatalf("parsing fetched grid: %v", err)
			}
			if len(posts) != 2 {
				t.Errorf("len(posts) = %d, want 2", len(posts))
			}
		})
	}

	t.Run("invalid soort makes no request", func(t *testing.T) {
		var last recordedRequest
		srv := jsonServer(t, 500, `must not be called`, &last)
		c := newTestClient(t, srv)

		if _, err := c.GetOpenPostGrid("Leverancier"); err == nil {
			t.Fatal("expected an error for an invalid soort")
		}
		if last.Method != "" {
			t.Errorf("a request was sent: %+v", last)
		}
	})
}
