package handler

import (
	"context"
	"errors"
	"path"
	"sort"
	"testing"

	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/redis/go-redis/v9"
)

const (
	testUserA = "11111111-2222-3333-4444-555555555555"
	testUserB = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
)

// userKeys returns one key per key builder for userID, so a builder that is
// added without a purge pattern shows up here first.
func userKeys(userID string) []string {
	accounts := []claude.LedgerAccountInfo{{Code: "4000", Omschrijving: "Kosten"}}
	return []string{
		inboxClassifyKey(userID),
		invoiceAnalysisCacheKey(userID, []byte("%PDF-1.4 x"), accounts),
		invoiceSubmittedMarkerKey(userID, invoicePDFContentHash([]byte("%PDF-1.4 x"))),
		documentAnalysisKey(userID, 12340002),
		documentAnalyzingKey(userID, 12340002),
		documentIgnoredKey(userID),
	}
}

// matchesAny reports whether key matches one of the Redis glob patterns.
// path.Match has the same semantics for the characters these keys use (no
// slashes), including backslash escapes.
func matchesAny(t *testing.T, patterns []string, key string) bool {
	t.Helper()
	for _, p := range patterns {
		ok, err := path.Match(p, key)
		if err != nil {
			t.Fatalf("pattern %q: %v", p, err)
		}
		if ok {
			return true
		}
	}
	return false
}

func TestUserCachePatternsCoverKeyBuilders(t *testing.T) {
	patterns := userCachePatterns(testUserA)
	for _, key := range userKeys(testUserA) {
		if !matchesAny(t, patterns, key) {
			t.Errorf("key %q matches no purge pattern", key)
		}
	}
}

func TestUserCachePatternsAreScopedToTheUser(t *testing.T) {
	patterns := userCachePatterns(testUserA)
	// The hash segments of the versioned keys are hex, so another user's key
	// can never carry this user's UUID; the patterns rely on that.
	other := append(userKeys(testUserB),
		"session:"+testUserA,
		"ratelimit:login:10.0.0.1",
	)
	for _, key := range other {
		if matchesAny(t, patterns, key) {
			t.Errorf("key %q of another user or family matches a purge pattern", key)
		}
	}
}

func TestRedisGlobEscape(t *testing.T) {
	tests := map[string]string{
		testUserA:  testUserA,
		"a*b?c[d]": `a\*b\?c\[d\]`,
		`back\sl`:  `back\\sl`,
	}
	for in, want := range tests {
		if got := redisGlobEscape(in); got != want {
			t.Errorf("redisGlobEscape(%q) = %q, want %q", in, got, want)
		}
	}
	// An escaped pattern matches only the literal key.
	p := userCachePatterns("u*")[0]
	if ok, _ := path.Match(p, "inbox:classify:u*"); !ok {
		t.Errorf("pattern %q does not match its literal key", p)
	}
	if ok, _ := path.Match(p, "inbox:classify:uX"); ok {
		t.Errorf("pattern %q matches a different key", p)
	}
}

// fakeScanner is an in-memory keyScanner. Cursors are positions in the
// sorted list of every key ever inserted, so a page boundary stays stable
// while earlier pages are deleted, as with a real Redis cursor; deleted keys
// are skipped when a page is returned.
type fakeScanner struct {
	all      []string
	deleted  map[string]bool
	pageSize int
	scans    int
	failScan bool
}

func newFakeScanner(pageSize int, keys ...string) *fakeScanner {
	f := &fakeScanner{deleted: map[string]bool{}, pageSize: pageSize}
	f.all = append(f.all, keys...)
	sort.Strings(f.all)
	return f
}

func (f *fakeScanner) Scan(_ context.Context, cursor uint64, match string, _ int64) *redis.ScanCmd {
	f.scans++
	if f.failScan {
		return redis.NewScanCmdResult(nil, 0, errors.New("scan refused"))
	}
	start := int(cursor)
	if start > len(f.all) {
		start = len(f.all)
	}
	end := len(f.all)
	if f.pageSize > 0 && start+f.pageSize < end {
		end = start + f.pageSize
	}
	var page []string
	for _, k := range f.all[start:end] {
		if ok, _ := path.Match(match, k); ok && !f.deleted[k] {
			page = append(page, k)
		}
	}
	next := uint64(end)
	if end >= len(f.all) {
		next = 0
	}
	return redis.NewScanCmdResult(page, next, nil)
}

func (f *fakeScanner) Del(_ context.Context, keys ...string) *redis.IntCmd {
	var n int64
	for _, k := range keys {
		if !f.deleted[k] {
			f.deleted[k] = true
			n++
		}
	}
	return redis.NewIntResult(n, nil)
}

func (f *fakeScanner) live() []string {
	var out []string
	for _, k := range f.all {
		if !f.deleted[k] {
			out = append(out, k)
		}
	}
	return out
}

func TestPurgeUserCaches(t *testing.T) {
	keysA := userKeys(testUserA)
	keysB := userKeys(testUserB)
	extra := []string{"session:abc", "ratelimit:login:10.0.0.1"}
	all := append(append(append([]string{}, keysA...), keysB...), extra...)

	// Page size 1 forces the cursor loop through every position.
	fake := newFakeScanner(1, all...)
	n, err := purgeUserCaches(context.Background(), fake, testUserA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != len(keysA) {
		t.Errorf("deleted %d keys, want %d", n, len(keysA))
	}
	want := append(append([]string{}, keysB...), extra...)
	sort.Strings(want)
	got := fake.live()
	if len(got) != len(want) {
		t.Fatalf("live keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("live key %d = %q, want %q", i, got[i], want[i])
		}
	}
	if fake.scans <= len(userCachePatterns(testUserA)) {
		t.Errorf("scans = %d, want more than one per pattern with page size 1", fake.scans)
	}
}

func TestPurgeUserCachesReportsScanError(t *testing.T) {
	fake := newFakeScanner(0, userKeys(testUserA)...)
	fake.failScan = true
	n, err := purgeUserCaches(context.Background(), fake, testUserA)
	if err == nil {
		t.Fatal("expected an error")
	}
	if n != 0 {
		t.Errorf("deleted %d keys, want 0", n)
	}
}
