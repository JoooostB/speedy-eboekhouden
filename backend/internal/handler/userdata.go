package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Per-user Redis data and how "Verbinding verbreken" purges it.
//
// Every cached piece of administration data lives under a key that carries
// the user id; the key builders are inboxClassifyKey (inbox.go),
// invoiceAnalysisCacheKey and invoiceSubmittedMarkerKey (invoice.go), and
// documentAnalysisKey, documentAnalyzingKey and documentIgnoredKey
// (documents.go). userCachePatterns lists the MATCH pattern of each family.
// TestUserCachePatternsCoverKeyBuilders checks that every builder's output
// matches one of them, so a new key family cannot be added without either
// extending this list or failing the build.
//
// Deliberately not purged: session:* (the session stays, only its token is
// cleared), ratelimit:* (per IP, not per user) and the learned classifications
// in PostgreSQL (user preferences, deletable under Instellingen → Geleerde
// boekingen).

// purgeScanCount is the COUNT hint per SCAN call: enough to finish a user's
// keys in a few round trips, small enough not to stall Redis.
const purgeScanCount = 200

// redisGlobEscape escapes the glob metacharacters of a MATCH pattern so a
// literal user id can never widen a pattern. User ids are UUIDs, so this is
// defence in depth rather than a live concern.
func redisGlobEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '*', '?', '[', ']', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// userCachePatterns returns the SCAN patterns of all cached administration
// data of one user: the classification base, upload analyses, duplicate
// markers, mail-folder analyses and locks, and the ignore list. The versioned
// families keep their "v" prefix in the pattern so the user id has to appear
// in its own segment; hashes and ids never contain a UUID.
func userCachePatterns(userID string) []string {
	u := redisGlobEscape(userID)
	return []string{
		"inbox:classify:" + u,
		"invoice:analyze:v*:" + u + ":*",
		"invoice:submitted:" + u + ":*",
		"doc:analysis:v*:" + u + ":*",
		"doc:analyzing:" + u + ":*",
		"doc:ignored:" + u,
	}
}

// keyScanner is the part of the Redis client the purge needs. *redis.Client
// satisfies it; tests supply an in-memory fake.
type keyScanner interface {
	Scan(ctx context.Context, cursor uint64, match string, count int64) *redis.ScanCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

// purgeUserCaches deletes every key matched by userCachePatterns, walking
// each pattern with SCAN (never KEYS) and deleting per page. It returns the
// number of keys deleted. A failing pattern does not stop the others; the
// first error is returned together with the count so far.
func purgeUserCaches(ctx context.Context, rdb keyScanner, userID string) (int, error) {
	deleted := 0
	var firstErr error
	fail := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	for _, pattern := range userCachePatterns(userID) {
		var cursor uint64
		for {
			keys, next, err := rdb.Scan(ctx, cursor, pattern, purgeScanCount).Result()
			if err != nil {
				fail(fmt.Errorf("scan: %w", err))
				break
			}
			if len(keys) > 0 {
				n, err := rdb.Del(ctx, keys...).Result()
				if err != nil {
					fail(fmt.Errorf("del: %w", err))
					break
				}
				deleted += int(n)
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	return deleted, firstErr
}
