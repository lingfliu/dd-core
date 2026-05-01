package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/observability"
	"dd-core/internal/service"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"uptime": time.Since(s.startTime).String(),
	})
}

func (s *Server) handlePeerRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Id              string                              `json:"id"`
		Name            string                              `json:"name"`
		Role            string                              `json:"role"`
		Resources       []string                            `json:"resources,omitempty"`
		Capabilities    []string                            `json:"capabilities,omitempty"`
		ResourceSchemas map[string]model.ResourceMetaSchema `json:"resource_meta_schemas,omitempty"`
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
		Id:              req.Id,
		Name:            req.Name,
		Role:            req.Role,
		Resources:       req.Resources,
		Capabilities:    req.Capabilities,
		ResourceSchemas: req.ResourceSchemas,
	}
	if err := s.peerRegistry.RegisterPeer(context.Background(), peer); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.resourceCatalog.OnPeerRegistered(peer)
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
		Resource     string                       `json:"resource"`
		Protocol     string                       `json:"protocol"`
		SourcePeerId string                       `json:"source_peer_id"`
		TargetPeerId string                       `json:"target_peer_id"`
		TimeoutMs    int64                        `json:"timeout_ms"`
		TraceId      string                       `json:"trace_id,omitempty"`
		Headers      map[string]string            `json:"headers,omitempty"`
		ProtocolMeta map[string]map[string]string `json:"protocol_meta,omitempty"`
		Payload      []byte                       `json:"payload"`
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
			TraceId:      req.TraceId,
		},
		Headers:      req.Headers,
		ProtocolMeta: req.ProtocolMeta,
		Payload:      req.Payload,
	}
	if msg.Header.TraceId == "" {
		msg.Header.TraceId = r.Header.Get("X-Trace-Id")
	}
	msg.Normalize()
	if msg.Header.ReplyTo == "" {
		msg.Header.ReplyTo = s.topics.TransferResponseByRequest(msg.Header.RequestId)
	}
	if err := s.validateRequestMeta(req.Resource, msg); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	routeMode := service.DecideRoute(s.cfg, msg)
	if routeMode == service.RouteModeDirect {
		directResp, err := s.sendDirectSync(r.Context(), msg)
		if err == nil {
			observability.RouteDecisionTotal.WithLabelValues(string(service.RouteModeDirect)).Inc()
			s.writeJSON(w, http.StatusOK, map[string]any{
				"response":   string(directResp),
				"route_mode": service.RouteModeDirect,
			})
			return
		}
		routeMode = service.RouteModeBroker
	}
	observability.RouteDecisionTotal.WithLabelValues(string(routeMode)).Inc()
	if s.dataService == nil {
		s.writeError(w, model.NewDdError(model.ErrInternal, "data service is unavailable"))
		return
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
		"response":   string(resp.Payload),
		"route_mode": routeMode,
	})
}

