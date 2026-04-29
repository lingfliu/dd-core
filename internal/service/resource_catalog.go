package service

import (
	"sync"

	"dd-core/internal/model"
)

type ResourceCatalog struct {
	mu    sync.RWMutex
	index map[string][]string
}

func NewResourceCatalog() *ResourceCatalog {
	return &ResourceCatalog{index: make(map[string][]string)}
}

func (c *ResourceCatalog) OnPeerRegistered(peer model.DdPeerInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, resource := range peer.Resources {
		c.addProvider(resource, peer.Id)
	}
}

func (c *ResourceCatalog) OnPeerUnregistered(peerId string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for resource, providers := range c.index {
		filtered := make([]string, 0, len(providers))
		for _, pid := range providers {
			if pid != peerId {
				filtered = append(filtered, pid)
			}
		}
		c.index[resource] = filtered
	}
}

func (c *ResourceCatalog) OnResourceReport(peerId string, resources []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for resource, providers := range c.index {
		filtered := make([]string, 0, len(providers))
		for _, pid := range providers {
			if pid != peerId {
				filtered = append(filtered, pid)
			}
		}
		c.index[resource] = filtered
	}
	for _, resource := range resources {
		c.addProvider(resource, peerId)
	}
}

func (c *ResourceCatalog) GetProviders(resource string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]string(nil), c.index[resource]...)
}

func (c *ResourceCatalog) ListResources() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]string, 0, len(c.index))
	for resource := range c.index {
		result = append(result, resource)
	}
	return result
}

func (c *ResourceCatalog) addProvider(resource, peerId string) {
	providers := c.index[resource]
	for _, pid := range providers {
		if pid == peerId {
			return
		}
	}
	c.index[resource] = append(providers, peerId)
}
