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
