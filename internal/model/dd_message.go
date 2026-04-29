package model

import (
	"errors"
	"time"
)

type DdTransferMode string

const (
	DdTransferModeSync  DdTransferMode = "sync"
	DdTransferModeAsync DdTransferMode = "async"
)

type DdProtocol string

const (
	DdProtocolHttp   DdProtocol = "http"
	DdProtocolMq     DdProtocol = "mq"
	DdProtocolSocket DdProtocol = "socket"
	DdProtocolStream DdProtocol = "stream"
	DdProtocolCoap   DdProtocol = "coap"
)

type DdHeader struct {
	RequestId      string `json:"request_id,omitempty"`
	CorrelationId  string `json:"correlation_id,omitempty"`
	ReplyTo        string `json:"reply_to,omitempty"`
	TimeoutMs      int64  `json:"timeout_ms,omitempty"`
	SourcePeerId   string `json:"source_peer_id,omitempty"`
	TargetPeerId   string `json:"target_peer_id,omitempty"`
	Tenant         string `json:"tenant,omitempty"`
	TraceId        string `json:"trace_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type DdMessage struct {
	Mode      DdTransferMode    `json:"mode"`
	Protocol  DdProtocol        `json:"protocol"`
	Resource  string            `json:"resource"`
	Header    DdHeader          `json:"header"`
	Headers   map[string]string `json:"headers,omitempty"`
	Payload   []byte            `json:"payload,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

func (m *DdMessage) Validate() error {
	if m == nil {
		return errors.New("message is nil")
	}
	if m.Mode != DdTransferModeSync && m.Mode != DdTransferModeAsync {
		return errors.New("invalid mode")
	}
	switch m.Protocol {
	case DdProtocolHttp, DdProtocolMq, DdProtocolSocket, DdProtocolStream, DdProtocolCoap:
	default:
		return errors.New("invalid protocol")
	}
	if m.Resource == "" {
		return errors.New("resource is required")
	}
	if m.Mode == DdTransferModeSync {
		if m.Header.RequestId == "" {
			return errors.New("sync request_id is required")
		}
		if m.Header.TimeoutMs <= 0 {
			return errors.New("sync timeout_ms must be > 0")
		}
	}
	return nil
}
