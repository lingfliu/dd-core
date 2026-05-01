package model

import "time"

type DdPeerRegisterEvent struct {
	Peer DdPeerInfo `json:"peer"`
}

type DdPeerHeartbeatEvent struct {
	PeerId    string    `json:"peer_id"`
	Timestamp time.Time `json:"timestamp"`
}

type DdPeerResourceReportEvent struct {
	PeerId    string    `json:"peer_id"`
	Role      string    `json:"role"`
	Timestamp time.Time `json:"timestamp"`
	Resources struct {
		Apis    []map[string]string `json:"apis"`
		Topics  []map[string]string `json:"topics"`
		Streams []map[string]string `json:"streams"`
	} `json:"resources"`
	ResourceSchemas map[string]ResourceMetaSchema `json:"resource_meta_schemas,omitempty"`
}

type DdPeerQueryRequest struct {
	RequestId string `json:"request_id"`
	ReplyTo   string `json:"reply_to"`
	Resource  string `json:"resource,omitempty"`
}

type DdPeerQueryResponse struct {
	RequestId string       `json:"request_id"`
	Peers     []DdPeerInfo `json:"peers"`
}