func (s *Server) handleTransferAsync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Resource     string                       `json:"resource"`
		Protocol     string                       `json:"protocol"`
		SourcePeerId string                       `json:"source_peer_id"`
		TargetPeerId string                       `json:"target_peer_id"`
		TraceId      string                       `json:"trace_id,omitempty"`
		Headers      map[string]string            `json:"headers,omitempty"`
		ProtocolMeta map[string]map[string]string `json:"protocol_meta,omitempty"`
		Payload      []byte                       `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Resource == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "resource is required"})
		return
	}

	msg := s.newAsyncMessage(req.Resource, req.Protocol, req.SourcePeerId, req.TargetPeerId, req.TraceId, req.Headers, req.ProtocolMeta, req.Payload)
	if msg.Header.TraceId == "" {
		msg.Header.TraceId = r.Header.Get("X-Trace-Id")
	}
	msg.Normalize()
	if err := s.validateRequestMeta(req.Resource, msg); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	routeMode := service.DecideRoute(s.cfg, msg)
	if routeMode == service.RouteModeDirect {
		if err := s.sendDirectAsync(r.Context(), msg); err == nil {
			observability.RouteDecisionTotal.WithLabelValues(string(service.RouteModeDirect)).Inc()
			s.writeJSON(w, http.StatusAccepted, map[string]any{
				"resource":   req.Resource,
				"status":     "published",
				"route_mode": service.RouteModeDirect,
			})
			return
		}
		routeMode = service.RouteModeBroker
	}
	observability.RouteDecisionTotal.WithLabelValues(string(routeMode)).Inc()
	if s.dataService == nil {
		s.writeError(w, model.NewDdError(model.ErrInternal, "data service is unavailable"))
		return
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
		"resource":   req.Resource,
		"status":     "published",
		"route_mode": routeMode,
	})
}

func (s *Server) handleTransferAsyncBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []struct {
			Resource     string                       `json:"resource"`
			Protocol     string                       `json:"protocol"`
			SourcePeerId string                       `json:"source_peer_id"`
			TargetPeerId string                       `json:"target_peer_id"`
			TraceId      string                       `json:"trace_id,omitempty"`
			Headers      map[string]string            `json:"headers,omitempty"`
			ProtocolMeta map[string]map[string]string `json:"protocol_meta,omitempty"`
			Payload      []byte                       `json:"payload"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if len(req.Items) == 0 {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "items is required"})
		return
	}
	if s.dataService == nil {
		s.writeError(w, model.NewDdError(model.ErrInternal, "data service is unavailable"))
		return
	}

	type itemResult struct {
		Index     int               `json:"index"`
		RequestID string            `json:"request_id"`
		Resource  string            `json:"resource"`
		RouteMode service.RouteMode `json:"route_mode"`
		Status    string            `json:"status"`
		Error     string            `json:"error,omitempty"`
	}
	results := make([]itemResult, 0, len(req.Items))
	accepted := 0
	failed := 0
	for idx, item := range req.Items {
		if item.Resource == "" {
			failed++
			results = append(results, itemResult{
				Index:    idx,
				Resource: item.Resource,
				Status:   "failed",
				Error:    "resource is required",
			})
			continue
		}
		msg := s.newAsyncMessage(item.Resource, item.Protocol, item.SourcePeerId, item.TargetPeerId, item.TraceId, item.Headers, item.ProtocolMeta, item.Payload)
		if msg.Header.TraceId == "" {
			msg.Header.TraceId = r.Header.Get("X-Trace-Id")
		}
		msg.Normalize()
		if err := s.validateRequestMeta(item.Resource, msg); err != nil {
			failed++
			results = append(results, itemResult{
				Index:     idx,
				RequestID: msg.Header.RequestId,
				Resource:  item.Resource,
				Status:    "failed",
				Error:     err.Error(),
			})
			continue
		}
		routeMode := service.DecideRoute(s.cfg, msg)
		observability.RouteDecisionTotal.WithLabelValues(string(routeMode)).Inc()
		if routeMode == service.RouteModeDirect {
			if err := s.sendDirectAsync(r.Context(), msg); err == nil {
				accepted++
				results = append(results, itemResult{
					Index:     idx,
					RequestID: msg.Header.RequestId,
					Resource:  item.Resource,
					RouteMode: service.RouteModeDirect,
					Status:    "published",
				})
				continue
			}
			routeMode = service.RouteModeBroker
		}
		eventTopic := s.topics.EventPublish(item.Resource)
		if err := s.dataService.SendAsync(r.Context(), eventTopic, msg); err != nil {
			failed++
			results = append(results, itemResult{
				Index:     idx,
				RequestID: msg.Header.RequestId,
				Resource:  item.Resource,
				RouteMode: routeMode,
				Status:    "failed",
				Error:     err.Error(),
			})
			continue
		}
		accepted++
		results = append(results, itemResult{
			Index:     idx,
			RequestID: msg.Header.RequestId,
			Resource:  item.Resource,
			RouteMode: routeMode,
			Status:    "published",
		})
	}

	status := "ok"
	code := http.StatusAccepted
	if accepted == 0 {
		status = "error"
		code = http.StatusBadRequest
	} else if failed > 0 {
		status = "partial"
	}
	observability.AsyncBatchRequestsTotal.WithLabelValues(status).Inc()
	s.writeJSON(w, code, map[string]any{
		"requested": len(req.Items),
		"accepted":  accepted,
		"failed":    failed,
		"status":    status,
		"results":   results,
	})
}

