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

type StreamService struct {
	mqClient mq.Client
	topics   TopicSet
	mu       sync.RWMutex
	sessions map[string]*model.StreamSession
}

func NewStreamService(mqClient mq.Client, topics TopicSet) *StreamService {
	return &StreamService{
		mqClient: mqClient,
		topics:   topics,
		sessions: make(map[string]*model.StreamSession),
	}
}

func (s *StreamService) Start(ctx context.Context) error {
	if err := s.mqClient.Subscribe(ctx, s.topics.StreamOpen("*"), s.handleStreamOpen); err != nil {
		return err
	}
	if err := s.mqClient.Subscribe(ctx, s.topics.StreamClose("*"), s.handleStreamClose); err != nil {
		return err
	}
	return nil
}

func (s *StreamService) handleStreamOpen(_ string, payload []byte) {
	var evt model.StreamOpenEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		return
	}
	now := time.Now().UTC()
	session := &model.StreamSession{
		StreamId:      evt.StreamId,
		Resource:      evt.Resource,
		Profile:       evt.Profile,
		SourcePeerId:  evt.SourcePeerId,
		TargetPeerIds: evt.TargetPeerIds,
		Status:        model.StreamStatusActive,
		IdleTimeoutMs: evt.IdleTimeoutMs,
		OpenedAt:      now,
		LastActiveAt:  now,
	}
	if session.Profile == "" {
		session.Profile = model.StreamProfileFull
	}
	if session.IdleTimeoutMs <= 0 {
		session.IdleTimeoutMs = 15000
	}

	s.mu.Lock()
	s.sessions[evt.StreamId] = session
	s.mu.Unlock()

	slog.Info("stream opened", "stream_id", evt.StreamId, "resource", evt.Resource, "profile", session.Profile)
}

func (s *StreamService) handleStreamClose(_ string, payload []byte) {
	var evt model.StreamCloseEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		return
	}
	s.mu.Lock()
	if session, ok := s.sessions[evt.StreamId]; ok {
		session.Status = model.StreamStatusClosed
		session.ClosedAt = time.Now().UTC()
	}
	s.mu.Unlock()

	slog.Info("stream closed", "stream_id", evt.StreamId, "reason", evt.Reason)
}

func (s *StreamService) OpenStream(ctx context.Context, evt model.StreamOpenEvent) error {
	evt.StreamId = "strm-" + time.Now().Format("20060102150405.000000000")
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return s.mqClient.Publish(ctx, s.topics.StreamOpen(evt.Resource), data)
}

func (s *StreamService) CloseStream(ctx context.Context, streamId string, reason string) error {
	evt := model.StreamCloseEvent{
		StreamId: streamId,
		Reason:   reason,
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return s.mqClient.Publish(ctx, s.topics.StreamClose("*"), data)
}

func (s *StreamService) GetSession(streamId string) (*model.StreamSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[streamId]
	return session, ok
}

func (s *StreamService) ListSessions() []*model.StreamSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.StreamSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		out = append(out, session)
	}
	return out
}

func (s *StreamService) SweepIdle(now time.Time, idleTimeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, session := range s.sessions {
		if session.Status != model.StreamStatusActive {
			continue
		}
		timeout := time.Duration(session.IdleTimeoutMs) * time.Millisecond
		if timeout <= 0 {
			timeout = idleTimeout
		}
		if now.Sub(session.LastActiveAt) > timeout {
			session.Status = model.StreamStatusTimeout
			slog.Warn("stream idle timeout", "stream_id", session.StreamId, "resource", session.Resource)
		}
	}
}
