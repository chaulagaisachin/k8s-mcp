package kube

import "sync"

// ContextStore holds a session-scoped default context. It never writes to the
// kubeconfig file; an empty override means "use the kubeconfig's current-context".
type ContextStore struct {
	mu       sync.RWMutex
	override string
}

// NewContextStore returns an empty store (defers to the kubeconfig default).
func NewContextStore() *ContextStore {
	return &ContextStore{}
}

// Set records the in-memory default context for this session.
func (s *ContextStore) Set(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.override = name
}

// Override returns the session default context, or "" if none is set.
func (s *ContextStore) Override() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.override
}
