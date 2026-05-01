package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
)

func TestPeerRegistryViaMqRegisterHeartbeatQuery(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")
	registry := NewPeerRegistryService(mock, topics, 2*time.Second)
	if err := registry.Start(context.Background()); err != nil {
		t.Fatalf("start registry failed: %v", err)
	}

	register := model.DdPeerRegisterEvent{
		Peer: model.DdPeerInfo{
			Id:   "peer-1",
			Name: "peer-one",
			Role: "edge",
		},
	}
	rawRegister, _ := json.Marshal(register)
	mock.Emit(topics.PeerRegister, rawRegister)

	report := model.DdPeerResourceReportEvent{
		PeerId: "peer-1",
		Role:   "edge",
	}
	report.Resources.Apis = []map[string]string{{"name": "order.query"}}
	rawReport, _ := json.Marshal(report)
	mock.Emit(topics.PeerResourceReport, rawReport)

	hb := model.DdPeerHeartbeatEvent{
		PeerId:    "peer-1",
		Timestamp: time.Now().UTC(),
	}
	rawHB, _ := json.Marshal(hb)
	mock.Emit(topics.PeerHeartbeat, rawHB)

	replyTopic := "dd/default/peer/query/reply"
	query := model.DdPeerQueryRequest{
		RequestId: "q-1",
		ReplyTo:   replyTopic,
		Resource:  "order.query",
	}
	rawQuery, _ := json.Marshal(query)
	mock.Emit(topics.PeerQuery, rawQuery)

	published := mock.Published()
	var found bool
	for _, p := range published {
		if p.Topic == replyTopic {
			found = true
			var resp model.DdPeerQueryResponse
			if err := json.Unmarshal(p.Payload, &resp); err != nil {
				t.Fatalf("unmarshal query response failed: %v", err)
			}
			if len(resp.Peers) != 1 || resp.Peers[0].Id != "peer-1" {
				t.Fatalf("unexpected peers in response: %+v", resp.Peers)
			}
		}
	}
	if !found {
		t.Fatalf("expected query response published to %s", replyTopic)
	}
}

func TestPeerRegistrySweepStale(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")
	registry := NewPeerRegistryService(mock, topics, time.Second)
	if err := registry.Start(context.Background()); err != nil {
		t.Fatalf("start registry failed: %v", err)
	}
	register := model.DdPeerRegisterEvent{
		Peer: model.DdPeerInfo{Id: "peer-2", Name: "peer-two", Role: "edge"},
	}
	raw, _ := json.Marshal(register)
	mock.Emit(topics.PeerRegister, raw)

	registry.SweepStale(time.Now().UTC().Add(2 * time.Second))
	peers := registry.GetPeers("")
	if len(peers) != 0 {
		t.Fatalf("expected stale peer filtered out, got %d", len(peers))
	}
}

func TestPeerRegistryGetResourceMetaSchema(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")
	registry := NewPeerRegistryService(mock, topics, time.Second)

	peer := model.DdPeerInfo{
		Id:           "peer-schema",
		Name:         "schema-peer",
		Role:         "edge",
		Status:       model.PeerStatusActive,
		Resources:    []string{"sensor.temp"},
		Capabilities: []string{"resource_meta_schema_v1"},
		ResourceSchemas: map[string]model.ResourceMetaSchema{
			"sensor.temp": {
				Required:             []string{"source_peer_id"},
				ProtocolMetaRequired: []string{"method"},
			},
		},
		LastHeartbeatAt: time.Now().UTC(),
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	registry.Store().Set(peer.Id, peer)

	schema, ok := registry.GetResourceMetaSchema("sensor.temp")
	if !ok {
		t.Fatal("expected resource schema to exist")
	}
	if len(schema.Required) != 1 || schema.Required[0] != "source_peer_id" {
		t.Fatalf("unexpected schema required fields: %+v", schema.Required)
	}
}

func TestPeerRegistryGetResourceMetaSchemaAggregatesProviders(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")
	registry := NewPeerRegistryService(mock, topics, time.Second)

	minA := int64(100)
	maxA := int64(2000)
	minB := int64(300)
	maxB := int64(1500)
	now := time.Now().UTC()

	registry.Store().Set("peer-a", model.DdPeerInfo{
		Id:           "peer-a",
		Status:       model.PeerStatusActive,
		Resources:    []string{"sensor.temp"},
		Capabilities: []string{"resource_meta_schema_v1"},
		ResourceSchemas: map[string]model.ResourceMetaSchema{
			"sensor.temp": {
				Required:             []string{"source_peer_id"},
				ProtocolMetaRequired: []string{"method"},
				Constraints: map[string]model.ResourceMetaConstraint{
					"timeout_ms": {Min: &minA, Max: &maxA},
					"method":     {Enum: []string{"GET", "POST"}},
				},
			},
		},
		LastHeartbeatAt: now,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	registry.Store().Set("peer-b", model.DdPeerInfo{
		Id:           "peer-b",
		Status:       model.PeerStatusActive,
		Resources:    []string{"sensor.temp"},
		Capabilities: []string{"resource_meta_schema_v1"},
		ResourceSchemas: map[string]model.ResourceMetaSchema{
			"sensor.temp": {
				Required:             []string{"target_peer_id"},
				ProtocolMetaRequired: []string{"path"},
				Constraints: map[string]model.ResourceMetaConstraint{
					"timeout_ms": {Min: &minB, Max: &maxB},
					"method":     {Enum: []string{"POST", "PUT"}},
				},
			},
		},
		LastHeartbeatAt: now,
		CreatedAt:       now,
		UpdatedAt:       now,
	})

	schema, ok := registry.GetResourceMetaSchema("sensor.temp")
	if !ok {
		t.Fatal("expected aggregated schema")
	}
	if !containsString(schema.Required, "source_peer_id") || !containsString(schema.Required, "target_peer_id") {
		t.Fatalf("required fields not aggregated: %+v", schema.Required)
	}
	if !containsString(schema.ProtocolMetaRequired, "method") || !containsString(schema.ProtocolMetaRequired, "path") {
		t.Fatalf("protocol meta required not aggregated: %+v", schema.ProtocolMetaRequired)
	}
	timeout := schema.Constraints["timeout_ms"]
	if timeout.Min == nil || *timeout.Min != 300 {
		t.Fatalf("expected merged min=300, got %+v", timeout.Min)
	}
	if timeout.Max == nil || *timeout.Max != 1500 {
		t.Fatalf("expected merged max=1500, got %+v", timeout.Max)
	}
	method := schema.Constraints["method"]
	if len(method.Enum) != 1 || method.Enum[0] != "POST" {
		t.Fatalf("expected enum intersection [POST], got %+v", method.Enum)
	}
}
