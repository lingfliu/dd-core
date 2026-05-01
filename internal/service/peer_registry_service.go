package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
	"dd-core/internal/observability"
	"dd-core/internal/registry"
)

type PeerRegistryService struct {
	mqClient   mq.Client
	topics     TopicSet
	leaseTtl   time.Duration
	store      registry.Store
	aclService *TopicAclService
	mu         sync.RWMutex
}

type PeerRegistryOption func(*PeerRegistryService)

func WithAclService(acl *TopicAclService) PeerRegistryOption {
	return func(s *PeerRegistryService) {
		s.aclService = acl
	}
}

func WithRegistryStore(store registry.Store) PeerRegistryOption {
	return func(s *PeerRegistryService) {
		s.store = store
	}
}

func NewPeerRegistryService(mqClient mq.Client, topics TopicSet, leaseTtl time.Duration, opts ...PeerRegistryOption) *PeerRegistryService {
	s := &PeerRegistryService{
		mqClient: mqClient,
		topics:   topics,
		leaseTtl: leaseTtl,
		store:    registry.NewMemoryStore(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *PeerRegistryService) Start(ctx context.Context) error {
	if err := s.mqClient.Subscribe(ctx, s.topics.PeerRegister, s.handleRegister); err != nil {
		return err
	}
	if err := s.mqClient.Subscribe(ctx, s.topics.PeerHeartbeat, s.handleHeartbeat); err != nil {
		return err
	}
	if err := s.mqClient.Subscribe(ctx, s.topics.PeerQuery, s.handleQuery); err != nil {
		return err
	}
	if err := s.mqClient.Subscribe(ctx, s.topics.PeerResourceReport, s.handleResourceReport); err != nil {
		return err
	}
	if err := s.mqClient.Subscribe(ctx, s.topics.PeerUnregister, s.handleUnregister); err != nil {
		return err
	}
	return nil
}

func (s *PeerRegistryService) handleRegister(_ string, payload []byte) {
	var evt model.DdPeerRegisterEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		return
	}
	now := time.Now().UTC()
	peer := evt.Peer
	peer.Status = model.PeerStatusRegistering
	peer.LastHeartbeatAt = now
	if peer.CreatedAt.IsZero() {
		peer.CreatedAt = now
	}
	peer.UpdatedAt = now

	s.store.Set(peer.Id, peer)
	slog.Info("peer registered", "peer_id", peer.Id, "role", peer.Role)
	s.syncPeerMetrics()
}

func (s *PeerRegistryService) handleHeartbeat(_ string, payload []byte) {
	var evt model.DdPeerHeartbeatEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		return
	}
	peer, ok := s.store.Get(evt.PeerId)
	if !ok {
		return
	}
	peer.LastHeartbeatAt = evt.Timestamp
	if peer.LastHeartbeatAt.IsZero() {
		peer.LastHeartbeatAt = time.Now().UTC()
	}
	peer.Status = model.PeerStatusActive
	peer.UpdatedAt = time.Now().UTC()
	s.store.Set(evt.PeerId, peer)
}

func (s *PeerRegistryService) handleResourceReport(_ string, payload []byte) {
	var evt model.DdPeerResourceReportEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		return
	}
	peer, ok := s.store.Get(evt.PeerId)
	if !ok {
		return
	}
	resources := make([]string, 0, len(evt.Resources.Apis)+len(evt.Resources.Topics)+len(evt.Resources.Streams))
	for _, api := range evt.Resources.Apis {
		if n := api["name"]; n != "" {
			resources = append(resources, n)
		}
	}
	for _, t := range evt.Resources.Topics {
		if n := t["name"]; n != "" {
			resources = append(resources, n)
		}
	}
	for _, st := range evt.Resources.Streams {
		if n := st["name"]; n != "" {
			resources = append(resources, n)
		}
	}
	peer.Resources = resources
	if len(evt.ResourceSchemas) > 0 {
		peer.ResourceSchemas = evt.ResourceSchemas
	}
	peer.UpdatedAt = time.Now().UTC()
	s.store.Set(evt.PeerId, peer)
}

func (s *PeerRegistryService) handleQuery(_ string, payload []byte) {
	var req model.DdPeerQueryRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return
	}
	peers := s.GetPeers(req.Resource)
	resp := model.DdPeerQueryResponse{
		RequestId: req.RequestId,
		Peers:     peers,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	replyTopic := req.ReplyTo
	if replyTopic == "" {
		return
	}
	_ = s.mqClient.Publish(context.Background(), replyTopic, data)
}

func (s *PeerRegistryService) handleUnregister(_ string, payload []byte) {
	var evt model.DdPeerHeartbeatEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		return
	}
	s.store.Delete(evt.PeerId)
}

func (s *PeerRegistryService) GetPeers(resource string) []model.DdPeerInfo {
	now := time.Now().UTC()
	allPeers := s.store.List()
	out := make([]model.DdPeerInfo, 0)
	for _, peer := range allPeers {
		if peer.Status == model.PeerStatusOffline || peer.Status == model.PeerStatusStale {
			continue
		}
		if !peer.LastHeartbeatAt.IsZero() && now.Sub(peer.LastHeartbeatAt) > s.leaseTtl {
			continue
		}
		if resource != "" && !containsString(peer.Resources, resource) {
			continue
		}
		out = append(out, peer)
	}
	return out
}

