package adapter

import (
	"context"
	"testing"
	"time"

	"dd-core/internal/mq"
)

func TestMqttBridgeMapping(t *testing.T) {
	mock := mq.NewMockClient()

	mappings := []MqttMapping{
		{Source: "dd/tenant-a/event/#", Target: "dd/tenant-b/event/#"},
		{Source: "dd/tenant-a/metrics", Target: "dd/tenant-b/metrics"},
	}

	bridge := NewMqttBridge(mock, mappings)
	if err := bridge.Start(context.Background()); err != nil {
		t.Fatalf("start mqtt bridge failed: %v", err)
	}

	recvCh := make(chan []byte, 1)
	if err := mock.Subscribe(context.Background(), "dd/tenant-b/event/sensor.temp", func(_ string, payload []byte) {
		recvCh <- payload
	}); err != nil {
		t.Fatalf("subscribe target failed: %v", err)
	}

	if err := mock.Publish(context.Background(), "dd/tenant-a/event/sensor.temp", []byte(`{"val":42}`)); err != nil {
		t.Fatalf("publish source failed: %v", err)
	}

	select {
	case got := <-recvCh:
		if string(got) != `{"val":42}` {
			t.Fatalf("unexpected payload: %s", string(got))
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for forwarded message")
	}
}

func TestMqttBridgeExactTopic(t *testing.T) {
	mock := mq.NewMockClient()

	mappings := []MqttMapping{
		{Source: "dd/tenant-a/metrics", Target: "dd/tenant-b/metrics"},
	}

	bridge := NewMqttBridge(mock, mappings)
	if err := bridge.Start(context.Background()); err != nil {
		t.Fatalf("start mqtt bridge failed: %v", err)
	}

	recvCh := make(chan []byte, 1)
	if err := mock.Subscribe(context.Background(), "dd/tenant-b/metrics", func(_ string, payload []byte) {
		recvCh <- payload
	}); err != nil {
		t.Fatalf("subscribe target failed: %v", err)
	}

	if err := mock.Publish(context.Background(), "dd/tenant-a/metrics", []byte(`cpu:80`)); err != nil {
		t.Fatalf("publish source failed: %v", err)
	}

	select {
	case got := <-recvCh:
		if string(got) != "cpu:80" {
			t.Fatalf("unexpected payload: %s", string(got))
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for forwarded message")
	}
}

func TestMqttBridgeNonMatchingTopic(t *testing.T) {
	mock := mq.NewMockClient()

	mappings := []MqttMapping{
		{Source: "dd/tenant-a/event/#", Target: "dd/tenant-b/event/#"},
	}

	bridge := NewMqttBridge(mock, mappings)
	if err := bridge.Start(context.Background()); err != nil {
		t.Fatalf("start mqtt bridge failed: %v", err)
	}

	recvCh := make(chan []byte, 1)
	if err := mock.Subscribe(context.Background(), "dd/tenant-b/event/sensor", func(_ string, payload []byte) {
		recvCh <- payload
	}); err != nil {
		t.Fatalf("subscribe target failed: %v", err)
	}

	if err := mock.Publish(context.Background(), "dd/tenant-a/data/sensor", []byte(`ignored`)); err != nil {
		t.Fatalf("publish source failed: %v", err)
	}

	select {
	case <-recvCh:
		t.Fatal("should not receive message from non-matching topic")
	case <-time.After(200 * time.Millisecond):
	}
}
