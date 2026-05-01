package api

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"dd-core/internal/config"
	"dd-core/internal/mq"
	"dd-core/internal/service"
)

func TestTransferSyncDirectCoapRoute(t *testing.T) {
	addr, hit, stop := startTestCoapServer(t, []byte("coap-ok"))
	defer stop()

	cfg := &config.Config{
		Tenant: "default",
		Peer:   config.PeerConfig{Id: "edge-a"},
		Bridges: config.BridgeConfig{
			Coap: config.CoapBridgeCfg{Enabled: true, TargetUrl: "coap://" + addr},
		},
	}
	topics := service.NewDefaultTopicSet("default")
	reg := service.NewPeerRegistryService(mq.NewMockClient(), topics, 30*time.Second)
	catalog := service.NewResourceCatalog()
	index := service.NewProtocolResourceIndex(cfg, topics, reg, catalog)
	srv := NewServer(cfg, topics, reg, nil, nil, nil, catalog, index)

	reqBody := map[string]any{
		"resource":       "edge-a",
		"protocol":       "coap",
		"source_peer_id": "client-a",
		"target_peer_id": "edge-a",
		"timeout_ms":     500,
		"protocol_meta": map[string]map[string]string{
			"coap": {"method": "GET", "path": "/d"},
		},
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
		t.Fatalf("route_mode=%v want=direct", resp["route_mode"])
	}
	if atomic.LoadInt32(hit) != 1 {
		t.Fatalf("hit=%d want=1", atomic.LoadInt32(hit))
	}
}

func TestTransferAsyncDirectCoapRoute(t *testing.T) {
	addr, hit, stop := startTestCoapServer(t, []byte("coap-async-ok"))
	defer stop()

	cfg := &config.Config{
		Tenant: "default",
		Peer:   config.PeerConfig{Id: "edge-a"},
		Bridges: config.BridgeConfig{
			Coap: config.CoapBridgeCfg{Enabled: true, TargetUrl: "coap://" + addr},
		},
	}
	topics := service.NewDefaultTopicSet("default")
	reg := service.NewPeerRegistryService(mq.NewMockClient(), topics, 30*time.Second)
	catalog := service.NewResourceCatalog()
	index := service.NewProtocolResourceIndex(cfg, topics, reg, catalog)
	srv := NewServer(cfg, topics, reg, nil, nil, nil, catalog, index)

	reqBody := map[string]any{
		"resource":       "edge-a",
		"protocol":       "coap",
		"source_peer_id": "client-a",
		"target_peer_id": "edge-a",
		"protocol_meta": map[string]map[string]string{
			"coap": {"method": "POST", "path": "/evt"},
		},
		"payload": []byte("x"),
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
		t.Fatalf("route_mode=%v want=direct", resp["route_mode"])
	}
	if atomic.LoadInt32(hit) != 1 {
		t.Fatalf("hit=%d want=1", atomic.LoadInt32(hit))
	}
}

func startTestCoapServer(t *testing.T, payload []byte) (string, *int32, func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	var hit int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		if n < 4 {
			return
		}
		atomic.AddInt32(&hit, 1)
		msgIDHi := buf[2]
		msgIDLo := buf[3]
		resp := []byte{0x60, 0x45, msgIDHi, msgIDLo, 0xff}
		resp = append(resp, payload...)
		_, _ = pc.WriteTo(resp, addr)
	}()
	stop := func() {
		_ = pc.Close()
		<-done
	}
	return pc.LocalAddr().String(), &hit, stop
}
