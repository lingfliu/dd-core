package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
	"dd-core/internal/service"
)

func TestHttpBridgeSyncRequest(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, `{"echo":%q,"method":"%s"}`, string(body), r.Method)
	}))
	defer backend.Close()

	mock := mq.NewMockClient()
	topics := service.NewDefaultTopicSet("default")

	bridge := NewHttpBridge(mock, topics, "peer-a", backend.URL)
	if err := bridge.Start(context.Background()); err != nil {
		t.Fatalf("start bridge failed: %v", err)
	}

	responseCh := make(chan *model.DdMessage, 1)
	responseTopic := "dd/default/transfer/peer-a/response"
	if err := mock.Subscribe(context.Background(), responseTopic, func(_ string, payload []byte) {
		var msg model.DdMessage
		model.DecodeDdMessage(payload, &msg)
		responseCh <- &msg
	}); err != nil {
		t.Fatalf("subscribe response failed: %v", err)
	}

	syncReq := model.DdMessage{
		Mode:     model.DdTransferModeSync,
		Protocol: model.DdProtocolHttp,
		Resource: "test.echo",
		Header: model.DdHeader{
			RequestId:    "req-1",
			SourcePeerId: "peer-b",
			ReplyTo:      responseTopic,
		},
		Headers: map[string]string{
			"method": http.MethodPost,
			"path":   "/api/echo",
		},
		Payload: []byte(`{"msg":"hello"}`),
	}
	raw, _ := json.Marshal(syncReq)

	requestTopic := topics.TransferRequest("peer-a")
	if err := mock.Publish(context.Background(), requestTopic, raw); err != nil {
		t.Fatalf("publish request failed: %v", err)
	}

	select {
	case resp := <-responseCh:
		if resp.Header.CorrelationId != "req-1" {
			t.Fatalf("expected correlation_id=req-1, got %s", resp.Header.CorrelationId)
		}
		if resp.Headers["http-status"] != "200" {
			t.Fatalf("expected http-status=200, got %s", resp.Headers["http-status"])
		}
		if string(resp.Payload) == "" {
			t.Fatal("expected non-empty response payload")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for sync response")
	}
}

func TestHttpBridgeAsyncEvent(t *testing.T) {
	bodyCh := make(chan string, 1)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyCh <- string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	mock := mq.NewMockClient()
	topics := service.NewDefaultTopicSet("default")

	bridge := NewHttpBridge(mock, topics, "peer-x", backend.URL)
	if err := bridge.Start(context.Background()); err != nil {
		t.Fatalf("start bridge failed: %v", err)
	}

	asyncMsg := model.DdMessage{
		Mode:     model.DdTransferModeAsync,
		Protocol: model.DdProtocolHttp,
		Resource: "sensor.alert",
		Header: model.DdHeader{
			RequestId:    "async-1",
			SourcePeerId: "peer-y",
		},
		Headers: map[string]string{
			"method": http.MethodPost,
			"path":   "/alerts",
		},
		Payload: []byte(`{"level":"critical"}`),
	}
	raw, _ := json.Marshal(asyncMsg)

	eventTopic := topics.EventPublish("peer-x")
	if err := mock.Publish(context.Background(), eventTopic, raw); err != nil {
		t.Fatalf("publish event failed: %v", err)
	}

	select {
	case body := <-bodyCh:
		if body != `{"level":"critical"}` {
			t.Fatalf("unexpected body: %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for async delivery")
	}
}

func TestHttpBridgeProtocol(t *testing.T) {
	bridge := NewHttpBridge(mq.NewMockClient(), service.NewDefaultTopicSet("t"), "p1", "http://localhost")
	if bridge.Protocol() != model.DdProtocolHttp {
		t.Fatalf("expected http protocol, got %s", bridge.Protocol())
	}
}
