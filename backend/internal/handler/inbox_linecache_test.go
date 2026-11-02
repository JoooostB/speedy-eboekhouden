package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
	"github.com/redis/go-redis/v9"
)

// fakeLineCache is an in-memory lineCacheStore: one hash with a TTL, and a
// transaction that only applies on Exec.
type fakeLineCache struct {
	mu       sync.Mutex
	key      string
	hash     map[string]string
	ttl      time.Duration
	failRead bool
	hmgets   int
}

func newFakeLineCache() *fakeLineCache {
	return &fakeLineCache{hash: map[string]string{}}
}

func (f *fakeLineCache) HMGet(_ context.Context, key string, fields ...string) *redis.SliceCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hmgets++
	if f.failRead {
		return redis.NewSliceResult(nil, errors.New("read refused"))
	}
	out := make([]any, len(fields))
	if key == f.key {
		for i, field := range fields {
			if v, ok := f.hash[field]; ok {
				out[i] = v
			}
		}
	}
	return redis.NewSliceResult(out, nil)
}

func (f *fakeLineCache) TxPipeline() redis.Pipeliner {
	return &fakeLinePipeline{store: f}
}

// fakeLinePipeline records DEL, HSET and EXPIRE and applies them on Exec.
// The embedded Pipeliner is nil: any other command panics, which is what a
// test wants to hear.
type fakeLinePipeline struct {
	redis.Pipeliner
	store *fakeLineCache
	ops   []func()
}

func (p *fakeLinePipeline) Del(_ context.Context, keys ...string) *redis.IntCmd {
	p.ops = append(p.ops, func() {
		for _, k := range keys {
			if k == p.store.key {
				p.store.hash = map[string]string{}
				p.store.ttl = 0
			}
		}
	})
	return redis.NewIntResult(1, nil)
}

func (p *fakeLinePipeline) HSet(_ context.Context, key string, values ...any) *redis.IntCmd {
	p.ops = append(p.ops, func() {
		p.store.key = key
		for _, v := range values {
			for field, value := range v.(map[string]string) {
				p.store.hash[field] = value
			}
		}
	})
	return redis.NewIntResult(int64(len(values)), nil)
}

func (p *fakeLinePipeline) Expire(_ context.Context, key string, ttl time.Duration) *redis.BoolCmd {
	p.ops = append(p.ops, func() {
		if key == p.store.key {
			p.store.ttl = ttl
		}
	})
	return redis.NewBoolResult(true, nil)
}

func (p *fakeLinePipeline) Exec(_ context.Context) ([]redis.Cmder, error) {
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	for _, op := range p.ops {
		op()
	}
	return nil, nil
}

func (f *fakeLineCache) size() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.hash)
}

var lineCacheLines = []claude.BatchLine{
	{ID: 1, Datum: "2026-10-20", Bedrag: -12.5, Omschrijving: "VOORBEELD SAAS", Tegenrekening: "NL00VOOR0000000001"},
	{ID: 2, Datum: "2026-10-21", Bedrag: -48, Omschrijving: "VOORBEELD TANKSTATION", Tegenrekening: "NL00VOOR0000000002"},
	{ID: 3, Datum: "2026-10-22", Bedrag: 1210, Omschrijving: "FACTUUR 2026-0042", Tegenrekening: "NL00VOOR0000000003"},
}

func lineCacheResult(id int, code string) claude.BatchClassifyResult {
	return claude.BatchClassifyResult{ID: id, Category: "invoice", NeedsInvoice: true, Confidence: 0.8, Grootboekcode: code, BTWCode: "HOOG_INK_21", Soort: "GeldUitgegeven", Omschrijving: "Voorbeeld", Indicator: "Abonnement"}
}

func TestInboxLineFingerprint(t *testing.T) {
	base := lineCacheLines[0]
	same := inboxLineFingerprint("BV", base)
	if same != inboxLineFingerprint("BV", base) {
		t.Fatal("fingerprint is not deterministic")
	}
	if len(same) != 64 {
		t.Errorf("fingerprint length = %d, want 64 hex characters", len(same))
	}
	cents := base
	cents.Bedrag = -12.50
	if inboxLineFingerprint("BV", cents) != same {
		t.Error("-12.5 and -12.50 must be the same line")
	}
	variants := map[string]claude.BatchLine{}
	variants["amount"] = base
	v := variants["amount"]
	v.Bedrag = -12.51
	variants["amount"] = v
	v = base
	v.Omschrijving = "VOORBEELD SAAS 2"
	variants["description"] = v
	v = base
	v.Tegenrekening = "NL00VOOR0000000009"
	variants["counterparty"] = v
	v = base
	v.ID = 99
	variants["id"] = v
	v = base
	v.Datum = "2026-10-21"
	variants["date"] = v
	for name, line := range variants {
		if inboxLineFingerprint("BV", line) == same {
			t.Errorf("a different %s must give a different fingerprint", name)
		}
	}
	if inboxLineFingerprint("ZZP", base) == same {
		t.Error("a different entity type must give a different fingerprint")
	}
	for _, line := range lineCacheLines {
		for _, s := range []string{line.Omschrijving, line.Tegenrekening} {
			if strings.Contains(strings.ToLower(inboxLineFingerprint("", line)), strings.ToLower(s)) {
				t.Errorf("fingerprint carries raw text %q", s)
			}
		}
	}
}

