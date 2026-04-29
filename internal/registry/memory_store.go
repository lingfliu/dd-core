package registry

import (
	"sync"
	"time"

	"dd-core/internal/model"
)

type Store interface {
	Get(peerID string) (model.DdPeerInfo, bool)
	Set(peerID string, peer model.DdPeerInfo)
	Delete(peerID string)
	List() []model.DdPeerInfo
	SweepStale(now time.Time, leaseTTL time.Duration)
}

type MemoryStore struct {
	mu    sync.RWMutex
	peers map[string]model.DdPeerInfo
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		peers: make(map[string]model.DdPeerInfo),
	}
}

func (s *MemoryStore) Get(peerID string) (model.DdPeerInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.peers[peerID]
	return p, ok
}

func (s *MemoryStore) Set(peerID string, peer model.DdPeerInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers[peerID] = peer
}

func (s *MemoryStore) Delete(peerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.peers, peerID)
}

func (s *MemoryStore) List() []model.DdPeerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.DdPeerInfo, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, p)
	}
	return out
}

func (s *MemoryStore) SweepStale(now time.Time, leaseTTL time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, peer := range s.peers {
		if peer.LastHeartbeatAt.IsZero() {
			continue
		}
		if now.Sub(peer.LastHeartbeatAt) > leaseTTL {
			peer.Status = "stale"
			peer.UpdatedAt = now
			s.peers[id] = peer
		}
	}
}
