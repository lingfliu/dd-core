package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"dd-core/internal/model"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"uptime": time.Since(s.startTime).String(),
	})
}

func (s *Server) handlePeerRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Id   string `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Id == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}

	peer := model.DdPeerInfo{
		Id:   req.Id,
		Name: req.Name,
		Role: req.Role,
	}
	if err := s.peerRegistry.RegisterPeer(context.Background(), peer); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusAccepted, map[string]any{
		"peer_id": req.Id,
		"status":  model.PeerStatusRegistering,
	})
}

func (s *Server) handlePeerHeartbeat(w http.ResponseWriter, r *http.Request) {
	peerID := r.PathValue("id")
	if peerID == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	if err := s.peerRegistry.HeartbeatPeer(context.Background(), peerID); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"peer_id": peerID,
		"status":  "ok",
	})
}

func (s *Server) handlePeerUnregister(w http.ResponseWriter, r *http.Request) {
	peerID := r.PathValue("id")
	if peerID == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	if err := s.peerRegistry.UnregisterPeer(context.Background(), peerID); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"peer_id": peerID,
		"status":  "unregistered",
	})
}

func (s *Server) handlePeerGet(w http.ResponseWriter, r *http.Request) {
	peerID := r.PathValue("id")
	if peerID == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	peer, ok := s.peerRegistry.Store().Get(peerID)
	if !ok {
		s.writeJSON(w, http.StatusNotFound, map[string]string{"error": "peer not found"})
		return
	}
	s.writeJSON(w, http.StatusOK, peer)
}

func (s *Server) handlePeersQuery(w http.ResponseWriter, r *http.Request) {
	resource := r.URL.Query().Get("resource")
	role := r.URL.Query().Get("role")
	status := r.URL.Query().Get("status")

	peers := s.peerRegistry.GetPeers(resource)

	filtered := make([]model.DdPeerInfo, 0)
	for _, p := range peers {
		if role != "" && p.Role != role {
			continue
		}
		if status != "" && p.Status != status {
			continue
		}
		filtered = append(filtered, p)
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"peers": filtered,
		"count": len(filtered),
	})
}

func (s *Server) handleTransferSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Resource     string `json:"resource"`
		Protocol     string `json:"protocol"`
		SourcePeerId string `json:"source_peer_id"`
		TargetPeerId string `json:"target_peer_id"`
		TimeoutMs    int64  `json:"timeout_ms"`
		Payload      []byte `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Resource == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "resource is required"})
		return
	}

	msg := &model.DdMessage{
		Protocol: model.DdProtocol(req.Protocol),
		Resource: req.Resource,
		Header: model.DdHeader{
			RequestId:    "req-" + time.Now().Format("20060102150405.000000000"),
			SourcePeerId: req.SourcePeerId,
			TargetPeerId: req.TargetPeerId,
			TimeoutMs:    req.TimeoutMs,
		},
		Payload: req.Payload,
	}

	requestTopic := s.topics.TransferRequest(req.Resource)
	resp, err := s.dataService.SendSync(r.Context(), requestTopic, msg)
	if err != nil {
		if ddErr, ok := err.(*model.DdError); ok {
			s.writeError(w, ddErr)
			return
		}
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"response": string(resp.Payload),
	})
}

func (s *Server) handleTransferAsync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Resource     string `json:"resource"`
		Protocol     string `json:"protocol"`
		SourcePeerId string `json:"source_peer_id"`
		TargetPeerId string `json:"target_peer_id"`
		Payload      []byte `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Resource == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "resource is required"})
		return
	}

	msg := &model.DdMessage{
		Protocol: model.DdProtocol(req.Protocol),
		Resource: req.Resource,
		Header: model.DdHeader{
			RequestId:    "async-" + time.Now().Format("20060102150405.000000000"),
			SourcePeerId: req.SourcePeerId,
			TargetPeerId: req.TargetPeerId,
		},
		Payload: req.Payload,
	}

	eventTopic := s.topics.EventPublish(req.Resource)
	if err := s.dataService.SendAsync(r.Context(), eventTopic, msg); err != nil {
		if ddErr, ok := err.(*model.DdError); ok {
			s.writeError(w, ddErr)
			return
		}
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusAccepted, map[string]any{
		"resource": req.Resource,
		"status":   "published",
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	active := 0
	stale := 0
	for _, p := range s.peerRegistry.Store().List() {
		switch p.Status {
		case model.PeerStatusActive, model.PeerStatusRegistering:
			active++
		case model.PeerStatusStale:
			stale++
		}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"active_peers":     active,
		"stale_peers":      stale,
		"pending_requests": s.dataService.PendingRequestCount(),
	})
}

func (s *Server) handleStreamOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Resource      string   `json:"resource"`
		Profile       string   `json:"profile"`
		SourcePeerId  string   `json:"source_peer_id"`
		TargetPeerIds []string `json:"target_peer_ids"`
		IdleTimeoutMs int64    `json:"idle_timeout_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Resource == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "resource is required"})
		return
	}

	evt := model.StreamOpenEvent{
		Resource:      req.Resource,
		Profile:       model.StreamProfile(req.Profile),
		SourcePeerId:  req.SourcePeerId,
		TargetPeerIds: req.TargetPeerIds,
		IdleTimeoutMs: req.IdleTimeoutMs,
	}
	if err := s.streamService.OpenStream(r.Context(), evt); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusAccepted, map[string]string{"status": "stream open requested"})
}

func (s *Server) handleStreamClose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StreamId string `json:"stream_id"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.StreamId == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "stream_id is required"})
		return
	}

	if err := s.streamService.CloseStream(r.Context(), req.StreamId, req.Reason); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "stream close requested"})
}

func (s *Server) handleStreamsList(w http.ResponseWriter, r *http.Request) {
	sessions := s.streamService.ListSessions()
	s.writeJSON(w, http.StatusOK, map[string]any{
		"streams": sessions,
		"count":   len(sessions),
	})
}

func (s *Server) handleTransferStatus(w http.ResponseWriter, r *http.Request) {
	requestId := r.PathValue("requestId")
	if requestId == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "requestId is required"})
		return
	}

	rec, ok := s.dataService.GetTransferStatus(requestId)
	if !ok {
		s.writeJSON(w, http.StatusNotFound, map[string]string{"error": "transfer not found"})
		return
	}
	s.writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleResourceProviders(w http.ResponseWriter, r *http.Request) {
	resource := r.PathValue("resource")
	if resource == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "resource is required"})
		return
	}

	providers := s.resourceCatalog.GetProviders(resource)

	peers := s.peerRegistry.GetPeers(resource)
	result := make([]model.DdPeerInfo, 0, len(peers))
	for _, p := range peers {
		for _, pid := range providers {
			if p.Id == pid {
				result = append(result, p)
				break
			}
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"resource":  resource,
		"providers": result,
		"count":     len(result),
	})
}

func (s *Server) handleResourcesList(w http.ResponseWriter, r *http.Request) {
	resources := s.resourceCatalog.ListResources()
	s.writeJSON(w, http.StatusOK, map[string]any{
		"resources": resources,
		"count":     len(resources),
	})
}
