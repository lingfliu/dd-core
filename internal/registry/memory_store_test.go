package registry

import (
	"testing"
	"time"

	"dd-core/internal/model"
)

func TestMemoryStoreSetAndGet(t *testing.T) {
	s := NewMemoryStore()
	peer := model.DdPeerInfo{
		Id:   "p1",
		Name: "peer-one",
		Role: "edge",
	}
	s.Set("p1", peer)

	got, ok := s.Get("p1")
	if !ok {
		t.Fatal("expected peer p1 to exist")
	}
	if got.Id != "p1" || got.Name != "peer-one" {
		t.Fatalf("unexpected peer: %+v", got)
	}
}

func TestMemoryStoreDelete(t *testing.T) {
	s := NewMemoryStore()
	s.Set("p1", model.DdPeerInfo{Id: "p1"})
	s.Delete("p1")
	if _, ok := s.Get("p1"); ok {
		t.Fatal("expected peer p1 to be deleted")
	}
}

func TestMemoryStoreList(t *testing.T) {
	s := NewMemoryStore()
	s.Set("p1", model.DdPeerInfo{Id: "p1"})
	s.Set("p2", model.DdPeerInfo{Id: "p2"})

	peers := s.List()
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
}

func TestMemoryStoreSweepStale(t *testing.T) {
	s := NewMemoryStore()
	now := time.Now().UTC()
	s.Set("active", model.DdPeerInfo{
		Id:              "active",
		Status:          "active",
		LastHeartbeatAt: now,
	})
	s.Set("stale", model.DdPeerInfo{
		Id:              "stale",
		Status:          "active",
		LastHeartbeatAt: now.Add(-10 * time.Second),
	})

	s.SweepStale(now, 5*time.Second)

	active, ok := s.Get("active")
	if !ok || active.Status != "active" {
		t.Fatalf("expected active peer to remain active, got %+v", active)
	}

	stale, ok := s.Get("stale")
	if !ok {
		t.Fatal("expected stale peer to still exist")
	}
	if stale.Status != "stale" {
		t.Fatalf("expected stale peer status to be stale, got %s", stale.Status)
	}
}

func TestMemoryStoreSweepStaleNoHeartbeat(t *testing.T) {
	s := NewMemoryStore()
	s.Set("no-hb", model.DdPeerInfo{
		Id:     "no-hb",
		Status: "active",
	})

	s.SweepStale(time.Now().UTC(), 5*time.Second)

	peer, ok := s.Get("no-hb")
	if !ok {
		t.Fatal("expected peer to still exist")
	}
	if peer.Status != "active" {
		t.Fatalf("expected peer without heartbeat to stay active, got %s", peer.Status)
	}
}
