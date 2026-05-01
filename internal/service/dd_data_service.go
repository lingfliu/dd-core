package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
	"dd-core/internal/observability"
)

const maxIdempotentKeys = 10000

type idempotentEntry struct {
	response *model.DdMessage
	expireAt time.Time
}

type SyncResult struct {
	Response *model.DdMessage
	Err      error
}

type DdDataService struct {
	mqClient           mq.Client
	defaultTimeout     time.Duration
	statusRetention    time.Duration
	compressThreshold  int
	maxPendingRequests int
	aclService         *TopicAclService
	pendingMu          sync.RWMutex
	pendingRequests    map[string]chan *model.DdMessage

	idempotentMu   sync.RWMutex
	idempotentKeys map[string]idempotentEntry

	statusMu         sync.RWMutex
	transferStatuses map[string]*model.TransferRecord
}

type DdDataOption func(*DdDataService)

func WithDataAclService(acl *TopicAclService) DdDataOption {
	return func(s *DdDataService) {
		s.aclService = acl
	}
}

func NewDdDataService(mqClient mq.Client, defaultTimeout time.Duration, opts ...DdDataOption) *DdDataService {
	s := &DdDataService{
		mqClient:           mqClient,
		defaultTimeout:     defaultTimeout,
		statusRetention:    30 * time.Minute,
		compressThreshold:  128,
		maxPendingRequests: 1024,
		pendingRequests:    make(map[string]chan *model.DdMessage),
		idempotentKeys:     make(map[string]idempotentEntry),
		transferStatuses:   make(map[string]*model.TransferRecord),
	}
	for _, o := range opts {
		o(s)
	}
	go s.sweepIdempotent()
	go s.sweepTransferStatuses()
	return s
}

func (s *DdDataService) checkAcl(peerId string, action string, topic string) error {
	if s.aclService == nil {
		return nil
	}
	if !s.aclService.Authorize(peerId, AclAction(action), topic) {
		observability.AclDeniedTotal.Inc()
		slog.Warn("acl denied", "peer_id", peerId, "action", action, "topic", topic)
		return model.ErrAclDeniedMsg(peerId, action, topic)
	}
	return nil
}

func (s *DdDataService) SubscribeSyncResponses(ctx context.Context, responseTopic string) error {
	return s.mqClient.Subscribe(ctx, responseTopic, func(_ string, payload []byte) {
		var msg model.DdMessage
		if err := model.DecodeDdMessage(payload, &msg); err != nil {
			return
		}
		reqId := msg.Header.CorrelationId
		if reqId == "" {
			reqId = msg.Header.RequestId
		}
		if reqId == "" {
			return
		}

		s.pendingMu.Lock()
		ch, ok := s.pendingRequests[reqId]
		s.pendingMu.Unlock()
		if ok {
			select {
			case ch <- &msg:
			default:
				// If the waiter channel is full (late or duplicate responses),
				// replace stale value with the latest response instead of silently dropping it.
				select {
				case <-ch:
				default:
				}
				select {
				case ch <- &msg:
				default:
				}
			}
		}
	})
}

func (s *DdDataService) SendAsync(ctx context.Context, topic string, msg *model.DdMessage) error {
	if msg == nil {
		return model.ErrInvalidEnvelopeMsg("nil message")
	}
	msg.Normalize()

	if err := s.checkAcl(msg.Header.SourcePeerId, "pub", topic); err != nil {
		observability.AsyncPublishedTotal.WithLabelValues(msg.Resource, "denied").Inc()
		return err
	}

	msg.Mode = model.DdTransferModeAsync
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if err := msg.Validate(); err != nil {
		observability.AsyncPublishedTotal.WithLabelValues(msg.Resource, "invalid").Inc()
		return model.ErrInvalidEnvelopeMsg(err.Error())
	}
	if msg.Header.TraceId == "" {
		msg.Header.TraceId = msg.Header.RequestId
	}
	data, err := model.EncodeDdMessage(msg, s.shouldCompress(msg))
	if err != nil {
		return err
	}
	if err := s.mqClient.Publish(ctx, topic, data); err != nil {
		observability.AsyncPublishedTotal.WithLabelValues(msg.Resource, "error").Inc()
		s.recordStatus(msg.Header.RequestId, msg, model.TransferStatusFailed, 0)
		return err
	}
	observability.AsyncPublishedTotal.WithLabelValues(msg.Resource, "ok").Inc()
	slog.Info("async published", "resource", msg.Resource, "request_id", msg.Header.RequestId)
	s.recordStatus(msg.Header.RequestId, msg, model.TransferStatusAccepted, 0)
	return nil
}

