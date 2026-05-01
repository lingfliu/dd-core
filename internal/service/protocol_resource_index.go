package service

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"dd-core/internal/config"
	"dd-core/internal/model"
)

type ProtocolResourceIndex struct {
	cfg             *config.Config
	topics          TopicSet
	peerRegistry    *PeerRegistryService
	resourceCatalog *ResourceCatalog
	statsMu         sync.Mutex
	stats           map[string]RuntimeStats
}

type RuntimeStats struct {
	QueryCount    int64     `json:"query_count"`
	LastQueriedAt time.Time `json:"last_queried_at"`
}

type HTTPResourceItem struct {
	Resource          string             `json:"resource"`
	Providers         []model.DdPeerInfo `json:"providers"`
	TargetURL         string             `json:"target_url,omitempty"`
	RequestMetaSchema map[string]any     `json:"request_meta_schema,omitempty"`
	RuntimeStats      RuntimeStats       `json:"runtime_stats"`
}

type CoapResourceItem struct {
	Resource          string             `json:"resource"`
	Providers         []model.DdPeerInfo `json:"providers"`
	TargetURL         string             `json:"target_url,omitempty"`
	RequestMetaSchema map[string]any     `json:"request_meta_schema,omitempty"`
	RuntimeStats      RuntimeStats       `json:"runtime_stats"`
}

type MqttTopicItem struct {
	Topic             string         `json:"topic"`
	Direction         string         `json:"direction"`
	QosDefault        int            `json:"qos_default"`
	Retain            bool           `json:"retain"`
	Providers         []string       `json:"providers,omitempty"`
	Consumers         []string       `json:"consumers,omitempty"`
	RequestMetaSchema map[string]any `json:"request_meta_schema,omitempty"`
	RuntimeStats      RuntimeStats   `json:"runtime_stats"`
}

func NewProtocolResourceIndex(
	cfg *config.Config,
	topics TopicSet,
	peerRegistry *PeerRegistryService,
	resourceCatalog *ResourceCatalog,
) *ProtocolResourceIndex {
	return &ProtocolResourceIndex{
		cfg:             cfg,
		topics:          topics,
		peerRegistry:    peerRegistry,
		resourceCatalog: resourceCatalog,
		stats:           make(map[string]RuntimeStats),
	}
}

func (i *ProtocolResourceIndex) ListHTTP(resource string) []HTTPResourceItem {
	if i.cfg.Bridges.Http.TargetUrl == "" {
		return []HTTPResourceItem{}
	}
	resources := i.resourceCatalog.ListResources()
	if resource != "" {
		resources = []string{resource}
	}
	sort.Strings(resources)
	out := make([]HTTPResourceItem, 0, len(resources))
	for _, r := range resources {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		providerIDs := i.resourceCatalog.GetProviders(r)
		if len(providerIDs) == 0 {
			continue
		}
		out = append(out, HTTPResourceItem{
			Resource:          r,
			Providers:         i.lookupPeers(providerIDs),
			TargetURL:         i.cfg.Bridges.Http.TargetUrl,
			RequestMetaSchema: i.lookupSchemaMap(r),
			RuntimeStats:      i.bumpStat("http/" + r),
		})
	}
	return out
}

func (i *ProtocolResourceIndex) ListCoap(resource string) []CoapResourceItem {
	if i.cfg.Bridges.Coap.TargetUrl == "" {
		return []CoapResourceItem{}
	}
	resources := i.resourceCatalog.ListResources()
	if resource != "" {
		resources = []string{resource}
	}
	sort.Strings(resources)
	out := make([]CoapResourceItem, 0, len(resources))
	for _, r := range resources {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		providerIDs := i.resourceCatalog.GetProviders(r)
		if len(providerIDs) == 0 {
			continue
		}
		out = append(out, CoapResourceItem{
			Resource:          r,
			Providers:         i.lookupPeers(providerIDs),
			TargetURL:         i.cfg.Bridges.Coap.TargetUrl,
			RequestMetaSchema: i.lookupSchemaMap(r),
			RuntimeStats:      i.bumpStat("coap/" + r),
		})
	}
	return out
}

func (i *ProtocolResourceIndex) ListMqttTopics(direction, peerID string, qos int) []MqttTopicItem {
	type key struct {
		topic     string
		direction string
	}
	items := map[key]MqttTopicItem{}

	add := func(item MqttTopicItem) {
		k := key{topic: item.Topic, direction: item.Direction}
		items[k] = item
	}

	peer := i.cfg.Peer.Id
	if peerID != "" {
		peer = peerID
	}

	// Core topics that are always meaningful for resource governance.
	core := []string{
		i.topics.PeerRegister,
		i.topics.PeerHeartbeat,
		i.topics.PeerQuery,
		i.topics.PeerResourceReport,
		i.topics.PeerUnregister,
		i.topics.PeerResourceRegister,
		i.topics.PeerAuthVerify,
		i.topics.PeerListQuery,
	}
	for _, t := range core {
		add(MqttTopicItem{
			Topic:      t,
			Direction:  "pub",
			QosDefault: 1,
			Providers:  []string{peer},
		})
		add(MqttTopicItem{
			Topic:      t,
			Direction:  "sub",
			QosDefault: 1,
			Consumers:  []string{peer},
		})
	}

	for _, m := range i.cfg.Bridges.MqttMappings {
		add(MqttTopicItem{
			Topic:      m.Source,
			Direction:  "sub",
			QosDefault: 1,
			Consumers:  []string{peer},
		})
		add(MqttTopicItem{
			Topic:      m.Target,
			Direction:  "pub",
			QosDefault: 1,
			Providers:  []string{peer},
		})
	}

	out := make([]MqttTopicItem, 0, len(items))
	for _, item := range items {
		if direction != "" && direction != "both" && item.Direction != direction {
			continue
		}
		if qos >= 0 && item.QosDefault != qos {
			continue
		}
		item.RuntimeStats = i.bumpStat("mqtt/" + item.Topic + "/" + item.Direction)
		out = append(out, item)
	}

	sort.Slice(out, func(a, b int) bool {
		if out[a].Topic == out[b].Topic {
			return out[a].Direction < out[b].Direction
		}
		return out[a].Topic < out[b].Topic
	})
	return out
}

func (i *ProtocolResourceIndex) GetMqttTopic(name string) []MqttTopicItem {
	all := i.ListMqttTopics("both", "", -1)
	out := make([]MqttTopicItem, 0, 2)
	for _, item := range all {
		if item.Topic == name {
			out = append(out, item)
		}
	}
	return out
}

func (i *ProtocolResourceIndex) lookupPeers(peerIDs []string) []model.DdPeerInfo {
	out := make([]model.DdPeerInfo, 0, len(peerIDs))
	for _, id := range peerIDs {
		peer, ok := i.peerRegistry.Store().Get(id)
		if ok {
			out = append(out, peer)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Id < out[b].Id })
	return out
}

func (i *ProtocolResourceIndex) lookupSchemaMap(resource string) map[string]any {
	schema, ok := i.peerRegistry.GetResourceMetaSchema(resource)
	if !ok {
		return nil
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func (i *ProtocolResourceIndex) bumpStat(key string) RuntimeStats {
	i.statsMu.Lock()
	defer i.statsMu.Unlock()
	s := i.stats[key]
	s.QueryCount++
	s.LastQueriedAt = time.Now().UTC()
	i.stats[key] = s
	return s
}
