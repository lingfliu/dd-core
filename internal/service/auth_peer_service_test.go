package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
)

func TestAuthPeerServiceVerifyGranted(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")

	authService := NewAuthPeerService(mock, topics, nil, "auth-secret")
	authService.SetSecret("edge-key", "edge-secret")
	if err := authService.Start(context.Background()); err != nil {
		t.Fatalf("start auth service failed: %v", err)
	}

	responseCh := make(chan model.PeerAuthResponse, 1)
	replyTopic := "dd/default/transfer/edge-01/response"
	if err := mock.Subscribe(context.Background(), replyTopic, func(_ string, payload []byte) {
		var resp model.PeerAuthResponse
		json.Unmarshal(payload, &resp)
		responseCh <- resp
	}); err != nil {
		t.Fatalf("subscribe response failed: %v", err)
	}

	req := model.PeerAuthRequest{
		RequestId: "auth-1",
		PeerId:    "edge-01",
		Key:       "edge-key",
		Secret:    "edge-secret",
		ReplyTo:   replyTopic,
	}
	raw, _ := json.Marshal(req)
	mock.Emit(topics.PeerAuthVerify, raw)

	select {
	case resp := <-responseCh:
		if !resp.Granted {
			t.Fatalf("expected granted, reason=%s", resp.Reason)
		}
		if resp.AuthToken == "" {
			t.Fatal("expected non-empty auth token")
		}
		if resp.TtlSec != 300 {
			t.Fatalf("expected ttl=300, got %d", resp.TtlSec)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for auth response")
	}
}

func TestAuthPeerServiceVerifyDenied(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")

	authService := NewAuthPeerService(mock, topics, nil, "auth-secret")
	authService.SetSecret("edge-key", "edge-secret")
	if err := authService.Start(context.Background()); err != nil {
		t.Fatalf("start auth service failed: %v", err)
	}

	responseCh := make(chan model.PeerAuthResponse, 1)
	replyTopic := "dd/default/transfer/edge-01/response"
	if err := mock.Subscribe(context.Background(), replyTopic, func(_ string, payload []byte) {
		var resp model.PeerAuthResponse
		json.Unmarshal(payload, &resp)
		responseCh <- resp
	}); err != nil {
		t.Fatalf("subscribe response failed: %v", err)
	}

	req := model.PeerAuthRequest{
		RequestId: "auth-2",
		PeerId:    "edge-02",
		Key:       "wrong-key",
		Secret:    "wrong-secret",
		ReplyTo:   replyTopic,
	}
	raw, _ := json.Marshal(req)
	mock.Emit(topics.PeerAuthVerify, raw)

	select {
	case resp := <-responseCh:
		if resp.Granted {
			t.Fatal("expected denied for wrong credentials")
		}
		if resp.Reason != "invalid key/secret" {
			t.Fatalf("expected reason 'invalid key/secret', got %s", resp.Reason)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for auth response")
	}
}

func TestAuthPeerServiceVerifyToken(t *testing.T) {
	authService := NewAuthPeerService(nil, TopicSet{}, nil, "auth-secret")
	authService.SetSecret("edge-key", "edge-secret")

	token := generateAuthToken("edge-01", "edge-secret", "auth-secret")

	if !authService.VerifyToken(token, "edge-01", "edge-key") {
		t.Fatal("expected token to be valid")
	}

	if authService.VerifyToken("bad-token", "edge-01", "edge-key") {
		t.Fatal("expected bad token to be rejected")
	}

	if authService.VerifyToken(token, "edge-01", "wrong-key") {
		t.Fatal("expected token with wrong key to be rejected")
	}
}
