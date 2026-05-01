package model

import "testing"

func TestDdMessageCodecPlainAndCompressed(t *testing.T) {
	msg := &DdMessage{
		Mode:     DdTransferModeSync,
		Protocol: DdProtocolHttp,
		Resource: "sensor.temp",
		Header: DdHeader{
			RequestId: "req-1",
			TimeoutMs: 1000,
		},
		Payload: []byte(`{"temperature":23.5}`),
	}

	plain, err := EncodeDdMessage(msg, false)
	if err != nil {
		t.Fatalf("encode plain failed: %v", err)
	}
	var plainOut DdMessage
	if err := DecodeDdMessage(plain, &plainOut); err != nil {
		t.Fatalf("decode plain failed: %v", err)
	}
	if string(plainOut.Payload) != string(msg.Payload) {
		t.Fatalf("plain payload mismatch: %s", string(plainOut.Payload))
	}

	compressed, err := EncodeDdMessage(msg, true)
	if err != nil {
		t.Fatalf("encode compressed failed: %v", err)
	}
	var gzOut DdMessage
	if err := DecodeDdMessage(compressed, &gzOut); err != nil {
		t.Fatalf("decode compressed failed: %v", err)
	}
	if string(gzOut.Payload) != string(msg.Payload) {
		t.Fatalf("compressed payload mismatch: %s", string(gzOut.Payload))
	}
}
