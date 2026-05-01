package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"dd-core/internal/config"
	"dd-core/internal/mq"
	"dd-core/internal/service"
)

func TestTransferSyncDirectHTTPRoute(t *testing.T) {
	var hit int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		if r.URL.Path != "/echo" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Tenant: "default",
		Peer:   config.PeerConfig{Id: "edge-a"},
		Bridges: config.BridgeConfig{
			Http: config.HttpBridgeCfg{Enabled: true, TargetUrl: upstream.URL},
		},
	}
	topics := service.NewDefaultTopicSet("default")
	reg := service.NewPeerRegistryService(mq.NewMockClient(), topics, 30*time.Second)
	catalog := service.NewResourceCatalog()
	index := service.NewProtocolResourceIndex(cfg, topics, reg, catalog)
	srv := NewServer(cfg, topics, reg, nil, nil, nil, catalog, index)

	reqBody := struct {
		Resource     string                       `json:"resource"`
		Protocol     string                       `json:"protocol"`
		SourcePeerId string                       `json:"source_peer_id"`
		TargetPeerId string                       `json:"target_peer_id"`
		TimeoutMs    int64                        `json:"timeout_ms"`
		ProtocolMeta map[string]map[string]string `json:"protocol_meta"`
		Payload      []byte                       `json:"payload"`
	}{
		Resource:     "edge-a",
		Protocol:     "http",
		SourcePeerId: "client-a",
		TargetPeerId: "edge-a",
		TimeoutMs:    500,
		ProtocolMeta: map[string]map[string]string{
			"http": {"method": "POST", "path": "/echo"},
		},
		Payload: []byte(`{"hello":"world"}`),
	}
	raw, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/dd/api/v1/transfer/sync", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["route_mode"] != "direct" {
		t.Fatalf("route_mode=%v want direct", resp["route_mode"])
	}
	if atomic.LoadInt32(&hit) != 1 {
		t.Fatalf("upstream hit=%d want=1", hit)
	}
}

func TestTransferAsyncDirectHTTPRoute(t *testing.T) {
	var hit int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Tenant: "default",
		Peer:   config.PeerConfig{Id: "edge-a"},
		Bridges: config.BridgeConfig{
			Http: config.HttpBridgeCfg{Enabled: true, TargetUrl: upstream.URL},
		},
	}
	topics := service.NewDefaultTopicSet("default")
	reg := service.NewPeerRegistryService(mq.NewMockClient(), topics, 30*time.Second)
	catalog := service.NewResourceCatalog()
	index := service.NewProtocolResourceIndex(cfg, topics, reg, catalog)
	srv := NewServer(cfg, topics, reg, nil, nil, nil, catalog, index)

	reqBody := struct {
		Resource     string                       `json:"resource"`
		Protocol     string                       `json:"protocol"`
		SourcePeerId string                       `json:"source_peer_id"`
		TargetPeerId string                       `json:"target_peer_id"`
		ProtocolMeta map[string]map[string]string `json:"protocol_meta"`
		Payload      []byte                       `json:"payload"`
	}{
		Resource:     "edge-a",
		Protocol:     "http",
		SourcePeerId: "client-a",
		TargetPeerId: "edge-a",
		ProtocolMeta: map[string]map[string]string{
			"http": {"method": "POST", "path": "/event"},
		},
		Payload: []byte(`{"event":"temp"}`),
	}
	raw, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/dd/api/v1/transfer/async", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status=%d want=%d body=%s", rr.Code, http.StatusAccepted, rr.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["route_mode"] != "direct" {
		t.Fatalf("route_mode=%v want direct", resp["route_mode"])
	}
	if atomic.LoadInt32(&hit) != 1 {
		t.Fatalf("upstream hit=%d want=1", hit)
	}
}
