package mq

import (
	"context"
	"strings"
	"sync"
)

type PublishedMessage struct {
	Topic   string
	Payload []byte
}

type MockClient struct {
	mu            sync.RWMutex
	subscribers   map[string][]MessageHandler
	published     []PublishedMessage
	failPublish   error
	failSubscribe error
}

func NewMockClient() *MockClient {
	return &MockClient{
		subscribers: make(map[string][]MessageHandler),
		published:   make([]PublishedMessage, 0),
	}
}

func (m *MockClient) Publish(_ context.Context, topic string, payload []byte) error {
	m.mu.Lock()
	if m.failPublish != nil {
		m.mu.Unlock()
		return m.failPublish
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	m.published = append(m.published, PublishedMessage{Topic: topic, Payload: cp})
	handlers := m.matchingHandlers(topic)
	m.mu.Unlock()

	for _, h := range handlers {
		h(topic, cp)
	}
	return nil
}

func (m *MockClient) Subscribe(_ context.Context, topic string, handler MessageHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSubscribe != nil {
		return m.failSubscribe
	}
	m.subscribers[topic] = append(m.subscribers[topic], handler)
	return nil
}

func (m *MockClient) Close() error { return nil }

func (m *MockClient) Emit(topic string, payload []byte) {
	m.mu.RLock()
	handlers := m.matchingHandlers(topic)
	m.mu.RUnlock()
	for _, h := range handlers {
		h(topic, payload)
	}
}

func (m *MockClient) matchingHandlers(topic string) []MessageHandler {
	var handlers []MessageHandler
	for pattern, subs := range m.subscribers {
		if matchMQTTTopicWildcard(pattern, topic) {
			handlers = append(handlers, subs...)
		}
	}
	return handlers
}

func matchMQTTTopicWildcard(pattern, topic string) bool {
	if pattern == topic {
		return true
	}
	pp := strings.Split(pattern, "/")
	tp := strings.Split(topic, "/")
	return matchLevels(pp, tp)
}

func matchLevels(pp, tp []string) bool {
	pi, ti := 0, 0
	for pi < len(pp) {
		if pp[pi] == "#" {
			return true
		}
		if ti >= len(tp) {
			return false
		}
		if pp[pi] == "+" || pp[pi] == "*" {
			pi++
			ti++
			continue
		}
		if pp[pi] != tp[ti] {
			return false
		}
		pi++
		ti++
	}
	return len(pp) == len(tp)
}

func (m *MockClient) Published() []PublishedMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]PublishedMessage, len(m.published))
	copy(out, m.published)
	return out
}

func (m *MockClient) SetPublishError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failPublish = err
}

func (m *MockClient) SetSubscribeError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failSubscribe = err
}
