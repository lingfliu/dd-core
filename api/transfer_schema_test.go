package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dd-core/internal/config"
	"dd-core/internal/model"
	"dd-core/internal/mq"
	"dd-core/internal/service"
)

func TestTransferSyncRejectsMissingRequiredSchemaMeta(t *testing.T) {
	cfg := &config.Config{Tenant: "default"}
	topics := service.NewDefaultTopicSet("default")
	reg := service.NewPeerRegistryService(mq.NewMockClient(), topics, 30*time.Second)
	catalog := service.NewResourceCatalog()
	index := service.NewProtocolResourceIndex(cfg, topics, reg, catalog)

	peer := model.DdPeerInfo{
		Id:           "provider-1",
		Role:         "edge",
		Status:       model.PeerStatusActive,
		Resources:    []string{"sensor.temp"},
		Capabilities: []string{"resource_meta_schema_v1"},
		ResourceSchemas: map[string]model.ResourceMetaSchema{
			"sensor.temp": {
				Required: []string{"source_peer_id"},
			},
		},
		LastHeartbeatAt: time.Now().UTC(),
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	reg.Store().Set(peer.Id, peer)
	catalog.OnPeerRegistered(peer)

	srv := NewServer(cfg, topics, reg, nil, nil, nil, catalog, index)
	body := map[string]any{
		"resource": "sensor.temp",
		"protocol": "http",
		"payload":  []byte(`{}`),
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/dd/api/v1/transfer/sync", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}
