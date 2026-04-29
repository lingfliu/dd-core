package service

import (
	"testing"

	"dd-core/internal/model"
)

func TestResourceCatalogRegisterAndQuery(t *testing.T) {
	cat := NewResourceCatalog()

	peer := model.DdPeerInfo{
		Id:        "edge-01",
		Resources: []string{"order.query", "sensor.temp"},
	}
	cat.OnPeerRegistered(peer)

	providers := cat.GetProviders("order.query")
	if len(providers) != 1 || providers[0] != "edge-01" {
		t.Fatalf("expected [edge-01], got %v", providers)
	}

	providers = cat.GetProviders("sensor.temp")
	if len(providers) != 1 || providers[0] != "edge-01" {
		t.Fatalf("expected [edge-01], got %v", providers)
	}

	providers = cat.GetProviders("nonexistent")
	if len(providers) != 0 {
		t.Fatalf("expected empty, got %v", providers)
	}
}

func TestResourceCatalogMultiplePeers(t *testing.T) {
	cat := NewResourceCatalog()

	cat.OnPeerRegistered(model.DdPeerInfo{Id: "p1", Resources: []string{"r1", "r2"}})
	cat.OnPeerRegistered(model.DdPeerInfo{Id: "p2", Resources: []string{"r1", "r3"}})

	providers := cat.GetProviders("r1")
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}

	providers = cat.GetProviders("r2")
	if len(providers) != 1 || providers[0] != "p1" {
		t.Fatalf("expected [p1], got %v", providers)
	}
}

func TestResourceCatalogUnregister(t *testing.T) {
	cat := NewResourceCatalog()

	cat.OnPeerRegistered(model.DdPeerInfo{Id: "p1", Resources: []string{"r1", "r2"}})
	cat.OnPeerRegistered(model.DdPeerInfo{Id: "p2", Resources: []string{"r1"}})

	cat.OnPeerUnregistered("p1")

	providers := cat.GetProviders("r1")
	if len(providers) != 1 || providers[0] != "p2" {
		t.Fatalf("expected [p2], got %v", providers)
	}

	providers = cat.GetProviders("r2")
	if len(providers) != 0 {
		t.Fatalf("expected empty, got %v", providers)
	}
}

func TestResourceCatalogResourceReport(t *testing.T) {
	cat := NewResourceCatalog()

	cat.OnPeerRegistered(model.DdPeerInfo{Id: "p1", Resources: []string{"r1"}})
	cat.OnPeerRegistered(model.DdPeerInfo{Id: "p2", Resources: []string{"r2"}})

	cat.OnResourceReport("p1", []string{"r1", "r3"})

	providers := cat.GetProviders("r1")
	if len(providers) != 1 || providers[0] != "p1" {
		t.Fatalf("expected [p1], got %v", providers)
	}

	providers = cat.GetProviders("r3")
	if len(providers) != 1 || providers[0] != "p1" {
		t.Fatalf("expected [p1], got %v", providers)
	}
}

func TestResourceCatalogListResources(t *testing.T) {
	cat := NewResourceCatalog()

	cat.OnPeerRegistered(model.DdPeerInfo{Id: "p1", Resources: []string{"a", "b"}})
	cat.OnPeerRegistered(model.DdPeerInfo{Id: "p2", Resources: []string{"b", "c"}})

	resources := cat.ListResources()
	if len(resources) != 3 {
		t.Fatalf("expected 3 resources, got %d: %v", len(resources), resources)
	}
}
