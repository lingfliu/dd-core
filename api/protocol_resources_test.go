package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"dd-core/internal/config"
	"dd-core/internal/model"
	"dd-core/internal/mq"
	"dd-core/internal/service"
)

func TestProtocolResourceEndpoints(t *testing.T) {
	cfg := &config.Config{
		Tenant: "default",
		Peer:   config.PeerConfig{Id: "edge-a"},
		Bridges: config.BridgeConfig{
			Http: config.HttpBridgeCfg{Enabled: true, TargetUrl: "http://svc.local"},
			Coap: config.CoapBridgeCfg{Enabled: true, TargetUrl: "coap://svc.local:5683"},
			MqttMappings: []config.MqttMappingCfg{
				{Source: "dd/default/event/in", Target: "dd/default/event/out"},
			},
		},
	}
	topics := service.NewDefaultTopicSet("default")
	reg := service.NewPeerRegistryService(mq.NewMockClient(), topics, 30*time.Second)
	catalog := service.NewResourceCatalog()

	peer := model.DdPeerInfo{Id: "edge-a", Resources: []string{"sensor.temp"}}
	peer.Capabilities = []string{"resource_meta_schema_v1"}
	peer.ResourceSchemas = map[string]model.ResourceMetaSchema{
		"sensor.temp": {
			Required:             []string{"source_peer_id"},
			ProtocolMetaRequired: []string{"method"},
		},
	}
	reg.Store().Set(peer.Id, peer)
	catalog.OnPeerRegistered(peer)

	index := service.NewProtocolResourceIndex(cfg, topics, reg, catalog)
	srv := NewServer(cfg, topics, reg, nil, nil, nil, catalog, index)

	testGET := func(path string, wantStatus int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
		if rr.Code != wantStatus {
			t.Fatalf("GET %s status=%d want=%d body=%s", path, rr.Code, wantStatus, rr.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return body
	}

	httpBody := testGET("/dd/api/v1/protocol-resources/http", http.StatusOK)
	if httpBody["protocol"] != "http" {
		t.Fatalf("unexpected protocol: %v", httpBody["protocol"])
	}
	items, ok := httpBody["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("expected non-empty http items, got %v", httpBody["items"])
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected first item: %T", items[0])
	}
	if _, ok := first["request_meta_schema"]; !ok {
		t.Fatal("expected request_meta_schema in protocol resources response")
	}
	if stats, ok := first["runtime_stats"].(map[string]any); !ok || stats["query_count"] == nil {
		t.Fatal("expected runtime_stats.query_count in protocol resources response")
	}

	testGET("/dd/api/v1/protocol-resources/http/sensor.temp", http.StatusOK)
	testGET("/dd/api/v1/protocol-resources/coap", http.StatusOK)
	testGET("/dd/api/v1/protocol-resources/mqtt/topics", http.StatusOK)
	testGET("/dd/api/v1/protocol-resources/mqtt/topics/"+url.PathEscape("dd/default/event/in"), http.StatusOK)
	testGET("/dd/api/v1/resources/sensor.temp/meta-schema", http.StatusOK)
}

func TestMqttTopicsQoSValidation(t *testing.T) {
	cfg := &config.Config{
		Tenant: "default",
		Peer:   config.PeerConfig{Id: "edge-a"},
	}
	topics := service.NewDefaultTopicSet("default")
	reg := service.NewPeerRegistryService(mq.NewMockClient(), topics, 30*time.Second)
	catalog := service.NewResourceCatalog()
	index := service.NewProtocolResourceIndex(cfg, topics, reg, catalog)
	srv := NewServer(cfg, topics, reg, nil, nil, nil, catalog, index)

	req := httptest.NewRequest(http.MethodGet, "/dd/api/v1/protocol-resources/mqtt/topics?qos=2", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}
