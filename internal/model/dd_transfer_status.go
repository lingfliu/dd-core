package model

import "time"

type TransferStatus string

const (
	TransferStatusAccepted  TransferStatus = "accepted"
	TransferStatusRouted    TransferStatus = "routed"
	TransferStatusDelivered TransferStatus = "delivered"
	TransferStatusResponded TransferStatus = "responded"
	TransferStatusTimeout   TransferStatus = "timeout"
	TransferStatusFailed    TransferStatus = "failed"
)

type TransferRecord struct {
	RequestId    string         `json:"request_id"`
	Resource     string         `json:"resource"`
	Protocol     DdProtocol     `json:"protocol"`
	Mode         DdTransferMode `json:"mode"`
	SourcePeerId string         `json:"source_peer_id"`
	TargetPeerId string         `json:"target_peer_id"`
	Status       TransferStatus `json:"status"`
	LatencyMs    int64          `json:"latency_ms,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}