func (s *Server) newAsyncMessage(resource, protocol, sourcePeerID, targetPeerID, traceID string, headers map[string]string, protocolMeta map[string]map[string]string, payload []byte) *model.DdMessage {
	return &model.DdMessage{
		Protocol: model.DdProtocol(protocol),
		Resource: resource,
		Header: model.DdHeader{
			RequestId:    "async-" + time.Now().Format("20060102150405.000000000"),
			SourcePeerId: sourcePeerID,
			TargetPeerId: targetPeerID,
			TraceId:      traceID,
		},
		Headers:      headers,
		ProtocolMeta: protocolMeta,
		Payload:      payload,
	}
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

func (s *Server) handleResourceMetaSchema(w http.ResponseWriter, r *http.Request) {
	resource := r.PathValue("resource")
	if resource == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "resource is required"})
		return
	}
	schema, ok := s.peerRegistry.GetResourceMetaSchema(resource)
	if !ok {
		s.writeJSON(w, http.StatusNotFound, map[string]string{"error": "resource meta schema not found"})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"resource": resource,
		"schema":   schema,
	})
}

func (s *Server) handleHTTPProtocolResources(w http.ResponseWriter, r *http.Request) {
	if s.protocolIndex == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "protocol resource index is unavailable"})
		return
	}
	observability.ProtocolResourceQueryTotal.WithLabelValues("http", "list").Inc()
	items := s.protocolIndex.ListHTTP("")
	s.writeJSON(w, http.StatusOK, map[string]any{
		"protocol": "http",
		"items":    items,
		"count":    len(items),
	})
}

func (s *Server) handleHTTPProtocolResourceByName(w http.ResponseWriter, r *http.Request) {
	resource := r.PathValue("resource")
	if resource == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "resource is required"})
		return
	}
	if s.protocolIndex == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "protocol resource index is unavailable"})
		return
	}
	observability.ProtocolResourceQueryTotal.WithLabelValues("http", "detail").Inc()
	items := s.protocolIndex.ListHTTP(resource)
	if len(items) == 0 {
		s.writeJSON(w, http.StatusNotFound, map[string]string{"error": "resource not found"})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"protocol": "http",
		"items":    items,
		"count":    len(items),
	})
}

func (s *Server) handleCoapProtocolResources(w http.ResponseWriter, r *http.Request) {
	if s.protocolIndex == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "protocol resource index is unavailable"})
		return
	}
	observability.ProtocolResourceQueryTotal.WithLabelValues("coap", "list").Inc()
	items := s.protocolIndex.ListCoap("")
	s.writeJSON(w, http.StatusOK, map[string]any{
		"protocol": "coap",
		"items":    items,
		"count":    len(items),
	})
}

func (s *Server) handleCoapProtocolResourceByName(w http.ResponseWriter, r *http.Request) {
	resource := r.PathValue("resource")
	if resource == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "resource is required"})
		return
	}
	if s.protocolIndex == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "protocol resource index is unavailable"})
		return
	}
	observability.ProtocolResourceQueryTotal.WithLabelValues("coap", "detail").Inc()
	items := s.protocolIndex.ListCoap(resource)
	if len(items) == 0 {
		s.writeJSON(w, http.StatusNotFound, map[string]string{"error": "resource not found"})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"protocol": "coap",
		"items":    items,
		"count":    len(items),
	})
}

func (s *Server) handleMqttProtocolTopics(w http.ResponseWriter, r *http.Request) {
	if s.protocolIndex == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "protocol resource index is unavailable"})
		return
	}
	observability.ProtocolResourceQueryTotal.WithLabelValues("mqtt", "list").Inc()
	direction := r.URL.Query().Get("direction")
	if direction == "" {
		direction = "both"
	}
	peerID := r.URL.Query().Get("peer_id")
	qos := -1
	if v := r.URL.Query().Get("qos"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || (n != 0 && n != 1) {
			s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "qos must be 0 or 1"})
			return
		}
		qos = n
	}
	items := s.protocolIndex.ListMqttTopics(direction, peerID, qos)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"protocol": "mqtt",
		"items":    items,
		"count":    len(items),
	})
}

