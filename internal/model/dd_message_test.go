package model

import (
	"testing"
	"time"
)

func TestDdMessageValidateSync(t *testing.T) {
	msg := &DdMessage{
		Mode:     DdTransferModeSync,
		Protocol: DdProtocolHttp,
		Resource: "order.query",
		Header: DdHeader{
			RequestId: "req-1",
			TimeoutMs: 1000,
		},
		CreatedAt: time.Now(),
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("expected valid sync message, got error: %v", err)
	}
}

func TestDdMessageValidateAsync(t *testing.T) {
	msg := &DdMessage{
		Mode:      DdTransferModeAsync,
		Protocol:  DdProtocolMq,
		Resource:  "iot.temp",
		CreatedAt: time.Now(),
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("expected valid async message, got error: %v", err)
	}
}

func TestDdMessageValidateSyncMissingRequestId(t *testing.T) {
	msg := &DdMessage{
		Mode:     DdTransferModeSync,
		Protocol: DdProtocolHttp,
		Resource: "order.query",
		Header: DdHeader{
			TimeoutMs: 1000,
		},
	}
	if err := msg.Validate(); err == nil {
		t.Fatal("expected sync validation to fail without request_id")
	}
}
