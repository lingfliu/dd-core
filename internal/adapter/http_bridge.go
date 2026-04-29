package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
	"dd-core/internal/service"
)

type HttpBridge struct {
	mqClient   mq.Client
	topics     service.TopicSet
	peerId     string
	targetUrl  string
	httpClient *http.Client
}

func NewHttpBridge(mqClient mq.Client, topics service.TopicSet, peerId string, targetUrl string) *HttpBridge {
	return &HttpBridge{
		mqClient:  mqClient,
		topics:    topics,
		peerId:    peerId,
		targetUrl: targetUrl,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (b *HttpBridge) Protocol() model.DdProtocol {
	return model.DdProtocolHttp
}

func (b *HttpBridge) Start(ctx context.Context) error {
	requestTopic := b.topics.TransferRequest(b.peerId)
	if err := b.mqClient.Subscribe(ctx, requestTopic, b.handleSyncRequest); err != nil {
		return fmt.Errorf("subscribe request topic %s: %w", requestTopic, err)
	}

	eventTopic := b.topics.EventPublish(b.peerId)
	if err := b.mqClient.Subscribe(ctx, eventTopic, b.handleAsyncEvent); err != nil {
		return fmt.Errorf("subscribe event topic %s: %w", eventTopic, err)
	}

	slog.Info("http bridge subscribed",
		"peer_id", b.peerId,
		"request_topic", requestTopic,
		"event_topic", eventTopic,
		"target_url", b.targetUrl,
	)
	return nil
}

func (b *HttpBridge) handleSyncRequest(topic string, payload []byte) {
	var msg model.DdMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		slog.Warn("failed to unmarshal sync request", "error", err)
		return
	}

	respBody := b.doHttp(&msg)

	replyTopic := msg.Header.ReplyTo
	if replyTopic == "" {
		replyTopic = fmt.Sprintf("dd/default/transfer/%s/response", msg.Header.SourcePeerId)
	}

	resp := model.DdMessage{
		Mode:     model.DdTransferModeSync,
		Protocol: model.DdProtocolHttp,
		Resource: msg.Resource,
		Header: model.DdHeader{
			RequestId:     "resp-" + msg.Header.RequestId,
			CorrelationId: msg.Header.RequestId,
			SourcePeerId:  b.peerId,
			TargetPeerId:  msg.Header.SourcePeerId,
		},
		Payload:   respBody,
		CreatedAt: time.Now().UTC(),
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		slog.Warn("failed to marshal sync response", "error", err)
		return
	}

	if err := b.mqClient.Publish(context.Background(), replyTopic, raw); err != nil {
		slog.Warn("failed to publish sync response", "error", err, "topic", replyTopic)
	}
}

func (b *HttpBridge) handleAsyncEvent(topic string, payload []byte) {
	var msg model.DdMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		slog.Warn("failed to unmarshal async event", "error", err)
		return
	}
	b.doHttp(&msg)
}

func (b *HttpBridge) doHttp(msg *model.DdMessage) []byte {
	method := msg.Headers["method"]
	if method == "" {
		method = http.MethodGet
	}
	path := msg.Headers["path"]
	if path == "" {
		path = "/"
	}
	contentType := msg.Headers["content-type"]

	url := b.targetUrl + path
	req, err := http.NewRequest(method, url, bytes.NewReader(msg.Payload))
	if err != nil {
		slog.Warn("failed to build http request", "error", err, "url", url)
		return []byte(`{"error":"build_request_failed"}`)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range msg.Headers {
		if k != "method" && k != "path" && k != "content-type" {
			req.Header.Set(k, v)
		}
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		slog.Warn("http call failed", "error", err, "url", url)
		return []byte(`{"error":"http_call_failed"}`)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("failed to read http response", "error", err)
		return []byte(`{"error":"read_response_failed"}`)
	}

	slog.Info("http bridge call completed",
		"method", method,
		"url", url,
		"status", resp.StatusCode,
		"peer_id", b.peerId,
	)
	return body
}
