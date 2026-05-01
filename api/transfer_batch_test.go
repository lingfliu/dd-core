package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dd-core/internal/config"
	"dd-core/internal/mq"
	"dd-core/internal/service"
)

func TestTransferAsyncBatchPartialSuccess(t *testing.T) {
	cfg := &config.Config{Tenant: "default", Peer: config.PeerConfig{Id: "edge-a"}}
	topics := service.NewDefaultTopicSet("default")
	mock := mq.NewMockClient()
	dataService := service.NewDdDataService(mock, time.Second)
	reg := service.NewPeerRegistryService(mock, topics, 30*time.Second)
	catalog := service.NewResourceCatalog()
	index := service.NewProtocolResourceIndex(cfg, topics, reg, catalog)
	srv := NewServer(cfg, topics, reg, dataService, nil, nil, catalog, index)

	body := map[string]any{
		"items": []map[string]any{
			{
				"resource":       "sensor.temp",
				"protocol":       "mq",
				"source_peer_id": "edge-a",
				"payload":        []byte(`{"v":1}`),
			},
			{
				"protocol": "mq",
				"payload":  []byte(`{"v":2}`),
			},
		},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/dd/api/v1/transfer/async/batch", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status=%d want=%d body=%s", rr.Code, http.StatusAccepted, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "partial" {
		t.Fatalf("status=%v want=partial", resp["status"])
	}
	if resp["accepted"].(float64) != 1 || resp["failed"].(float64) != 1 {
		t.Fatalf("unexpected accepted/failed: %+v", resp)
	}
}

func TestTransferAsyncBatchInvalidItems(t *testing.T) {
	cfg := &config.Config{Tenant: "default"}
	topics := service.NewDefaultTopicSet("default")
	mock := mq.NewMockClient()
	reg := service.NewPeerRegistryService(mock, topics, 30*time.Second)
	catalog := service.NewResourceCatalog()
	index := service.NewProtocolResourceIndex(cfg, topics, reg, catalog)
	srv := NewServer(cfg, topics, reg, nil, nil, nil, catalog, index)

	raw, _ := json.Marshal(map[string]any{"items": []any{}})
	req := httptest.NewRequest(http.MethodPost, "/dd/api/v1/transfer/async/batch", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}
