package model

import "time"

type HubBroadcast struct {
	HubId         string    `json:"hub_id"`
	HubName       string    `json:"hub_name"`
	RegisterTopic string    `json:"register_topic"`
	AuthTopic     string    `json:"auth_topic"`
	QueryTopic    string    `json:"query_topic"`
	TtlSec        int       `json:"ttl_sec"`
	Timestamp     time.Time `json:"timestamp"`
}

type AuthBroadcast struct {
	AuthId      string    `json:"auth_id"`
	AuthName    string    `json:"auth_name"`
	VerifyTopic string    `json:"verify_topic"`
	PublicKey   string    `json:"public_key"`
	TtlSec      int       `json:"ttl_sec"`
	Timestamp   time.Time `json:"timestamp"`
}

type PeerAuthRequest struct {
	RequestId string `json:"request_id"`
	PeerId    string `json:"peer_id"`
	Key       string `json:"key"`
	Secret    string `json:"secret"`
	ReplyTo   string `json:"reply_to"`
}

type PeerAuthResponse struct {
	RequestId string `json:"request_id"`
	PeerId    string `json:"peer_id"`
	Granted   bool   `json:"granted"`
	AuthToken string `json:"auth_token"`
	TtlSec    int    `json:"ttl_sec"`
	Reason    string `json:"reason,omitempty"`
}

type PeerResourceRegisterRequest struct {
	RequestId string `json:"request_id"`
	PeerId    string `json:"peer_id"`
	PeerName  string `json:"peer_name"`
	Role      string `json:"role"`
	AuthToken string `json:"auth_token"`
	Resources struct {
		Apis    []map[string]string `json:"apis"`
		Topics  []map[string]string `json:"topics"`
		Streams []map[string]string `json:"streams"`
	} `json:"resources"`
}

type PeerListQuery struct {
	RequestId      string `json:"request_id"`
	SourcePeerId   string `json:"source_peer_id"`
	ResourceFilter string `json:"resource_filter,omitempty"`
	RoleFilter     string `json:"role_filter,omitempty"`
	StatusFilter   string `json:"status_filter,omitempty"`
	PageSize       int    `json:"page_size"`
	PageOffset     int    `json:"page_offset"`
	ReplyTo        string `json:"reply_to"`
}

type PeerListResponse struct {
	RequestId     string       `json:"request_id"`
	CorrelationId string       `json:"correlation_id"`
	Peers         []DdPeerInfo `json:"peers"`
	TotalCount    int          `json:"total_count"`
	PageOffset    int          `json:"page_offset"`
	HasMore       bool         `json:"has_more"`
}
