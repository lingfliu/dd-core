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
	Role            string                        `json:"role"`
	Key             string                        `json:"key"`
	Secret          string                        `json:"secret"`
	Status          string                        `json:"status"`
	Resources       []string                      `json:"resources,omitempty"`
	Capabilities    []string                      `json:"capabilities,omitempty"`
	ResourceSchemas map[string]ResourceMetaSchema `json:"resource_meta_schemas,omitempty"`
	Tags            map[string]string             `json:"tags,omitempty"`
	LastHeartbeatAt time.Time                     `json:"last_heartbeat_at,omitempty"`
	CreatedAt       time.Time                     `json:"created_at"`
	UpdatedAt       time.Time                     `json:"updated_at"`
}

type ResourceMetaSchema struct {
	Required             []string                          `json:"required,omitempty"`
	Optional             []string                          `json:"optional,omitempty"`
	Constraints          map[string]ResourceMetaConstraint `json:"constraints,omitempty"`
	ProtocolMetaRequired []string                          `json:"protocol_meta_required,omitempty"`
}

type ResourceMetaConstraint struct {
	Min  *int64   `json:"min,omitempty"`
	Max  *int64   `json:"max,omitempty"`
	Enum []string `json:"enum,omitempty"`
}
