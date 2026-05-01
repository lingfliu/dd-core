package model

import (
	"errors"
	"strings"
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

const (
	DdMessageVersionV1  = "v1"
	DdMessageVersionV13 = "v1.3"
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
	Version      string                       `json:"version,omitempty"`
	Mode         DdTransferMode               `json:"mode"`
	Protocol     DdProtocol                   `json:"protocol"`
	Resource     string                       `json:"resource"`
	Header       DdHeader                     `json:"header,omitempty"`
	Meta         DdHeader                     `json:"meta,omitempty"`
	Headers      map[string]string            `json:"headers,omitempty"`
	ProtocolMeta map[string]map[string]string `json:"protocol_meta,omitempty"`
	Payload      []byte                       `json:"payload,omitempty"`
	CreatedAt    time.Time                    `json:"created_at"`
}

func (m *DdMessage) Validate() error {
	if m == nil {
		return errors.New("message is nil")
	}
	m.Normalize()
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
	if err := m.validateProtocolMeta(); err != nil {
		return err
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

func (m *DdMessage) validateProtocolMeta() error {
	fields := m.ProtocolFields()
	if len(fields) == 0 {
		return nil
	}
	allowed := map[DdProtocol]map[string]struct{}{
		DdProtocolHttp: {
			"method": {}, "path": {}, "query": {}, "content-type": {}, "content_type": {},
		},
		DdProtocolCoap: {
			"method": {}, "path": {}, "content_format": {}, "token": {}, "observe": {},
		},
		DdProtocolMq: {
			"topic": {}, "qos": {}, "retain": {}, "partition_key": {},
		},
		DdProtocolStream: {
			"stream_id": {}, "profile": {}, "subproto": {}, "chunk_seq": {},
		},
		DdProtocolSocket: {},
	}
	allow, ok := allowed[m.Protocol]
	if !ok {
		return nil
	}
	for k := range fields {
		if _, ok := allow[k]; !ok {
			if m.Protocol == DdProtocolHttp && strings.Contains(k, "-") {
				// allow pass-through http headers such as accept/user-agent/x-*
				continue
			}
			return errors.New("invalid protocol_meta field: " + k)
		}
	}
	return nil
}

func (m *DdMessage) Normalize() {
	if m.Version == "" {
		m.Version = DdMessageVersionV1
	}
	if isHeaderZero(m.Meta) && !isHeaderZero(m.Header) {
		m.Meta = m.Header
	}
	if isHeaderZero(m.Header) && !isHeaderZero(m.Meta) {
		m.Header = m.Meta
	}
	if len(m.ProtocolMeta) == 0 && len(m.Headers) > 0 {
		m.ProtocolMeta = map[string]map[string]string{
			string(m.Protocol): cloneMap(m.Headers),
		}
	}
	if len(m.Headers) == 0 && len(m.ProtocolMeta) > 0 {
		if pm, ok := m.ProtocolMeta[string(m.Protocol)]; ok {
			m.Headers = cloneMap(pm)
		}
	}
}

func (m *DdMessage) ProtocolFields() map[string]string {
	m.Normalize()
	if pm, ok := m.ProtocolMeta[string(m.Protocol)]; ok {
		return pm
	}
	return m.Headers
}

func isHeaderZero(h DdHeader) bool {
	return h == (DdHeader{})
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
