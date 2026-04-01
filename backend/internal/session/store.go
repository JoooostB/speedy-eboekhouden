package session

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
)

// Store manages in-memory sessions.
type Store struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	maxAge   int // minutes
}

// NewStore creates a new session store and starts the cleanup goroutine.
func NewStore(maxAgeMinutes int) *Store {
	s := &Store{
		sessions: make(map[string]*Session),
		maxAge:   maxAgeMinutes,
	}
	go s.cleanup()
	return s
}

// Create creates a new session with an e-boekhouden client.
func (s *Store) Create(client *eboekhouden.Client) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := uuid.New().String()
	now := time.Now()
	sess := &Session{
		ID:        id,
		Client:    client,
		CreatedAt: now,
		LastUsed:  now,
	}
	s.sessions[id] = sess
	return sess
}

// Get retrieves a session by ID. Returns nil if not found or expired.
func (s *Store) Get(id string) *Session {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()

	if !ok {
		return nil
	}

	if sess.IsExpired(s.maxAge) {
		s.Delete(id)
		return nil
	}

	sess.Touch()
	return sess
}

// Delete removes a session.
func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// cleanup periodically removes expired sessions.
func (s *Store) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		s.mu.Lock()
		for id, sess := range s.sessions {
			if sess.IsExpired(s.maxAge) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}