func (s *Server) handleMqttProtocolTopicByName(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if s.protocolIndex == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "protocol resource index is unavailable"})
		return
	}
	observability.ProtocolResourceQueryTotal.WithLabelValues("mqtt", "detail").Inc()
	items := s.protocolIndex.GetMqttTopic(name)
	if len(items) == 0 {
		s.writeJSON(w, http.StatusNotFound, map[string]string{"error": "topic not found"})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"protocol": "mqtt",
		"items":    items,
		"count":    len(items),
	})
}

func (s *Server) validateRequestMeta(resource string, msg *model.DdMessage) error {
	schema, ok := s.peerRegistry.GetResourceMetaSchema(resource)
	if !ok {
		observability.ResourceMetaValidateTotal.WithLabelValues(resource, "skip").Inc()
		return nil
	}
	for _, field := range schema.Required {
		if !s.hasMetaField(msg, field) {
			observability.ResourceMetaValidateTotal.WithLabelValues(resource, "invalid").Inc()
			return fmt.Errorf("missing required meta field: %s", field)
		}
	}
	fields := msg.ProtocolFields()
	for _, key := range schema.ProtocolMetaRequired {
		if fields[key] == "" {
			observability.ResourceMetaValidateTotal.WithLabelValues(resource, "invalid").Inc()
			return fmt.Errorf("missing required protocol_meta field: %s", key)
		}
	}
	for field, c := range schema.Constraints {
		if err := s.validateConstraint(msg, fields, field, c); err != nil {
			observability.ResourceMetaValidateTotal.WithLabelValues(resource, "invalid").Inc()
			return err
		}
	}
	observability.ResourceMetaValidateTotal.WithLabelValues(resource, "ok").Inc()
	return nil
}

func (s *Server) hasMetaField(msg *model.DdMessage, field string) bool {
	switch field {
	case "request_id":
		return msg.Header.RequestId != ""
	case "correlation_id":
		return msg.Header.CorrelationId != ""
	case "reply_to":
		return msg.Header.ReplyTo != ""
	case "source_peer_id":
		return msg.Header.SourcePeerId != ""
	case "target_peer_id":
		return msg.Header.TargetPeerId != ""
	case "tenant":
		return msg.Header.Tenant != ""
	case "trace_id":
		return msg.Header.TraceId != ""
	case "idempotency_key":
		return msg.Header.IdempotencyKey != ""
	case "timeout_ms":
		return msg.Header.TimeoutMs > 0
	default:
		return false
	}
}

func (s *Server) validateConstraint(msg *model.DdMessage, fields map[string]string, field string, c model.ResourceMetaConstraint) error {
	if field == "timeout_ms" {
		value := msg.Header.TimeoutMs
		if c.Min != nil && value < *c.Min {
			return fmt.Errorf("constraint violation: timeout_ms < %d", *c.Min)
		}
		if c.Max != nil && value > *c.Max {
			return fmt.Errorf("constraint violation: timeout_ms > %d", *c.Max)
		}
		return nil
	}
	if len(c.Enum) == 0 {
		return nil
	}
	value := fields[field]
	if value == "" {
		return nil
	}
	for _, v := range c.Enum {
		if value == v {
			return nil
		}
	}
	return fmt.Errorf("constraint violation: %s not in enum", field)
}

func (s *Server) sendDirectSync(ctx context.Context, msg *model.DdMessage) ([]byte, error) {
	switch msg.Protocol {
	case model.DdProtocolHttp:
		return s.sendDirectHTTP(ctx, msg)
	case model.DdProtocolCoap:
		return s.sendDirectCoap(ctx, msg)
	default:
		return nil, fmt.Errorf("direct route unsupported protocol: %s", msg.Protocol)
	}
}

func (s *Server) sendDirectAsync(ctx context.Context, msg *model.DdMessage) error {
	switch msg.Protocol {
	case model.DdProtocolHttp:
		_, err := s.sendDirectHTTP(ctx, msg)
		return err
	case model.DdProtocolCoap:
		_, err := s.sendDirectCoap(ctx, msg)
		return err
	default:
		return fmt.Errorf("direct route unsupported protocol: %s", msg.Protocol)
	}
}