func TestInboxLineCacheKeyIsVersioned(t *testing.T) {
	if got, want := inboxLineCacheKey(testUserA), "inbox:line:v1:"+testUserA; got != want {
		t.Errorf("key = %q, want %q", got, want)
	}
}

func TestLineCacheStoreThenLoadSplitsHitsAndMisses(t *testing.T) {
	ctx := context.Background()
	store := newFakeLineCache()
	results := map[int]claude.BatchClassifyResult{1: lineCacheResult(1, "4500"), 2: lineCacheResult(2, "4320")}
	// Line 3 has no result (Claude left it out) and must not be stored.
	storeLineCache(ctx, store, testUserA, "BV", lineCacheLines, results)
	if store.size() != 2 {
		t.Fatalf("stored %d fields, want 2", store.size())
	}
	if store.ttl != inboxLineCacheTTL {
		t.Errorf("ttl = %v, want %v", store.ttl, inboxLineCacheTTL)
	}
	if store.key != inboxLineCacheKey(testUserA) {
		t.Errorf("key = %q", store.key)
	}
	for _, raw := range store.hash {
		var r claude.BatchClassifyResult
		if err := json.Unmarshal([]byte(raw), &r); err != nil || r.ID != 0 {
			t.Errorf("stored value %q should be a result without an id", raw)
		}
	}

	hits, misses := loadLineCache(ctx, store, testUserA, "BV", lineCacheLines)
	if len(hits) != 2 || len(misses) != 1 || misses[0].ID != 3 {
		t.Fatalf("hits = %v, misses = %v", hits, misses)
	}
	if hits[1].ID != 1 || hits[1].Grootboekcode != "4500" || hits[2].ID != 2 || hits[2].Grootboekcode != "4320" {
		t.Errorf("hits carry the wrong result: %v", hits)
	}

	// Another user, another entity type or a changed line: all misses.
	if h, m := loadLineCache(ctx, store, testUserB, "BV", lineCacheLines); len(h) != 0 || len(m) != 3 {
		t.Errorf("another user: hits = %v, misses = %d", h, len(m))
	}
	if h, m := loadLineCache(ctx, store, testUserA, "ZZP", lineCacheLines); len(h) != 0 || len(m) != 3 {
		t.Errorf("another entity type: hits = %v, misses = %d", h, len(m))
	}
	changed := []claude.BatchLine{lineCacheLines[0]}
	changed[0].Bedrag = -13
	if h, m := loadLineCache(ctx, store, testUserA, "BV", changed); len(h) != 0 || len(m) != 1 {
		t.Errorf("changed line: hits = %v, misses = %d", h, len(m))
	}
}

func TestLineCacheStorePrunesLinesThatAreGone(t *testing.T) {
	ctx := context.Background()
	store := newFakeLineCache()
	all := map[int]claude.BatchClassifyResult{1: lineCacheResult(1, "4500"), 2: lineCacheResult(2, "4320"), 3: lineCacheResult(3, "8000")}
	storeLineCache(ctx, store, testUserA, "", lineCacheLines, all)
	if store.size() != 3 {
		t.Fatalf("stored %d fields, want 3", store.size())
	}
	// Line 1 was booked: the next pass only has lines 2 and 3.
	storeLineCache(ctx, store, testUserA, "", lineCacheLines[1:], all)
	if store.size() != 2 {
		t.Fatalf("after pruning %d fields, want 2", store.size())
	}
	hits, misses := loadLineCache(ctx, store, testUserA, "", lineCacheLines)
	if len(misses) != 1 || misses[0].ID != 1 || len(hits) != 2 {
		t.Errorf("hits = %v, misses = %v", hits, misses)
	}
	// Nothing left: the hash is dropped rather than left empty.
	storeLineCache(ctx, store, testUserA, "", nil, all)
	if store.size() != 0 || store.ttl != 0 {
		t.Errorf("empty pass left %d fields with ttl %v", store.size(), store.ttl)
	}
}

