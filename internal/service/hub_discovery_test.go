package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
)

func TestHubDiscoveryEdgeReceivesBroadcast(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")

	edge := NewHubDiscoveryService(mock, topics, "edge-01", "Edge", "edge", time.Second, 30)
	if err := edge.Start(context.Background()); err != nil {
		t.Fatalf("start edge discovery failed: %v", err)
	}

	hubCh := make(chan model.HubBroadcast, 1)
	edge.OnHubDiscovered(func(msg model.HubBroadcast) {
		hubCh <- msg
	})

	hubMsg := model.HubBroadcast{
		HubId:         "hub-01",
		HubName:       "Hub",
		RegisterTopic: topics.PeerResourceRegister,
		AuthTopic:     topics.PeerAuthVerify,
		QueryTopic:    topics.PeerListQuery,
		TtlSec:        30,
		Timestamp:     time.Now().UTC(),
	}
	raw, _ := json.Marshal(hubMsg)
	mock.Emit(topics.HubBroadcast, raw)

	select {
	case got := <-hubCh:
		if got.HubId != "hub-01" {
			t.Fatalf("expected hub-01, got %s", got.HubId)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for hub broadcast")
	}

	activeHub, ok := edge.GetActiveHub()
	if !ok || activeHub.HubId != "hub-01" {
		t.Fatal("expected active hub hub-01")
	}
}

func TestHubDiscoveryGetActiveHubTtlExpired(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")

	edge := NewHubDiscoveryService(mock, topics, "edge-01", "Edge", "edge", time.Second, 1)
	if err := edge.Start(context.Background()); err != nil {
		t.Fatalf("start edge discovery failed: %v", err)
	}

	hubMsg := model.HubBroadcast{
		HubId:     "hub-01",
		Timestamp: time.Now().UTC().Add(-2 * time.Second),
		TtlSec:    1,
	}
	raw, _ := json.Marshal(hubMsg)
	mock.Emit(topics.HubBroadcast, raw)

	_, ok := edge.GetActiveHub()
	if ok {
		t.Fatal("expected expired hub to not be active")
	}
}

func TestHubDiscoveryAuthBroadcast(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")

	edge := NewHubDiscoveryService(mock, topics, "edge-01", "Edge", "edge", time.Second, 30)
	if err := edge.Start(context.Background()); err != nil {
		t.Fatalf("start edge discovery failed: %v", err)
	}

	authCh := make(chan model.AuthBroadcast, 1)
	edge.OnAuthDiscovered(func(msg model.AuthBroadcast) {
		authCh <- msg
	})

	authMsg := model.AuthBroadcast{
		AuthId:      "auth-01",
		AuthName:    "Auth",
		VerifyTopic: "dd/default/peer/auth/verify",
		PublicKey:   "key123",
		TtlSec:      30,
		Timestamp:   time.Now().UTC(),
	}
	raw, _ := json.Marshal(authMsg)
	mock.Emit(topics.AuthBroadcast, raw)

	select {
	case got := <-authCh:
		if got.AuthId != "auth-01" {
			t.Fatalf("expected auth-01, got %s", got.AuthId)
		}
		if got.PublicKey != "key123" {
			t.Fatalf("expected key123, got %s", got.PublicKey)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for auth broadcast")
	}

	activeAuth, ok := edge.GetActiveAuth()
	if !ok || activeAuth.AuthId != "auth-01" {
		t.Fatal("expected active auth auth-01")
	}
}

func TestHubDiscoveryListActiveHubs(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")

	edge := NewHubDiscoveryService(mock, topics, "edge-01", "Edge", "edge", time.Second, 30)
	if err := edge.Start(context.Background()); err != nil {
		t.Fatalf("start edge discovery failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		hubMsg := model.HubBroadcast{
			HubId:     "hub-" + string(rune('A'+i)),
			TtlSec:    30,
			Timestamp: time.Now().UTC(),
		}
		raw, _ := json.Marshal(hubMsg)
		mock.Emit(topics.HubBroadcast, raw)
	}

	hubs := edge.ListActiveHubs()
	if len(hubs) != 3 {
		t.Fatalf("expected 3 hubs, got %d", len(hubs))
	}
}
