package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"

	"dd-core/internal/model"
	"dd-core/internal/mq"
)

type AuthPeerService struct {
	mqClient mq.Client
	topics   TopicSet
	secrets  map[string]string
	authSecret string
}

func NewAuthPeerService(mqClient mq.Client, topics TopicSet, secrets map[string]string, authSecret string) *AuthPeerService {
	if secrets == nil {
		secrets = make(map[string]string)
	}
	return &AuthPeerService{
		mqClient:   mqClient,
		topics:     topics,
		secrets:    secrets,
		authSecret: authSecret,
	}
}

func (s *AuthPeerService) Start(ctx context.Context) error {
	if err := s.mqClient.Subscribe(ctx, s.topics.PeerAuthVerify, s.handleVerifyReq); err != nil {
		return fmt.Errorf("subscribe auth verify topic: %w", err)
	}
	slog.Info("auth peer service started", "verify_topic", s.topics.PeerAuthVerify)
	return nil
}

func (s *AuthPeerService) SetSecret(key, secret string) {
	s.secrets[key] = secret
}

func (s *AuthPeerService) handleVerifyReq(_ string, payload []byte) {
	var req model.PeerAuthRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		slog.Warn("failed to unmarshal auth request", "error", err)
		return
	}

	expectedSecret, ok := s.secrets[req.Key]
	if !ok || expectedSecret != req.Secret {
		slog.Warn("auth verification failed",
			"peer_id", req.PeerId,
			"key", req.Key,
		)
		s.replyAuth(req, false, "", "invalid key/secret")
		return
	}

	token := generateAuthToken(req.PeerId, expectedSecret, s.authSecret)
	slog.Info("auth verification granted", "peer_id", req.PeerId)
	s.replyAuth(req, true, token, "")
}

func (s *AuthPeerService) replyAuth(req model.PeerAuthRequest, granted bool, token, reason string) {
	resp := model.PeerAuthResponse{
		RequestId: req.RequestId,
		PeerId:    req.PeerId,
		Granted:   granted,
		AuthToken: token,
		TtlSec:    300,
		Reason:    reason,
	}
	data, _ := json.Marshal(resp)
	if err := s.mqClient.Publish(context.Background(), req.ReplyTo, data); err != nil {
		slog.Warn("failed to publish auth response", "error", err, "reply_to", req.ReplyTo)
	}
}

func (s *AuthPeerService) VerifyToken(token, peerId, key string) bool {
	secret, ok := s.secrets[key]
	if !ok {
		return false
	}
	expected := generateAuthToken(peerId, secret, s.authSecret)
	return hmac.Equal([]byte(token), []byte(expected))
}

func generateAuthToken(peerId, peerSecret, authSecret string) string {
	mac := hmac.New(sha256.New, []byte(authSecret))
	mac.Write([]byte(peerId + ":" + peerSecret))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