func TestLineCacheWithoutRedisOrOnErrorMissesEverything(t *testing.T) {
	ctx := context.Background()
	hits, misses := loadLineCache(ctx, nil, testUserA, "", lineCacheLines)
	if len(hits) != 0 || len(misses) != len(lineCacheLines) {
		t.Errorf("nil store: hits = %v, misses = %d", hits, len(misses))
	}
	storeLineCache(ctx, nil, testUserA, "", lineCacheLines, map[int]claude.BatchClassifyResult{1: lineCacheResult(1, "4500")})

	store := newFakeLineCache()
	storeLineCache(ctx, store, testUserA, "", lineCacheLines, map[int]claude.BatchClassifyResult{1: lineCacheResult(1, "4500")})
	store.failRead = true
	hits, misses = loadLineCache(ctx, store, testUserA, "", lineCacheLines)
	if len(hits) != 0 || len(misses) != len(lineCacheLines) {
		t.Errorf("read error: hits = %v, misses = %d", hits, len(misses))
	}
	if h, _ := loadLineCache(ctx, store, testUserA, "", nil); len(h) != 0 || store.hmgets != 1 {
		t.Errorf("no lines must not touch Redis (hmgets = %d)", store.hmgets)
	}
}

// lineCacheFake plays e-boekhouden for classifyFresh: an import grid whose
// rows the test sets, no open posts. All data is fictitious.
type lineCacheFake struct {
	mu   sync.Mutex
	rows [][]any
}

