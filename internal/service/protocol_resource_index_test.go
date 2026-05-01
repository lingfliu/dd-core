package service

import (
	"testing"
	"time"

	"dd-core/internal/config"
	"dd-core/internal/model"
	"dd-core/internal/mq"
)

func TestProtocolResourceIndexListHTTPAndCoap(t *testing.T) {
	cfg := &config.Config{
		Peer: config.PeerConfig{Id: "edge-a"},
		Bridges: config.BridgeConfig{
			Http: config.HttpBridgeCfg{Enabled: true, TargetUrl: "http://svc.local"},
			Coap: config.CoapBridgeCfg{Enabled: true, TargetUrl: "coap://svc.local:5683"},
		},
	}
	topics := NewDefaultTopicSet("default")
	reg := NewPeerRegistryService(mq.NewMockClient(), topics, 30*time.Second)
	catalog := NewResourceCatalog()

	peer := model.DdPeerInfo{Id: "edge-a", Resources: []string{"sensor.temp", "sensor.humidity"}}
	reg.Store().Set(peer.Id, peer)
	catalog.OnPeerRegistered(peer)

	index := NewProtocolResourceIndex(cfg, topics, reg, catalog)

	httpItems := index.ListHTTP("")
	if len(httpItems) != 2 {
		t.Fatalf("expected 2 http items, got %d", len(httpItems))
	}
	if httpItems[0].RuntimeStats.QueryCount <= 0 {
		t.Fatalf("expected runtime stats query count > 0, got %+v", httpItems[0].RuntimeStats)
	}
	if httpItems[0].TargetURL != "http://svc.local" {
		t.Fatalf("unexpected http target url: %s", httpItems[0].TargetURL)
	}

	coapItems := index.ListCoap("sensor.temp")
	if len(coapItems) != 1 {
		t.Fatalf("expected 1 coap item, got %d", len(coapItems))
	}
	if coapItems[0].RuntimeStats.QueryCount <= 0 {
		t.Fatalf("expected coap runtime stats query count > 0, got %+v", coapItems[0].RuntimeStats)
	}
	if coapItems[0].Resource != "sensor.temp" {
		t.Fatalf("unexpected coap resource: %s", coapItems[0].Resource)
	}
}

func TestProtocolResourceIndexMqttTopicsFilters(t *testing.T) {
	cfg := &config.Config{
		Peer: config.PeerConfig{Id: "edge-a"},
		Bridges: config.BridgeConfig{
			MqttMappings: []config.MqttMappingCfg{
				{Source: "dd/a/event/in", Target: "dd/b/event/out"},
			},
		},
	}
	topics := NewDefaultTopicSet("default")
	reg := NewPeerRegistryService(mq.NewMockClient(), topics, 30*time.Second)
	index := NewProtocolResourceIndex(cfg, topics, reg, NewResourceCatalog())

	all := index.ListMqttTopics("both", "", -1)
	if len(all) == 0 {
		t.Fatal("expected mqtt topics")
	}

	pubOnly := index.ListMqttTopics("pub", "", -1)
	for _, item := range pubOnly {
		if item.Direction != "pub" {
			t.Fatalf("expected pub direction, got %s", item.Direction)
		}
	}

	topicItems := index.GetMqttTopic("dd/a/event/in")
	if len(topicItems) == 0 {
		t.Fatal("expected mapped mqtt topic")
	}
	if topicItems[0].RuntimeStats.QueryCount <= 0 {
		t.Fatalf("expected mqtt runtime stats query count > 0, got %+v", topicItems[0].RuntimeStats)
	}
}
