package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/crypto"
	"github.com/redis/go-redis/v9"
)

// fakeStatusStore is an in-memory apiKeyStatusStore.
type fakeStatusStore struct {
	values  map[string]string
	ttls    map[string]time.Duration
	failGet bool
}

func newFakeStatusStore() *fakeStatusStore {
	return &fakeStatusStore{values: map[string]string{}, ttls: map[string]time.Duration{}}
}

func (f *fakeStatusStore) Get(_ context.Context, key string) *redis.StringCmd {
	if f.failGet {
		return redis.NewStringResult("", errors.New("redis down"))
	}
	v, ok := f.values[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(v, nil)
}

func (f *fakeStatusStore) Set(_ context.Context, key string, value any, ttl time.Duration) *redis.StatusCmd {
	f.values[key] = value.(string)
	f.ttls[key] = ttl
	return redis.NewStatusResult("OK", nil)
}

func (f *fakeStatusStore) Del(_ context.Context, keys ...string) *redis.IntCmd {
	var n int64
	for _, k := range keys {
		if _, ok := f.values[k]; ok {
			delete(f.values, k)
			n++
		}
	}
	return redis.NewIntResult(n, nil)
}

// statusProbe plays Anthropic: it answers status and records the keys it
// was asked about.
type statusProbe struct {
	status string
	asked  []string
}

func (p *statusProbe) probe(_ context.Context, apiKey string) gin.H {
	p.asked = append(p.asked, apiKey)
	if p.status == "active" {
		return gin.H{"status": "active"}
	}
	return gin.H{"status": p.status, "message": "Voorbeeldmelding"}
}

var testEncKey = crypto.AESKey{1, 2, 3}

func statusHandler(store *fakeStatusStore, p *statusProbe) *SettingsHandler {
	h := &SettingsHandler{encKey: testEncKey, probeKey: p.probe}
	if store != nil {
		h.redis = store
	}
	return h
}

// storedKey encrypts an API key the way SetAPIKey stores it.
func storedKey(t *testing.T, apiKey string) []byte {
	t.Helper()
	enc, err := crypto.Encrypt(testEncKey, []byte(apiKey))
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

func TestAPIKeyStatusCachesOnlyActive(t *testing.T) {
	ctx := context.Background()
	store := newFakeStatusStore()
	p := &statusProbe{status: "active"}
	h := statusHandler(store, p)
	key1 := storedKey(t, "sk-ant-voorbeeld-1")

	for i := 0; i < 3; i++ {
		if got := h.apiKeyStatus(ctx, testUserA, key1); got["status"] != "active" {
			t.Fatalf("visit %d: status %v", i, got)
		}
	}
	if len(p.asked) != 1 || p.asked[0] != "sk-ant-voorbeeld-1" {
		t.Errorf("Anthropic was asked %v for three visits, want the decrypted key once", p.asked)
	}
	key := apiKeyStatusKey(testUserA)
	if store.ttls[key] != apiKeyStatusTTL {
		t.Errorf("TTL = %v, want %v", store.ttls[key], apiKeyStatusTTL)
	}
	if store.values[key] != keyFingerprint(key1) {
		t.Errorf("cached value %q, want the fingerprint of the stored key", store.values[key])
	}

	// Another key (saved elsewhere, cache not dropped) is checked afresh.
	p.status = "invalid"
	if got := h.apiKeyStatus(ctx, testUserA, storedKey(t, "sk-ant-voorbeeld-2")); got["status"] != "invalid" {
		t.Errorf("new key: status %v", got)
	}
	if len(p.asked) != 2 {
		t.Errorf("asked %v, want a second check", p.asked)
	}
}

func TestAPIKeyStatusCacheHitDoesNotDecrypt(t *testing.T) {
	// The security page says the key is only decrypted for a call to
	// Anthropic. A cache hit makes no call, so it must not decrypt either:
	// with a ciphertext that cannot be decrypted, a hit still answers.
	store := newFakeStatusStore()
	garbage := []byte("not a ciphertext")
	store.values[apiKeyStatusKey(testUserA)] = keyFingerprint(garbage)
	p := &statusProbe{status: "active"}

	if got := statusHandler(store, p).apiKeyStatus(context.Background(), testUserA, garbage); got["status"] != "active" || len(p.asked) != 0 {
		t.Errorf("status %v, asked %v", got, p.asked)
	}
}

func TestAPIKeyStatusUndecryptableKey(t *testing.T) {
	p := &statusProbe{status: "active"}
	got := statusHandler(newFakeStatusStore(), p).apiKeyStatus(context.Background(), testUserA, []byte("not a ciphertext"))
	if got["status"] != "error" || got["message"] != "Speedy kan je opgeslagen API-sleutel niet meer ontsleutelen. Vul de sleutel opnieuw in." {
		t.Errorf("status %v", got)
	}
	if len(p.asked) != 0 {
		t.Errorf("asked %v", p.asked)
	}
}

func TestAPIKeyStatusDoesNotCacheRefusals(t *testing.T) {
	// A refused check is not billed, and the user may top up or fix the key
	// at any moment, so no credit, an invalid key and an error are asked
	// again every time; no credit still comes through.
	for _, s := range []string{"no_credits", "invalid", "error"} {
		t.Run(s, func(t *testing.T) {
			ctx := context.Background()
			store := newFakeStatusStore()
			p := &statusProbe{status: s}
			h := statusHandler(store, p)
			key := storedKey(t, "sk-ant-voorbeeld-1")

			for i := 0; i < 2; i++ {
				if got := h.apiKeyStatus(ctx, testUserA, key); got["status"] != s || got["message"] == nil {
					t.Fatalf("status %v", got)
				}
			}
			if len(p.asked) != 2 || len(store.values) != 0 {
				t.Errorf("asked %v, cached %v: want 2 checks and nothing cached", p.asked, store.values)
			}

			// Topped up: the next visit says so at once.
			p.status = "active"
			if got := h.apiKeyStatus(ctx, testUserA, key); got["status"] != "active" {
				t.Errorf("after top-up: %v", got)
			}
		})
	}
}

func TestAPIKeyStatusWithoutRedis(t *testing.T) {
	key := storedKey(t, "k")
	p := &statusProbe{status: "active"}
	h := statusHandler(nil, p)
	h.apiKeyStatus(context.Background(), testUserA, key)
	h.apiKeyStatus(context.Background(), testUserA, key)
	if len(p.asked) != 2 {
		t.Errorf("asked %v, want a check per visit without Redis", p.asked)
	}

	store := newFakeStatusStore()
	store.failGet = true
	if got := statusHandler(store, p).apiKeyStatus(context.Background(), testUserA, key); got["status"] != "active" {
		t.Errorf("a Redis failure must fall back to the check: %v", got)
	}
}

func TestAPIKeyStatusIsPerUserAndForgotten(t *testing.T) {
	ctx := context.Background()
	store := newFakeStatusStore()
	p := &statusProbe{status: "active"}
	h := statusHandler(store, p)
	key := storedKey(t, "sk-ant-voorbeeld-1")

	h.apiKeyStatus(ctx, testUserA, key)
	h.apiKeyStatus(ctx, testUserB, key)
	if len(p.asked) != 2 {
		t.Errorf("asked %v: one user's verdict must not answer for another", p.asked)
	}

	h.forgetAPIKeyStatus(ctx, testUserA)
	if _, ok := store.values[apiKeyStatusKey(testUserA)]; ok {
		t.Error("forgetAPIKeyStatus left the entry")
	}
	if _, ok := store.values[apiKeyStatusKey(testUserB)]; !ok {
		t.Error("forgetAPIKeyStatus removed another user's entry")
	}
}

func TestNewSettingsHandlerWithoutRedis(t *testing.T) {
	h := NewSettingsHandler(nil, crypto.AESKey{}, nil)
	if h.redis != nil {
		t.Error("a nil Redis client must leave the store nil, not a typed nil")
	}
	if h.probeKey == nil {
		t.Error("the Anthropic probe must default to probeAnthropicKey")
	}
}