func (f *lineCacheFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	write := func(v any) {
		b, _ := json.Marshal(v)
		w.Write(b)
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/api/openpost/gridtable/openpost":
		write(map[string]any{"colMetadata": []any{}, "data": [][]any{}})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/api/import/gridtable":
		write(map[string]any{"rowCount": len(f.rows), "data": f.rows})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func lineCacheRow(id int, bedrag float64, omschrijving string) []any {
	return []any{id, "2026-10-20T00:00:00", "NL00VOOR0000000001", "2026-10-20T00:00:00", bedrag, omschrijving, "", 1020, "", false, ""}
}

// gridLine is the BatchLine classifyFresh builds from lineCacheRow.
func gridLine(id int, bedrag float64, omschrijving string) claude.BatchLine {
	return claude.BatchLine{ID: id, Datum: "2026-10-20T00:00:00", Bedrag: bedrag, Omschrijving: omschrijving, Tegenrekening: "NL00VOOR0000000001"}
}

// runClassifyFresh calls classifyFresh for user u1 against fake and returns
// the base rows by id (nil when the handler wrote an error) and the
// recorder.
func runClassifyFresh(t *testing.T, h *InboxHandler, fake *lineCacheFake) (map[int]inboxClassifiedRow, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client, err := eboekhouden.NewClientForTesting(srv.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	sess := &session.SessionData{ID: "s1", UserID: "u1"}
	base, _, _, ok := h.classifyFresh(c, sess, client)
	if !ok {
		return nil, w
	}
	byID := make(map[int]inboxClassifiedRow)
	for _, row := range base.Classifications {
		byID[row.ID] = row
	}
	return byID, w
}

func TestClassifyFreshAsksClaudeOnlyForLinesItHasNotSeen(t *testing.T) {
	var sent [][]int
	orig := classifyWithClaude
	classifyWithClaude = func(_ *InboxHandler, _ *gin.Context, _ string, _ string, lines []claude.BatchLine) ([]claude.BatchClassifyResult, error) {
		var ids []int
		var out []claude.BatchClassifyResult
		for _, l := range lines {
			ids = append(ids, l.ID)
			out = append(out, lineCacheResult(l.ID, "4"+string(rune('0'+l.ID))+"00"))
		}
		sent = append(sent, ids)
		return out, nil
	}
	t.Cleanup(func() { classifyWithClaude = orig })

	store := newFakeLineCache()
	h := &InboxHandler{lineCache: store} // no database, no Anthropic key
	fake := &lineCacheFake{rows: [][]any{
		lineCacheRow(1, -12.5, "VOORBEELD SAAS"),
		lineCacheRow(2, -48, "VOORBEELD TANKSTATION"),
	}}

	// First pass: both lines are new.
	rows, w := runClassifyFresh(t, h, fake)
	if rows == nil {
		t.Fatalf("first pass failed: %d %s", w.Code, w.Body.String())
	}
	if len(sent) != 1 || len(sent[0]) != 2 {
		t.Fatalf("Claude saw %v, want both lines once", sent)
	}
	if rows[1].Grootboekcode != "4100" || rows[2].Grootboekcode != "4200" {
		t.Errorf("rows = %v", rows)
	}
	if store.size() != 2 {
		t.Fatalf("cache holds %d lines, want 2", store.size())
	}

	// Line 1 is booked; the second pass has only line 2 and must not reach
	// Claude at all, so it also needs no API key.
	fake.mu.Lock()
	fake.rows = [][]any{lineCacheRow(2, -48, "VOORBEELD TANKSTATION")}
	fake.mu.Unlock()
	rows, w = runClassifyFresh(t, h, fake)
	if rows == nil {
		t.Fatalf("second pass failed: %d %s", w.Code, w.Body.String())
	}
	if len(sent) != 1 {
		t.Errorf("Claude was called again: %v", sent)
	}
	if rows[2].Grootboekcode != "4200" || rows[2].Category != "invoice" || rows[2].AIOmschrijving != "Voorbeeld" {
		t.Errorf("cached row = %+v", rows[2])
	}
	// A pass without a Claude call still rewrites the hash: the booked
	// line 1 drops out and the TTL slides.
	if store.size() != 1 {
		t.Errorf("cache holds %d lines after a cached pass, want 1", store.size())
	}

	// A new line arrives: only that one goes to Claude.
	fake.mu.Lock()
	fake.rows = append(fake.rows, lineCacheRow(3, -9.99, "VOORBEELD PARKEREN"))
	fake.mu.Unlock()
	rows, w = runClassifyFresh(t, h, fake)
	if rows == nil {
		t.Fatalf("third pass failed: %d %s", w.Code, w.Body.String())
	}
	if len(sent) != 2 || len(sent[1]) != 1 || sent[1][0] != 3 {
		t.Errorf("Claude saw %v, want only line 3 in the second call", sent)
	}
	if rows[2].Grootboekcode != "4200" || rows[3].Grootboekcode != "4300" {
		t.Errorf("rows = %v", rows)
	}
	if store.size() != 2 {
		t.Errorf("cache holds %d lines, want lines 2 and 3", store.size())
	}
}

func TestClassifyFreshDoesNotCacheAFailedPass(t *testing.T) {
	orig := classifyWithClaude
	classifyWithClaude = func(_ *InboxHandler, _ *gin.Context, _ string, _ string, _ []claude.BatchLine) ([]claude.BatchClassifyResult, error) {
		return nil, errors.New("Claude is niet bereikbaar")
	}
	t.Cleanup(func() { classifyWithClaude = orig })

	store := newFakeLineCache()
	storeLineCache(context.Background(), store, "u1", "", []claude.BatchLine{gridLine(1, -12.5, "VOORBEELD SAAS")},
		map[int]claude.BatchClassifyResult{1: lineCacheResult(1, "4500")})
	h := &InboxHandler{lineCache: store}
	fake := &lineCacheFake{rows: [][]any{
		lineCacheRow(1, -12.5, "VOORBEELD SAAS"),
		lineCacheRow(2, -48, "VOORBEELD TANKSTATION"),
	}}
	rows, w := runClassifyFresh(t, h, fake)
	if rows != nil || w.Code != http.StatusBadGateway {
		t.Fatalf("got rows %v, status %d; want 502", rows, w.Code)
	}
	if store.size() != 1 {
		t.Errorf("a failed pass changed the cache (%d lines)", store.size())
	}
}

func TestClassifyFreshWithoutAPIKeyAnswersNoAPIKeyOnlyWhenClaudeIsNeeded(t *testing.T) {
	// No seam: the real classifyWithClaude runs against a handler without a
	// database, which has no key.
	store := newFakeLineCache()
	h := &InboxHandler{lineCache: store}
	fake := &lineCacheFake{rows: [][]any{lineCacheRow(1, -12.5, "VOORBEELD SAAS")}}
	rows, w := runClassifyFresh(t, h, fake)
	if rows != nil || w.Code != http.StatusBadRequest {
		t.Fatalf("got rows %v, status %d; want 400 no_api_key", rows, w.Code)
	}
	var body map[string]any
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || body["error"] != "no_api_key" {
		t.Errorf("body = %s", w.Body.String())
	}

	// With the line in the cache the same handler answers without a key.
	storeLineCache(context.Background(), store, "u1", "", []claude.BatchLine{gridLine(1, -12.5, "VOORBEELD SAAS")},
		map[int]claude.BatchClassifyResult{1: lineCacheResult(1, "4500")})
	rows, w = runClassifyFresh(t, h, fake)
	if rows == nil {
		t.Fatalf("cached pass failed: %d %s", w.Code, w.Body.String())
	}
	if rows[1].Grootboekcode != "4500" {
		t.Errorf("row = %+v", rows[1])
	}
}
