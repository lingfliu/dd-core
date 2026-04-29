package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"dd-core/internal/model"
	"dd-core/internal/mq"
)

type PeerListService struct {
	mqClient   mq.Client
	topics     TopicSet
	peerRegistry *PeerRegistryService
	aclService *TopicAclService
	maxPageSize int
}

func NewPeerListService(mqClient mq.Client, topics TopicSet, peerRegistry *PeerRegistryService, aclService *TopicAclService, maxPageSize int) *PeerListService {
	if maxPageSize <= 0 {
		maxPageSize = 100
	}
	return &PeerListService{
		mqClient:     mqClient,
		topics:       topics,
		peerRegistry: peerRegistry,
		aclService:   aclService,
		maxPageSize:  maxPageSize,
	}
}

func (s *PeerListService) Start(ctx context.Context) error {
	if err := s.mqClient.Subscribe(ctx, s.topics.PeerListQuery, s.handleQuery); err != nil {
		return fmt.Errorf("subscribe peer list query topic: %w", err)
	}
	slog.Info("peer list service started", "query_topic", s.topics.PeerListQuery)
	return nil
}

func (s *PeerListService) handleQuery(_ string, payload []byte) {
	var req model.PeerListQuery
	if err := json.Unmarshal(payload, &req); err != nil {
		slog.Warn("failed to unmarshal peer list query", "error", err)
		return
	}

	allPeers := s.peerRegistry.GetPeers(req.ResourceFilter)

	filtered := s.filterByAcl(req.SourcePeerId, allPeers)
	filtered = s.filterByRole(filtered, req.RoleFilter)
	filtered = s.filterByStatus(filtered, req.StatusFilter)

	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > s.maxPageSize {
		pageSize = 20
	}
	offset := req.PageOffset
	if offset < 0 {
		offset = 0
	}

	totalCount := len(filtered)
	hasMore := false
	if offset+pageSize < totalCount {
		hasMore = true
		filtered = filtered[offset : offset+pageSize]
	} else if offset < totalCount {
		filtered = filtered[offset:]
	} else {
		filtered = nil
	}

	resp := model.PeerListResponse{
		RequestId:     req.RequestId,
		CorrelationId: req.RequestId,
		Peers:         filtered,
		TotalCount:    totalCount,
		PageOffset:    offset,
		HasMore:       hasMore,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		slog.Warn("failed to marshal peer list response", "error", err)
		return
	}

	if req.ReplyTo != "" {
		if err := s.mqClient.Publish(context.Background(), req.ReplyTo, data); err != nil {
			slog.Warn("failed to publish peer list response", "error", err, "reply_to", req.ReplyTo)
		}
	}
}

func (s *PeerListService) filterByAcl(callerId string, peers []model.DdPeerInfo) []model.DdPeerInfo {
	if s.aclService == nil || callerId == "" {
		return peers
	}

	result := make([]model.DdPeerInfo, 0, len(peers))
	for _, peer := range peers {
		if s.aclService.Authorize(callerId, AclActionSub, s.topics.TransferRequest(peer.Id)) ||
			s.aclService.Authorize(callerId, AclActionPub, s.topics.TransferRequest(peer.Id)) {
			result = append(result, peer)
		}
	}
	return result
}

func (s *PeerListService) filterByRole(peers []model.DdPeerInfo, roleFilter string) []model.DdPeerInfo {
	if roleFilter == "" {
		return peers
	}
	result := make([]model.DdPeerInfo, 0, len(peers))
	for _, peer := range peers {
		if peer.Role == roleFilter {
			result = append(result, peer)
		}
	}
	return result
}

func (s *PeerListService) filterByStatus(peers []model.DdPeerInfo, statusFilter string) []model.DdPeerInfo {
	if statusFilter == "" {
		return peers
	}
	result := make([]model.DdPeerInfo, 0, len(peers))
	for _, peer := range peers {
		if peer.Status == statusFilter {
			result = append(result, peer)
		}
	}
	return result
}