func (s *DdDataService) SendSync(
	ctx context.Context,
	requestTopic string,
	msg *model.DdMessage,
) (*model.DdMessage, error) {
	start := time.Now()

	if msg == nil {
		return nil, model.ErrInvalidEnvelopeMsg("nil message")
	}
	msg.Normalize()

	if err := s.checkAcl(msg.Header.SourcePeerId, "pub", requestTopic); err != nil {
		observability.SyncRequestsTotal.WithLabelValues(msg.Resource, "denied").Inc()
		return nil, err
	}

	if cached := s.lookupIdempotent(msg); cached != nil {
		observability.SyncRequestsTotal.WithLabelValues(msg.Resource, "idempotent").Inc()
		slog.Info("idempotent cache hit", "request_id", msg.Header.RequestId, "key", msg.Header.IdempotencyKey)
		return cached, nil
	}

	msg.Mode = model.DdTransferModeSync
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if msg.Header.TimeoutMs <= 0 {
		msg.Header.TimeoutMs = s.defaultTimeout.Milliseconds()
	}
	if msg.Header.RequestId == "" {
		return nil, model.ErrInvalidEnvelopeMsg("request_id is required")
	}
	if msg.Header.TraceId == "" {
		msg.Header.TraceId = msg.Header.RequestId
	}
	if err := msg.Validate(); err != nil {
		return nil, model.ErrInvalidEnvelopeMsg(err.Error())
	}

	s.recordStatus(msg.Header.RequestId, msg, model.TransferStatusAccepted, 0)

	waitCh := make(chan *model.DdMessage, 1)
	s.pendingMu.Lock()
	if len(s.pendingRequests) >= s.maxPendingRequests {
		s.pendingMu.Unlock()
		return nil, model.NewDdError(model.ErrInternal, "sync pending backpressure limit reached")
	}
	if _, exists := s.pendingRequests[msg.Header.RequestId]; exists {
		s.pendingMu.Unlock()
		return nil, model.ErrDuplicateRequestMsg(msg.Header.RequestId)
	}
	s.pendingRequests[msg.Header.RequestId] = waitCh
	s.pendingMu.Unlock()
	defer func() {
		s.pendingMu.Lock()
		delete(s.pendingRequests, msg.Header.RequestId)
		s.pendingMu.Unlock()
	}()

	s.recordStatus(msg.Header.RequestId, msg, model.TransferStatusRouted, 0)

	data, err := model.EncodeDdMessage(msg, s.shouldCompress(msg))
	if err != nil {
		s.recordStatus(msg.Header.RequestId, msg, model.TransferStatusFailed, 0)
		return nil, err
	}
	if err := s.mqClient.Publish(ctx, requestTopic, data); err != nil {
		observability.SyncRequestsTotal.WithLabelValues(msg.Resource, "error").Inc()
		s.recordStatus(msg.Header.RequestId, msg, model.TransferStatusFailed, 0)
		return nil, err
	}

	timeout := time.Duration(msg.Header.TimeoutMs) * time.Millisecond
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		observability.SyncRequestsTotal.WithLabelValues(msg.Resource, "cancelled").Inc()
		return nil, ctx.Err()
	case <-timer.C:
		observability.TimeoutTotal.Inc()
		observability.SyncRequestsTotal.WithLabelValues(msg.Resource, "timeout").Inc()
		slog.Warn("sync timeout", "request_id", msg.Header.RequestId, "resource", msg.Resource)
		s.recordStatus(msg.Header.RequestId, msg, model.TransferStatusTimeout, 0)
		return nil, model.ErrSyncTimeoutMsg(msg.Header.RequestId)
	case resp := <-waitCh:
		elapsed := time.Since(start).Milliseconds()
		observability.SyncLatencyMs.WithLabelValues(msg.Resource).Observe(float64(elapsed))
		observability.SyncRequestsTotal.WithLabelValues(msg.Resource, "ok").Inc()
		slog.Info("sync completed", "request_id", msg.Header.RequestId, "resource", msg.Resource, "latency_ms", elapsed)
		s.recordStatus(msg.Header.RequestId, msg, model.TransferStatusResponded, elapsed)
		s.storeIdempotent(msg, resp)
		return resp, nil
	}
}

