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

func TestDdMessageNormalizeMetaAndProtocolMeta(t *testing.T) {
	msg := &DdMessage{
		Mode:     DdTransferModeSync,
		Protocol: DdProtocolHttp,
		Resource: "order.query",
		Meta: DdHeader{
			RequestId: "req-meta-1",
			TimeoutMs: 1000,
		},
		ProtocolMeta: map[string]map[string]string{
			"http": {
				"method": "GET",
				"path":   "/api/v1/order/1",
			},
		},
		CreatedAt: time.Now(),
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("expected valid message after normalize: %v", err)
	}
	if msg.Header.RequestId != "req-meta-1" {
		t.Fatalf("expected header copied from meta, got %s", msg.Header.RequestId)
	}
	fields := msg.ProtocolFields()
	if fields["path"] != "/api/v1/order/1" {
		t.Fatalf("expected protocol field path, got %q", fields["path"])
	}
}

func TestDdMessageValidateRejectInvalidProtocolMetaField(t *testing.T) {
	msg := &DdMessage{
		Mode:     DdTransferModeAsync,
		Protocol: DdProtocolHttp,
		Resource: "order.query",
		ProtocolMeta: map[string]map[string]string{
			"http": {
				"method": "GET",
				"hack":   "x",
			},
		},
		CreatedAt: time.Now(),
	}
	if err := msg.Validate(); err == nil {
		t.Fatal("expected validation failure for invalid protocol_meta field")
	}
}