func (s *Server) sendDirectHTTP(ctx context.Context, msg *model.DdMessage) ([]byte, error) {
	if s.cfg == nil || !s.cfg.Bridges.Http.Enabled || s.cfg.Bridges.Http.TargetUrl == "" {
		return nil, fmt.Errorf("direct http target unavailable")
	}
	fields := msg.ProtocolFields()
	method := fields["method"]
	if method == "" {
		method = http.MethodGet
	}
	path := fields["path"]
	if path == "" {
		path = "/"
	}
	url := s.cfg.Bridges.Http.TargetUrl + path

	reqCtx := ctx
	var cancel context.CancelFunc
	if msg.Header.TimeoutMs > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, time.Duration(msg.Header.TimeoutMs)*time.Millisecond)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(reqCtx, method, url, bytes.NewReader(msg.Payload))
	if err != nil {
		return nil, err
	}
	for k, v := range fields {
		if k == "method" || k == "path" {
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (s *Server) sendDirectCoap(ctx context.Context, msg *model.DdMessage) ([]byte, error) {
	if s.cfg == nil || !s.cfg.Bridges.Coap.Enabled || s.cfg.Bridges.Coap.TargetUrl == "" {
		return nil, fmt.Errorf("direct coap target unavailable")
	}
	target := s.cfg.Bridges.Coap.TargetUrl
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		target = u.Host
	}
	addr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return nil, err
	}

	fields := msg.ProtocolFields()
	method := fields["method"]
	if method == "" {
		method = "GET"
	}
	path := fields["path"]
	if path == "" {
		path = "/"
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	deadline := time.Now().Add(5 * time.Second)
	if msg.Header.TimeoutMs > 0 {
		deadline = time.Now().Add(time.Duration(msg.Header.TimeoutMs) * time.Millisecond)
	}
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	reqPkt := buildCoapPacket(method, path, msg.Payload)
	if _, err := conn.Write(reqPkt); err != nil {
		return nil, err
	}
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return parseCoapPayloadBytes(buf[:n]), nil
}

func buildCoapPacket(method, path string, payload []byte) []byte {
	code := byte(0x01)
	switch method {
	case "POST":
		code = 0x02
	case "PUT":
		code = 0x03
	case "DELETE":
		code = 0x04
	}
	msgID := uint16(time.Now().UnixNano() & 0xffff)
	pkt := make([]byte, 0, 256)
	pkt = append(pkt, 0x40, code, byte(msgID>>8), byte(msgID))
	pkt = append(pkt, encodeCoapOptionPath(path)...)
	if len(payload) > 0 {
		pkt = append(pkt, 0xff)
		pkt = append(pkt, payload...)
	}
	return pkt
}

func encodeCoapOptionPath(path string) []byte {
	deltaNibble, deltaExt := encodeCoapNibble(11)
	val := []byte(path)
	lengthNibble, lengthExt := encodeCoapNibble(len(val))
	out := make([]byte, 0, 1+len(deltaExt)+len(lengthExt)+len(val))
	out = append(out, byte(deltaNibble<<4|lengthNibble))
	out = append(out, deltaExt...)
	out = append(out, lengthExt...)
	out = append(out, val...)
	return out
}

func encodeCoapNibble(v int) (int, []byte) {
	switch {
	case v < 13:
		return v, nil
	case v < 269:
		return 13, []byte{byte(v - 13)}
	default:
		n := v - 269
		return 14, []byte{byte(n >> 8), byte(n)}
	}
}

func parseCoapPayloadBytes(data []byte) []byte {
	if len(data) < 4 {
		return nil
	}
	tkl := int(data[0] & 0x0f)
	pos := 4 + tkl
	for pos < len(data) {
		if data[pos] == 0xff {
			pos++
			if pos < len(data) {
				return data[pos:]
			}
			return nil
		}
		delta := int(data[pos] >> 4)
		length := int(data[pos] & 0x0f)
		pos++
		if delta == 13 && pos < len(data) {
			pos++
		} else if delta == 14 && pos+1 < len(data) {
			pos += 2
		}
		if length == 13 && pos < len(data) {
			length = int(data[pos]) + 13
			pos++
		} else if length == 14 && pos+1 < len(data) {
			length = (int(data[pos])<<8 | int(data[pos+1])) + 269
			pos += 2
		}
		if length > len(data)-pos {
			break
		}
		pos += length
	}
	return nil
}