func (s *DdDataService) shouldCompress(msg *model.DdMessage) bool {
	return len(msg.Payload) >= s.compressThreshold
}

func (s *DdDataService) SendSyncAsync(
	ctx context.Context,
	requestTopic string,
	msg *model.DdMessage,
) <-chan SyncResult {
	out := make(chan SyncResult, 1)
	go func() {
		resp, err := s.SendSync(ctx, requestTopic, msg)
		out <- SyncResult{Response: resp, Err: err}
		close(out)
	}()
	return out
}

func (s *DdDataService) PendingRequestCount() int {
	s.pendingMu.RLock()
	defer s.pendingMu.RUnlock()
	return len(s.pendingRequests)
}

func (s *DdDataService) GetTransferStatus(requestId string) (*model.TransferRecord, bool) {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	rec, ok := s.transferStatuses[requestId]
	if !ok {
		return nil, false
	}
	copy := *rec
	return &copy, true
}

func (s *DdDataService) recordStatus(requestId string, msg *model.DdMessage, status model.TransferStatus, latencyMs int64) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	now := time.Now().UTC()
	rec, exists := s.transferStatuses[requestId]
	if !exists {
		rec = &model.TransferRecord{
			RequestId:    requestId,
			Resource:     msg.Resource,
			Protocol:     msg.Protocol,
			Mode:         msg.Mode,
			SourcePeerId: msg.Header.SourcePeerId,
			TargetPeerId: msg.Header.TargetPeerId,
			CreatedAt:    now,
		}
		s.transferStatuses[requestId] = rec
	}
	rec.Status = status
	rec.LatencyMs = latencyMs
	rec.UpdatedAt = now
}

func (s *DdDataService) lookupIdempotent(msg *model.DdMessage) *model.DdMessage {
	if msg.Header.IdempotencyKey == "" {
		return nil
	}
	s.idempotentMu.RLock()
	defer s.idempotentMu.RUnlock()
	entry, ok := s.idempotentKeys[msg.Header.IdempotencyKey]
	if ok && time.Now().Before(entry.expireAt) {
		return entry.response
	}
	return nil
}

func (s *DdDataService) storeIdempotent(msg *model.DdMessage, resp *model.DdMessage) {
	if msg.Header.IdempotencyKey == "" {
		return
	}
	ttl := time.Duration(msg.Header.TimeoutMs) * time.Millisecond * 5
	if ttl < time.Minute {
		ttl = time.Minute
	}
	s.idempotentMu.Lock()
	defer s.idempotentMu.Unlock()
	s.idempotentKeys[msg.Header.IdempotencyKey] = idempotentEntry{
		response: resp,
		expireAt: time.Now().Add(ttl),
	}
}

func (s *DdDataService) sweepIdempotent() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.idempotentMu.Lock()
		now := time.Now()
		for key, entry := range s.idempotentKeys {
			if now.After(entry.expireAt) {
				delete(s.idempotentKeys, key)
			}
		}
		if len(s.idempotentKeys) > maxIdempotentKeys {
			for len(s.idempotentKeys) > maxIdempotentKeys {
				var oldestKey string
				var oldestAt time.Time
				first := true
				for key, entry := range s.idempotentKeys {
					if first || entry.expireAt.Before(oldestAt) {
						oldestKey = key
						oldestAt = entry.expireAt
						first = false
					}
				}
				if oldestKey == "" {
					break
				}
				delete(s.idempotentKeys, oldestKey)
			}
		}
		s.idempotentMu.Unlock()
	}
}

func (s *DdDataService) sweepTransferStatuses() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.statusMu.Lock()
		now := time.Now().UTC()
		for requestID, rec := range s.transferStatuses {
			if now.Sub(rec.UpdatedAt) > s.statusRetention {
				delete(s.transferStatuses, requestID)
			}
		}
		s.statusMu.Unlock()
	}
}
