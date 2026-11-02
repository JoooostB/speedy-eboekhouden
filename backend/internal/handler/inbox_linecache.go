package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/redis/go-redis/v9"
)

// Per-line classification cache.
//
// The classification base (inbox:classify, one hour) covers the whole inbox
// at once and is dropped by every booking, so without this cache every line
// that is still open goes back to Claude after each booking. Claude
// classifies each line on its own (the prompt looks at one line and the
// entity type, never across lines), so its answer can be remembered per
// line: one Redis HASH per user, field = a fingerprint of the line, value =
// the answer. classifyFresh asks Claude only for the lines that miss.
//
// The hash is rewritten with exactly the current lines after every Claude
// pass, so lines that were booked drop out by themselves, and the TTL
// slides as long as the inbox is in use. Only HASH commands and EXPIRE are
// used (no HEXPIRE), so redis 7 and 8 both work.

// inboxLineCachePromptVersion is part of the key. Bump it when
// batchClassifyPrompt, buildBatchSystemPrompt or the classification model
// changes (see claude/batch.go), so every line is classified again.
const inboxLineCachePromptVersion = 1

// inboxLineCacheTTL is how long the per-line cache lives after its last
// write.
const inboxLineCacheTTL = 30 * 24 * time.Hour

// inboxLineCacheKey is the Redis key of the user's per-line classification
// hash.
func inboxLineCacheKey(userID string) string {
	return fmt.Sprintf("inbox:line:v%d:%s", inboxLineCachePromptVersion, userID)
}

// inboxLineFingerprint is the hash field of one line: a SHA-256 over
// everything Claude sees for it. No raw text ends up in the field; a line
// that changes in e-boekhouden or a changed entity type simply misses. The
// amount is formatted to cents so -12.5 and -12.50 are the same line.
func inboxLineFingerprint(entityType string, line claude.BatchLine) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%.2f|%s|%s",
		entityType, line.ID, line.Datum, line.Bedrag, line.Omschrijving, line.Tegenrekening)))
	return hex.EncodeToString(sum[:])
}

// lineCacheStore is the part of the Redis client the per-line cache uses.
// *redis.Client satisfies it; tests supply an in-memory fake.
type lineCacheStore interface {
	HMGet(ctx context.Context, key string, fields ...string) *redis.SliceCmd
	TxPipeline() redis.Pipeliner
}

// loadLineCache splits lines into the ones with a cached classification
// (keyed by line id, with the id set again from the line) and the ones that
// have to go to Claude. One HMGET; a nil store or an error makes every line
// a miss, like the other AI caches.
func loadLineCache(ctx context.Context, store lineCacheStore, userID, entityType string, lines []claude.BatchLine) (map[int]claude.BatchClassifyResult, []claude.BatchLine) {
	hits := make(map[int]claude.BatchClassifyResult)
	if store == nil || len(lines) == 0 {
		return hits, lines
	}
	fields := make([]string, len(lines))
	for i, line := range lines {
		fields[i] = inboxLineFingerprint(entityType, line)
	}
	values, err := store.HMGet(ctx, inboxLineCacheKey(userID), fields...).Result()
	if err != nil || len(values) != len(lines) {
		if err != nil {
			log.Printf("Inbox: line cache read failed for user %s: %v", userID, err)
		}
		return hits, lines
	}
	var misses []claude.BatchLine
	for i, line := range lines {
		raw, ok := values[i].(string)
		if !ok {
			misses = append(misses, line)
			continue
		}
		var r claude.BatchClassifyResult
		if json.Unmarshal([]byte(raw), &r) != nil {
			misses = append(misses, line)
			continue
		}
		r.ID = line.ID
		hits[line.ID] = r
	}
	return hits, misses
}

// storeLineCache rewrites the user's hash with exactly the lines of the
// current pass that have a classification (cache hits and fresh answers
// alike), then refreshes the TTL. Lines Claude left out are not stored, so
// they are asked again next time. DEL + HSET + EXPIRE run in one
// transaction so a concurrent read never sees an empty hash. Best effort.
func storeLineCache(ctx context.Context, store lineCacheStore, userID, entityType string, lines []claude.BatchLine, results map[int]claude.BatchClassifyResult) {
	if store == nil {
		return
	}
	values := make(map[string]string, len(lines))
	for _, line := range lines {
		r, ok := results[line.ID]
		if !ok {
			continue
		}
		r.ID = 0 // set again from the line when read back
		data, err := json.Marshal(r)
		if err != nil {
			continue
		}
		values[inboxLineFingerprint(entityType, line)] = string(data)
	}
	key := inboxLineCacheKey(userID)
	pipe := store.TxPipeline()
	pipe.Del(ctx, key)
	if len(values) > 0 {
		pipe.HSet(ctx, key, values)
		pipe.Expire(ctx, key, inboxLineCacheTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("Inbox: line cache write failed for user %s: %v", userID, err)
	}
}
