package model

import "time"

type StreamProfile string

const (
	StreamProfileFull    StreamProfile = "full"
	StreamProfileMinimal StreamProfile = "minimal"
	StreamProfileZero    StreamProfile = "zero"
)

type StreamStatus string

const (
	StreamStatusOpening StreamStatus = "opening"
	StreamStatusActive  StreamStatus = "active"
	StreamStatusClosing StreamStatus = "closing"
	StreamStatusClosed  StreamStatus = "closed"
	StreamStatusTimeout StreamStatus = "timeout"
	StreamStatusError   StreamStatus = "error"
)

type StreamSession struct {
	StreamId      string        `json:"stream_id"`
	Resource      string        `json:"resource"`
	Profile       StreamProfile `json:"profile"`
	SourcePeerId  string        `json:"source_peer_id"`
	TargetPeerIds []string      `json:"target_peer_ids"`
	Status        StreamStatus  `json:"status"`
	IdleTimeoutMs int64         `json:"idle_timeout_ms"`
	OpenedAt      time.Time     `json:"opened_at"`
	ClosedAt      time.Time     `json:"closed_at,omitempty"`
	LastActiveAt  time.Time     `json:"last_active_at"`
}

type StreamOpenEvent struct {
	StreamId      string        `json:"stream_id"`
	Resource      string        `json:"resource"`
	Profile       StreamProfile `json:"profile"`
	SourcePeerId  string        `json:"source_peer_id"`
	TargetPeerIds []string      `json:"target_peer_ids"`
	IdleTimeoutMs int64         `json:"idle_timeout_ms"`
}

type StreamCloseEvent struct {
	StreamId string `json:"stream_id"`
	Reason   string `json:"reason"`
}
