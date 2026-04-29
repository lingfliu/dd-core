package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
)

func TestIdempotencyCacheHit(t *testing.T) {
	mock := mq.NewMockClient()
	svc := NewDdDataService(mock, 500*time.Millisecond)

	responseTopic := "dd/default/transfer/order.query/response"
	if err := svc.SubscribeSyncResponses(context.Background(), responseTopic); err != nil {
		t.Fatalf("subscribe responses failed: %v", err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		resp := model.DdMessage{
			Mode:     model.DdTransferModeSync,
			Protocol: model.DdProtocolHttp,
			Resource: "order.query",
			Header: model.DdHeader{
				CorrelationId: "dedup-req-1",
			},
			Payload: []byte(`{"ok":true}`),
		}
		raw, _ := json.Marshal(resp)
		mock.Emit(responseTopic, raw)
	}()

	req := &model.DdMessage{
		Protocol: model.DdProtocolHttp,
		Resource: "order.query",
		Header: model.DdHeader{
			RequestId:      "dedup-req-1",
			TimeoutMs:      1000,
			IdempotencyKey: "key-001",
		},
	}

	resp1, err := svc.SendSync(context.Background(), "dd/default/transfer/order.query/request", req)
	if err != nil {
		t.Fatalf("first SendSync failed: %v", err)
	}
	if string(resp1.Payload) != `{"ok":true}` {
		t.Fatalf("unexpected payload: %s", string(resp1.Payload))
	}

	req2 := &model.DdMessage{
		Protocol: model.DdProtocolHttp,
		Resource: "order.query",
		Header: model.DdHeader{
			RequestId:      "dedup-req-2",
			TimeoutMs:      10,
			IdempotencyKey: "key-001",
		},
	}

	resp2, err := svc.SendSync(context.Background(), "dd/default/transfer/order.query/request", req2)
	if err != nil {
		t.Fatalf("cached SendSync failed: %v", err)
	}
	if string(resp2.Payload) != `{"ok":true}` {
		t.Fatalf("cached response mismatch: %s", string(resp2.Payload))
	}
}

func TestIdempotencyNoKey(t *testing.T) {
	mock := mq.NewMockClient()
	svc := NewDdDataService(mock, 500*time.Millisecond)

	responseTopic := "dd/default/transfer/test/response"
	svc.SubscribeSyncResponses(context.Background(), responseTopic)

	go func() {
		time.Sleep(20 * time.Millisecond)
		resp := model.DdMessage{
			Mode:     model.DdTransferModeSync,
			Protocol: model.DdProtocolHttp,
			Resource: "test",
			Header: model.DdHeader{
				CorrelationId: "no-key-1",
			},
			Payload: []byte(`ok`),
		}
		raw, _ := json.Marshal(resp)
		mock.Emit(responseTopic, raw)
	}()

	req := &model.DdMessage{
		Protocol: model.DdProtocolHttp,
		Resource: "test",
		Header: model.DdHeader{
			RequestId: "no-key-1",
			TimeoutMs: 1000,
		},
	}

	_, err := svc.SendSync(context.Background(), "dd/default/transfer/test/request", req)
	if err != nil {
		t.Fatalf("SendSync failed: %v", err)
	}

	req2 := &model.DdMessage{
		Protocol: model.DdProtocolHttp,
		Resource: "test",
		Header: model.DdHeader{
			RequestId: "no-key-2",
			TimeoutMs: 10,
		},
	}

	_, err = svc.SendSync(context.Background(), "dd/default/transfer/test/request", req2)
	if err == nil {
		t.Fatal("expected timeout for non-idempotent duplicate")
	}
}

func TestTransferStatusTracking(t *testing.T) {
	mock := mq.NewMockClient()
	svc := NewDdDataService(mock, 500*time.Millisecond)

	responseTopic := "dd/default/transfer/status-test/response"
	svc.SubscribeSyncResponses(context.Background(), responseTopic)

	go func() {
		time.Sleep(20 * time.Millisecond)
		resp := model.DdMessage{
			Mode:     model.DdTransferModeSync,
			Protocol: model.DdProtocolHttp,
			Resource: "status-test",
			Header: model.DdHeader{
				CorrelationId: "status-req-1",
			},
			Payload: []byte(`done`),
		}
		raw, _ := json.Marshal(resp)
		mock.Emit(responseTopic, raw)
	}()

	req := &model.DdMessage{
		Protocol: model.DdProtocolHttp,
		Resource: "status-test",
		Header: model.DdHeader{
			RequestId:    "status-req-1",
			SourcePeerId: "peer-a",
			TargetPeerId: "peer-b",
			TimeoutMs:    1000,
		},
	}

	_, err := svc.SendSync(context.Background(), "dd/default/transfer/status-test/request", req)
	if err != nil {
		t.Fatalf("SendSync failed: %v", err)
	}

	rec, ok := svc.GetTransferStatus("status-req-1")
	if !ok {
		t.Fatal("expected transfer record to exist")
	}
	if rec.Status != model.TransferStatusResponded {
		t.Fatalf("expected responded, got %s", rec.Status)
	}
	if rec.SourcePeerId != "peer-a" {
		t.Fatalf("expected source peer-a, got %s", rec.SourcePeerId)
	}
}

func TestTransferStatusTimeout(t *testing.T) {
	mock := mq.NewMockClient()
	svc := NewDdDataService(mock, 50*time.Millisecond)

	req := &model.DdMessage{
		Protocol: model.DdProtocolHttp,
		Resource: "timeout-test",
		Header: model.DdHeader{
			RequestId: "timeout-1",
			TimeoutMs: 10,
		},
	}

	_, err := svc.SendSync(context.Background(), "dd/default/transfer/timeout-test/request", req)
	if err == nil {
		t.Fatal("expected timeout")
	}

	rec, ok := svc.GetTransferStatus("timeout-1")
	if !ok {
		t.Fatal("expected timeout record to exist")
	}
	if rec.Status != model.TransferStatusTimeout {
		t.Fatalf("expected timeout, got %s", rec.Status)
	}
}

func TestTransferStatusNotFound(t *testing.T) {
	svc := NewDdDataService(mq.NewMockClient(), time.Second)
	_, ok := svc.GetTransferStatus("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestAsyncTransferStatus(t *testing.T) {
	mock := mq.NewMockClient()
	svc := NewDdDataService(mock, time.Second)

	msg := &model.DdMessage{
		Protocol: model.DdProtocolMq,
		Resource: "async-status",
		Header: model.DdHeader{
			RequestId: "async-status-1",
		},
	}
	if err := svc.SendAsync(context.Background(), "dd/default/event/async-status/publish", msg); err != nil {
		t.Fatalf("SendAsync failed: %v", err)
	}

	rec, ok := svc.GetTransferStatus("async-status-1")
	if !ok {
		t.Fatal("expected async record to exist")
	}
	if rec.Status != model.TransferStatusAccepted {
		t.Fatalf("expected accepted, got %s", rec.Status)
	}
}