func (s *PeerRegistryService) SweepStale(now time.Time) {
	s.store.SweepStale(now, s.leaseTtl)
}

func (s *PeerRegistryService) RegisterPeer(ctx context.Context, peer model.DdPeerInfo) error {
	now := time.Now().UTC()
	if peer.CreatedAt.IsZero() {
		peer.CreatedAt = now
	}
	peer.Status = model.PeerStatusRegistering
	peer.LastHeartbeatAt = now
	peer.UpdatedAt = now

	s.store.Set(peer.Id, peer)

	evt := model.DdPeerRegisterEvent{Peer: peer}
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return s.mqClient.Publish(ctx, s.topics.PeerRegister, data)
}

func (s *PeerRegistryService) HeartbeatPeer(ctx context.Context, peerId string) error {
	evt := model.DdPeerHeartbeatEvent{
		PeerId:    peerId,
		Timestamp: time.Now().UTC(),
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return s.mqClient.Publish(ctx, s.topics.PeerHeartbeat, data)
}

func (s *PeerRegistryService) UnregisterPeer(ctx context.Context, peerId string) error {
	s.store.Delete(peerId)

	evt := model.DdPeerHeartbeatEvent{
		PeerId:    peerId,
		Timestamp: time.Now().UTC(),
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return s.mqClient.Publish(ctx, s.topics.PeerUnregister, data)
}

func (s *PeerRegistryService) QueryPeers(ctx context.Context, resource string, replyTo string) ([]model.DdPeerInfo, error) {
	req := model.DdPeerQueryRequest{
		RequestId: "q-" + time.Now().Format("20060102150405"),
		ReplyTo:   replyTo,
		Resource:  resource,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return nil, s.mqClient.Publish(ctx, s.topics.PeerQuery, data)
}

func (s *PeerRegistryService) Store() registry.Store {
	return s.store
}

func (s *PeerRegistryService) GetResourceMetaSchema(resource string) (model.ResourceMetaSchema, bool) {
	peers := s.GetPeers(resource)
	var merged model.ResourceMetaSchema
	merged.Constraints = make(map[string]model.ResourceMetaConstraint)
	seenRequired := make(map[string]struct{})
	seenOptional := make(map[string]struct{})
	seenProto := make(map[string]struct{})
	found := false

	for _, p := range peers {
		if !containsString(p.Capabilities, "resource_meta_schema_v1") {
			continue
		}
		if p.ResourceSchemas == nil {
			continue
		}
		if schema, ok := p.ResourceSchemas[resource]; ok {
			found = true
			for _, f := range schema.Required {
				if _, ok := seenRequired[f]; ok {
					continue
				}
				seenRequired[f] = struct{}{}
				merged.Required = append(merged.Required, f)
			}
			for _, f := range schema.Optional {
				if _, ok := seenOptional[f]; ok {
					continue
				}
				seenOptional[f] = struct{}{}
				merged.Optional = append(merged.Optional, f)
			}
			for _, f := range schema.ProtocolMetaRequired {
				if _, ok := seenProto[f]; ok {
					continue
				}
				seenProto[f] = struct{}{}
				merged.ProtocolMetaRequired = append(merged.ProtocolMetaRequired, f)
			}
			for field, c := range schema.Constraints {
				existing, exists := merged.Constraints[field]
				if !exists {
					merged.Constraints[field] = c
					continue
				}
				merged.Constraints[field] = mergeConstraint(existing, c)
			}
		}
	}
	if !found {
		return model.ResourceMetaSchema{}, false
	}
	return merged, true
}

func mergeConstraint(a, b model.ResourceMetaConstraint) model.ResourceMetaConstraint {
	out := a
	if b.Min != nil {
		if out.Min == nil || *b.Min > *out.Min {
			v := *b.Min
			out.Min = &v
		}
	}
	if b.Max != nil {
		if out.Max == nil || *b.Max < *out.Max {
			v := *b.Max
			out.Max = &v
		}
	}
	if len(a.Enum) > 0 && len(b.Enum) > 0 {
		set := make(map[string]struct{}, len(b.Enum))
		for _, v := range b.Enum {
			set[v] = struct{}{}
		}
		inter := make([]string, 0, len(a.Enum))
		for _, v := range a.Enum {
			if _, ok := set[v]; ok {
				inter = append(inter, v)
			}
		}
		out.Enum = inter
		return out
	}
	if len(out.Enum) == 0 && len(b.Enum) > 0 {
		out.Enum = append([]string(nil), b.Enum...)
	}
	return out
}

func (s *PeerRegistryService) syncPeerMetrics() {
	active := 0
	stale := 0
	for _, p := range s.store.List() {
		switch p.Status {
		case model.PeerStatusActive, model.PeerStatusRegistering:
			active++
		case model.PeerStatusStale:
			stale++
		}
	}
	observability.PeerActiveCount.Set(float64(active))
	observability.PeerStaleCount.Set(float64(stale))
}

func containsString(arr []string, v string) bool {
	for _, s := range arr {
		if s == v {
			return true
		}
	}
	return false
}
