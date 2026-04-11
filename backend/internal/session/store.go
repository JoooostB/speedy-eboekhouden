package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/joooostb/speedy-eboekhouden/internal/crypto"
	"github.com/redis/go-redis/v9"
)

const keyPrefix = "session:"

// Store manages sessions backed by Redis.
type Store struct {
	redis  *redis.Client
	maxAge time.Duration
	encKey crypto.AESKey
}

// NewStore creates a new Redis-backed session store with encryption for sensitive fields.
func NewStore(redisClient *redis.Client, maxAgeMinutes int, encKey crypto.AESKey) *Store {
	return &Store{
		redis:  redisClient,
		maxAge: time.Duration(maxAgeMinutes) * time.Minute,
		encKey: encKey,
	}
}

// Create creates a new session for a user.
func (s *Store) Create(ctx context.Context, userID string) (*SessionData, error) {
	sess := &SessionData{
		ID:        uuid.New().String(),
		UserID:    userID,
		CreatedAt: time.Now(),
	}

	if err := s.save(ctx, sess); err != nil {
		return nil, err
	}

	return sess, nil
}

// Get retrieves a session by ID and refreshes its TTL.
func (s *Store) Get(ctx context.Context, sessionID string) (*SessionData, error) {
	key := keyPrefix + sessionID
	data, err := s.redis.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting session: %w", err)
	}

	var sess SessionData
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("unmarshaling session: %w", err)
	}

	// Decrypt sensitive fields
	if sess.EBoekhoudenToken != "" {
		encrypted, err := base64.StdEncoding.DecodeString(sess.EBoekhoudenToken)
		if err == nil {
			decrypted, err := crypto.Decrypt(s.encKey, encrypted)
			if err == nil {
				sess.EBoekhoudenToken = string(decrypted)
			}
		}
	}

	// Refresh TTL (sliding expiry)
	s.redis.Expire(ctx, key, s.maxAge)

	return &sess, nil
}

// Update saves modified session data back to Redis.
func (s *Store) Update(ctx context.Context, sess *SessionData) error {
	return s.save(ctx, sess)
}

// Delete removes a session.
func (s *Store) Delete(ctx context.Context, sessionID string) error {
	return s.redis.Del(ctx, keyPrefix+sessionID).Err()
}

func (s *Store) save(ctx context.Context, sess *SessionData) error {
	// Encrypt sensitive fields before storing
	toStore := *sess
	if toStore.EBoekhoudenToken != "" {
		encrypted, err := crypto.Encrypt(s.encKey, []byte(toStore.EBoekhoudenToken))
		if err != nil {
			return fmt.Errorf("encrypting eb token: %w", err)
		}
		toStore.EBoekhoudenToken = base64.StdEncoding.EncodeToString(encrypted)
	}

	data, err := json.Marshal(toStore)
	if err != nil {
		return fmt.Errorf("marshaling session: %w", err)
	}

	return s.redis.Set(ctx, keyPrefix+sess.ID, data, s.maxAge).Err()
}
