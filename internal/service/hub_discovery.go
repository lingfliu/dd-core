package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
)

type HubDiscoveryService struct {
	mqClient mq.Client
	topics   TopicSet
	peerId   string
	peerName string
	role     string
	interval time.Duration
	ttlSec   int
	stopCh   chan struct{}

	mu         sync.RWMutex
	activeHubs map[string]model.HubBroadcast
	activeAuth map[string]model.AuthBroadcast

	onHubDiscovered func(model.HubBroadcast)
	onAuthDiscovered func(model.AuthBroadcast)
}

func NewHubDiscoveryService(
	mqClient mq.Client,
	topics TopicSet,
	peerId string,
	peerName string,
	role string,
	interval time.Duration,
	ttlSec int,
) *HubDiscoveryService {
	return &HubDiscoveryService{
		mqClient:    mqClient,
		topics:      topics,
		peerId:      peerId,
		peerName:    peerName,
		role:        role,
		interval:    interval,
		ttlSec:      ttlSec,
		stopCh:      make(chan struct{}),
		activeHubs:  make(map[string]model.HubBroadcast),
		activeAuth:  make(map[string]model.AuthBroadcast),
	}
}

func (s *HubDiscoveryService) OnHubDiscovered(fn func(model.HubBroadcast)) {
	s.onHubDiscovered = fn
}

func (s *HubDiscoveryService) OnAuthDiscovered(fn func(model.AuthBroadcast)) {
	s.onAuthDiscovered = fn
}

func (s *HubDiscoveryService) Start(ctx context.Context) error {
	if err := s.mqClient.Subscribe(ctx, s.topics.HubBroadcast, s.handleHubBroadcast); err != nil {
		return err
	}
	if err := s.mqClient.Subscribe(ctx, s.topics.AuthBroadcast, s.handleAuthBroadcast); err != nil {
		return err
	}

	if s.role == "hub" || s.role == "edge_hub" {
		go s.startBroadcastLoop()
	}

	slog.Info("hub discovery service started",
		"peer_id", s.peerId,
		"role", s.role,
		"interval", s.interval,
	)
	return nil
}

func (s *HubDiscoveryService) Stop() {
	close(s.stopCh)
}

func (s *HubDiscoveryService) startBroadcastLoop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.broadcast()
		}
	}
}

func (s *HubDiscoveryService) broadcast() {
	msg := model.HubBroadcast{
		HubId:         s.peerId,
		HubName:       s.peerName,
		RegisterTopic: s.topics.PeerResourceRegister,
		AuthTopic:     s.topics.PeerAuthVerify,
		QueryTopic:    s.topics.PeerListQuery,
		TtlSec:        s.ttlSec,
		Timestamp:     time.Now().UTC(),
	}
	data, _ := json.Marshal(msg)
	if err := s.mqClient.Publish(context.Background(), s.topics.HubBroadcast, data); err != nil {
		slog.Warn("hub broadcast failed", "error", err)
	}
}

func (s *HubDiscoveryService) BroadcastAuth(verifyTopic, publicKey string) {
	msg := model.AuthBroadcast{
		AuthId:      s.peerId,
		AuthName:    s.peerName,
		VerifyTopic: verifyTopic,
		PublicKey:   publicKey,
		TtlSec:      s.ttlSec,
		Timestamp:   time.Now().UTC(),
	}
	data, _ := json.Marshal(msg)
	if err := s.mqClient.Publish(context.Background(), s.topics.AuthBroadcast, data); err != nil {
		slog.Warn("auth broadcast failed", "error", err)
	}
}

func (s *HubDiscoveryService) handleHubBroadcast(_ string, payload []byte) {
	var msg model.HubBroadcast
	if err := json.Unmarshal(payload, &msg); err != nil {
		return
	}

	s.mu.Lock()
	s.activeHubs[msg.HubId] = msg
	s.mu.Unlock()

	if s.onHubDiscovered != nil {
		s.onHubDiscovered(msg)
	}
}

func (s *HubDiscoveryService) handleAuthBroadcast(_ string, payload []byte) {
	var msg model.AuthBroadcast
	if err := json.Unmarshal(payload, &msg); err != nil {
		return
	}

	s.mu.Lock()
	s.activeAuth[msg.AuthId] = msg
	s.mu.Unlock()

	if s.onAuthDiscovered != nil {
		s.onAuthDiscovered(msg)
	}
}

func (s *HubDiscoveryService) GetActiveHub() (model.HubBroadcast, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now().UTC()
	for _, h := range s.activeHubs {
		if now.Sub(h.Timestamp) <= time.Duration(h.TtlSec)*time.Second {
			return h, true
		}
	}
	return model.HubBroadcast{}, false
}

func (s *HubDiscoveryService) GetActiveAuth() (model.AuthBroadcast, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now().UTC()
	for _, a := range s.activeAuth {
		if now.Sub(a.Timestamp) <= time.Duration(a.TtlSec)*time.Second {
			return a, true
		}
	}
	return model.AuthBroadcast{}, false
}

func (s *HubDiscoveryService) ListActiveHubs() []model.HubBroadcast {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now().UTC()
	var result []model.HubBroadcast
	for _, h := range s.activeHubs {
		if now.Sub(h.Timestamp) <= time.Duration(h.TtlSec)*time.Second {
			result = append(result, h)
		}
	}
	return result
}
