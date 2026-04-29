package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
)

func TestPeerListServiceQueryWithPagination(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")

	registry := NewPeerRegistryService(mock, topics, 10*time.Second)
	if err := registry.Start(context.Background()); err != nil {
		t.Fatalf("start registry failed: %v", err)
	}

	for i := 0; i < 35; i++ {
		register := model.DdPeerRegisterEvent{
			Peer: model.DdPeerInfo{
				Id:   "peer-" + string(rune('A'+i%26)) + string(rune('0'+(i/26))),
				Name: "peer",
				Role: "edge",
			},
		}
		raw, _ := json.Marshal(register)
		mock.Emit(topics.PeerRegister, raw)

		hb := model.DdPeerHeartbeatEvent{
			PeerId:    register.Peer.Id,
			Timestamp: time.Now().UTC(),
		}
		rawHb, _ := json.Marshal(hb)
		mock.Emit(topics.PeerHeartbeat, rawHb)
	}

	peerListService := NewPeerListService(mock, topics, registry, nil, 100)
	if err := peerListService.Start(context.Background()); err != nil {
		t.Fatalf("start peer list service failed: %v", err)
	}

	replyTopic := "dd/default/transfer/edge-01/response"
	responseCh := make(chan model.PeerListResponse, 1)
	if err := mock.Subscribe(context.Background(), replyTopic, func(_ string, payload []byte) {
		var resp model.PeerListResponse
		json.Unmarshal(payload, &resp)
		responseCh <- resp
	}); err != nil {
		t.Fatalf("subscribe response failed: %v", err)
	}

	query := model.PeerListQuery{
		RequestId:    "q-1",
		SourcePeerId: "caller",
		PageSize:     10,
		PageOffset:   0,
		ReplyTo:      replyTopic,
	}
	raw, _ := json.Marshal(query)
	mock.Emit(topics.PeerListQuery, raw)

	select {
	case resp := <-responseCh:
		if len(resp.Peers) != 10 {
			t.Fatalf("expected 10 peers on page 1, got %d", len(resp.Peers))
		}
		if resp.TotalCount != 35 {
			t.Fatalf("expected total 35, got %d", resp.TotalCount)
		}
		if !resp.HasMore {
			t.Fatal("expected has_more=true")
		}
		if resp.PageOffset != 0 {
			t.Fatalf("expected offset 0, got %d", resp.PageOffset)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for peer list response")
	}
}

func TestPeerListServiceFilterByRole(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")

	registry := NewPeerRegistryService(mock, topics, 10*time.Second)
	if err := registry.Start(context.Background()); err != nil {
		t.Fatalf("start registry failed: %v", err)
	}

	registerEdge := model.DdPeerRegisterEvent{
		Peer: model.DdPeerInfo{Id: "edge-1", Name: "e1", Role: "edge"},
	}
	rawEdge, _ := json.Marshal(registerEdge)
	mock.Emit(topics.PeerRegister, rawEdge)
	hbEdge, _ := json.Marshal(model.DdPeerHeartbeatEvent{PeerId: "edge-1", Timestamp: time.Now().UTC()})
	mock.Emit(topics.PeerHeartbeat, hbEdge)

	registerHub := model.DdPeerRegisterEvent{
		Peer: model.DdPeerInfo{Id: "hub-1", Name: "h1", Role: "hub"},
	}
	rawHub, _ := json.Marshal(registerHub)
	mock.Emit(topics.PeerRegister, rawHub)
	hbHub, _ := json.Marshal(model.DdPeerHeartbeatEvent{PeerId: "hub-1", Timestamp: time.Now().UTC()})
	mock.Emit(topics.PeerHeartbeat, hbHub)

	peerListService := NewPeerListService(mock, topics, registry, nil, 100)
	if err := peerListService.Start(context.Background()); err != nil {
		t.Fatalf("start peer list service failed: %v", err)
	}

	replyTopic := "dd/default/transfer/edge-01/response"
	responseCh := make(chan model.PeerListResponse, 1)
	mock.Subscribe(context.Background(), replyTopic, func(_ string, payload []byte) {
		var resp model.PeerListResponse
		json.Unmarshal(payload, &resp)
		responseCh <- resp
	})

	query := model.PeerListQuery{
		RequestId:    "q-2",
		SourcePeerId: "caller",
		RoleFilter:   "hub",
		PageSize:     10,
		ReplyTo:      replyTopic,
	}
	raw, _ := json.Marshal(query)
	mock.Emit(topics.PeerListQuery, raw)

	select {
	case resp := <-responseCh:
		if len(resp.Peers) != 1 {
			t.Fatalf("expected 1 hub peer, got %d", len(resp.Peers))
		}
		if resp.Peers[0].Id != "hub-1" {
			t.Fatalf("expected hub-1, got %s", resp.Peers[0].Id)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for peer list response")
	}
}

func TestPeerListServiceHasMoreFalse(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")

	registry := NewPeerRegistryService(mock, topics, 10*time.Second)
	if err := registry.Start(context.Background()); err != nil {
		t.Fatalf("start registry failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		register := model.DdPeerRegisterEvent{
			Peer: model.DdPeerInfo{Id: "p-" + string(rune('A'+i)), Name: "peer", Role: "edge"},
		}
		raw, _ := json.Marshal(register)
		mock.Emit(topics.PeerRegister, raw)
		hb, _ := json.Marshal(model.DdPeerHeartbeatEvent{PeerId: register.Peer.Id, Timestamp: time.Now().UTC()})
		mock.Emit(topics.PeerHeartbeat, hb)
	}

	peerListService := NewPeerListService(mock, topics, registry, nil, 100)
	if err := peerListService.Start(context.Background()); err != nil {
		t.Fatalf("start peer list service failed: %v", err)
	}

	replyTopic := "dd/default/transfer/edge-01/response"
	responseCh := make(chan model.PeerListResponse, 1)
	mock.Subscribe(context.Background(), replyTopic, func(_ string, payload []byte) {
		var resp model.PeerListResponse
		json.Unmarshal(payload, &resp)
		responseCh <- resp
	})

	query := model.PeerListQuery{
		RequestId:    "q-3",
		SourcePeerId: "caller",
		PageSize:     10,
		ReplyTo:      replyTopic,
	}
	raw, _ := json.Marshal(query)
	mock.Emit(topics.PeerListQuery, raw)

	select {
	case resp := <-responseCh:
		if resp.HasMore {
			t.Fatal("expected has_more=false when all peers fit in one page")
		}
		if resp.TotalCount != 3 {
			t.Fatalf("expected total 3, got %d", resp.TotalCount)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for peer list response")
	}
}

func TestPeerListServicePageTwo(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")

	registry := NewPeerRegistryService(mock, topics, 10*time.Second)
	if err := registry.Start(context.Background()); err != nil {
		t.Fatalf("start registry failed: %v", err)
	}

	for i := 0; i < 7; i++ {
		register := model.DdPeerRegisterEvent{
			Peer: model.DdPeerInfo{Id: "p-" + string(rune('A'+i)), Name: "peer", Role: "edge"},
		}
		raw, _ := json.Marshal(register)
		mock.Emit(topics.PeerRegister, raw)
		hb, _ := json.Marshal(model.DdPeerHeartbeatEvent{PeerId: register.Peer.Id, Timestamp: time.Now().UTC()})
		mock.Emit(topics.PeerHeartbeat, hb)
	}

	peerListService := NewPeerListService(mock, topics, registry, nil, 100)
	if err := peerListService.Start(context.Background()); err != nil {
		t.Fatalf("start peer list service failed: %v", err)
	}

	replyTopic := "dd/default/transfer/edge-01/response"
	responseCh := make(chan model.PeerListResponse, 1)
	mock.Subscribe(context.Background(), replyTopic, func(_ string, payload []byte) {
		var resp model.PeerListResponse
		json.Unmarshal(payload, &resp)
		responseCh <- resp
	})

	query := model.PeerListQuery{
		RequestId:    "q-4",
		SourcePeerId: "caller",
		PageSize:     5,
		PageOffset:   5,
		ReplyTo:      replyTopic,
	}
	raw, _ := json.Marshal(query)
	mock.Emit(topics.PeerListQuery, raw)

	select {
	case resp := <-responseCh:
		if len(resp.Peers) != 2 {
			t.Fatalf("expected 2 peers on page 2 (offset 5, total 7), got %d", len(resp.Peers))
		}
		if resp.HasMore {
			t.Fatal("expected has_more=false on last page")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for peer list response")
	}
}
