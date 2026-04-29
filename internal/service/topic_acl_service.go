package service

import (
	"strings"
	"sync"
)

type AclAction string

const (
	AclActionPub AclAction = "pub"
	AclActionSub AclAction = "sub"
)

type TopicAclRule struct {
	PeerId  string
	Action  AclAction
	Pattern string
	Allow   bool
}

type TopicAclService struct {
	mu    sync.RWMutex
	rules []TopicAclRule
}

func NewTopicAclService() *TopicAclService {
	return &TopicAclService{rules: make([]TopicAclRule, 0)}
}

func (s *TopicAclService) SetRules(rules []TopicAclRule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = append([]TopicAclRule(nil), rules...)
}

func (s *TopicAclService) Authorize(peerID string, action AclAction, topic string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	allowed := false
	for _, rule := range s.rules {
		if rule.PeerId != peerID || rule.Action != action {
			continue
		}
		if !matchMQTTTopic(rule.Pattern, topic) {
			continue
		}
		if !rule.Allow {
			return false
		}
		allowed = true
	}
	return allowed
}

func matchMQTTTopic(pattern, topic string) bool {
	pp := strings.Split(pattern, "/")
	tp := strings.Split(topic, "/")

	for i := 0; i < len(pp); i++ {
		if pp[i] == "#" {
			return true
		}
		if i >= len(tp) {
			return false
		}
		if pp[i] == "+" {
			continue
		}
		if pp[i] != tp[i] {
			return false
		}
	}
	return len(pp) == len(tp)
}
