package memory

import (
	"context"
	"slices"
	"sync"
)

// ResumeStore keeps resume files in memory, for tests and for quick local runs without object
// storage. Everything is lost on restart.
type ResumeStore struct {
	mu    sync.RWMutex
	files map[string][]byte
}

// NewResumeStore returns an empty store.
func NewResumeStore() *ResumeStore {
	return &ResumeStore{files: map[string][]byte{}}
}

// StoreResume keeps a copy of the file under key.
func (s *ResumeStore) StoreResume(_ context.Context, key string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[key] = slices.Clone(content)
	return nil
}

// Resume returns the file stored under key.
func (s *ResumeStore) Resume(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	content, ok := s.files[key]
	return slices.Clone(content), ok
}
