package model

import "time"

const (
	PeerStatusRegistering = "registering"
	PeerStatusActive      = "active"
	PeerStatusStale       = "stale"
	PeerStatusOffline     = "offline"
)

type DdPeerInfo struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	//role: "edge", "term", "hub", "edge_hub"
	Role            string            `json:"role"`
	Key             string            `json:"key"`
	Secret          string            `json:"secret"`
	Status          string            `json:"status"`
	Resources       []string          `json:"resources,omitempty"`
	Capabilities    []string          `json:"capabilities,omitempty"`
	Tags            map[string]string `json:"tags,omitempty"`
	LastHeartbeatAt time.Time         `json:"last_heartbeat_at,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}
