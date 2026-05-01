package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
	"dd-core/internal/observability"
	"dd-core/internal/service"
)

type HttpBridge struct {
	mqClient    mq.Client
	topics      service.TopicSet
	peerId      string
	targetUrl   string
	httpClient  *http.Client
	retries     int
	cbMu        sync.Mutex
	cbFails     int
	cbOpenUntil time.Time
}

func NewHttpBridge(mqClient mq.Client, topics service.TopicSet, peerId string, targetUrl string) *HttpBridge {
	return &HttpBridge{
		mqClient:  mqClient,
		topics:    topics,
		peerId:    peerId,
		targetUrl: targetUrl,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        200,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		retries: 2,
	}
}

func (b *HttpBridge) Protocol() model.DdProtocol {
	return model.DdProtocolHttp
}

func (b *HttpBridge) Stop() error {
	return nil
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
	start := time.Now()
	var msg model.DdMessage
	if err := model.DecodeDdMessage(payload, &msg); err != nil {
		slog.Warn("failed to unmarshal sync request", "error", err)
		observability.BridgeRequestsTotal.WithLabelValues("http", "sync", "invalid").Inc()
		b.publishDLQ("sync_unmarshal_failed", topic, payload)
		return
	}
	msg.Normalize()
	msg.Mode = model.DdTransferModeSync
	if msg.Header.TimeoutMs <= 0 {
		msg.Header.TimeoutMs = 5000
	}
	if err := msg.Validate(); err != nil {
		slog.Warn("invalid sync request", "error", err)
		observability.BridgeRequestsTotal.WithLabelValues("http", "sync", "invalid").Inc()
		b.publishDLQ("sync_validate_failed", topic, payload)
		return
	}

	respBody, statusCode := b.doHttp(&msg)
	elapsed := time.Since(start).Milliseconds()
	observability.BridgeLatencyMs.WithLabelValues("http", "sync").Observe(float64(elapsed))
	if statusCode >= 500 {
		observability.BridgeRequestsTotal.WithLabelValues("http", "sync", "error").Inc()
	} else {
		observability.BridgeRequestsTotal.WithLabelValues("http", "sync", "ok").Inc()
	}

	replyTopic := msg.Header.ReplyTo
	if replyTopic == "" {
		replyTopic = b.topics.TransferResponse(msg.Header.SourcePeerId)
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
			TraceId:       msg.Header.TraceId,
		},
		Headers: map[string]string{
			"http-status": strconv.Itoa(statusCode),
		},
		Payload:   respBody,
		CreatedAt: time.Now().UTC(),
	}
	resp.Normalize()

	raw, err := model.EncodeDdMessage(&resp, len(resp.Payload) >= 128)
	if err != nil {
		slog.Warn("failed to marshal sync response", "error", err)
		return
	}

	if err := b.mqClient.Publish(context.Background(), replyTopic, raw); err != nil {
		slog.Warn("failed to publish sync response", "error", err, "topic", replyTopic)
		observability.BridgeRequestsTotal.WithLabelValues("http", "sync", "publish_error").Inc()
		b.publishDLQ("sync_reply_publish_failed", replyTopic, raw)
	}
}

func (b *HttpBridge) handleAsyncEvent(topic string, payload []byte) {
	start := time.Now()
	var msg model.DdMessage
	if err := model.DecodeDdMessage(payload, &msg); err != nil {
		slog.Warn("failed to unmarshal async event", "error", err)
		observability.BridgeRequestsTotal.WithLabelValues("http", "async", "invalid").Inc()
		b.publishDLQ("async_unmarshal_failed", topic, payload)
		return
	}
	msg.Normalize()
	msg.Mode = model.DdTransferModeAsync
	if err := msg.Validate(); err != nil {
		slog.Warn("invalid async event", "error", err)
		observability.BridgeRequestsTotal.WithLabelValues("http", "async", "invalid").Inc()
		b.publishDLQ("async_validate_failed", topic, payload)
		return
	}
	_, status := b.doHttp(&msg)
	elapsed := time.Since(start).Milliseconds()
	observability.BridgeLatencyMs.WithLabelValues("http", "async").Observe(float64(elapsed))
	if status >= 500 {
		observability.BridgeRequestsTotal.WithLabelValues("http", "async", "error").Inc()
	} else {
		observability.BridgeRequestsTotal.WithLabelValues("http", "async", "ok").Inc()
	}
}

func (b *HttpBridge) doHttp(msg *model.DdMessage) ([]byte, int) {
	if b.isCircuitOpen() {
		return []byte(`{"error":"http_circuit_open"}`), http.StatusServiceUnavailable
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
	contentType := fields["content-type"]

	url := b.targetUrl + path
	var lastErr error
	for attempt := 1; attempt <= b.retries; attempt++ {
		req, err := http.NewRequest(method, url, bytes.NewReader(msg.Payload))
		if err != nil {
			slog.Warn("failed to build http request", "error", err, "url", url)
			return []byte(`{"error":"build_request_failed"}`), http.StatusBadRequest
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		for k, v := range fields {
			if k != "method" && k != "path" && k != "content-type" {
				req.Header.Set(k, v)
			}
		}

		resp, err := b.httpClient.Do(req)
		if err != nil {
			lastErr = err
			b.markFailure()
			if attempt < b.retries {
				observability.BridgeRetriesTotal.WithLabelValues("http").Inc()
				time.Sleep(100 * time.Millisecond)
				continue
			}
			slog.Warn("http call failed", "error", err, "url", url, "attempt", attempt)
			return []byte(`{"error":"http_call_failed"}`), http.StatusBadGateway
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			b.markFailure()
			if attempt < b.retries {
				observability.BridgeRetriesTotal.WithLabelValues("http").Inc()
				time.Sleep(100 * time.Millisecond)
				continue
			}
			slog.Warn("failed to read http response", "error", readErr, "attempt", attempt)
			return []byte(`{"error":"read_response_failed"}`), http.StatusBadGateway
		}
		if resp.StatusCode >= 500 && attempt < b.retries {
			b.markFailure()
			observability.BridgeRetriesTotal.WithLabelValues("http").Inc()
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if resp.StatusCode >= 500 {
			b.markFailure()
		} else {
			b.markSuccess()
		}
		slog.Info("http bridge call completed",
			"method", method,
			"url", url,
			"status", resp.StatusCode,
			"peer_id", b.peerId,
			"attempt", attempt,
		)
		return body, resp.StatusCode
	}

	slog.Warn("http bridge exhausted retries", "url", url, "error", lastErr)
	return []byte(`{"error":"http_retry_exhausted"}`), http.StatusBadGateway
}

func (b *HttpBridge) markFailure() {
	b.cbMu.Lock()
	defer b.cbMu.Unlock()
	b.cbFails++
	if b.cbFails >= 3 {
		b.cbOpenUntil = time.Now().Add(5 * time.Second)
	}
}

func (b *HttpBridge) markSuccess() {
	b.cbMu.Lock()
	defer b.cbMu.Unlock()
	b.cbFails = 0
	b.cbOpenUntil = time.Time{}
}

func (b *HttpBridge) isCircuitOpen() bool {
	b.cbMu.Lock()
	defer b.cbMu.Unlock()
	return time.Now().Before(b.cbOpenUntil)
}

func (b *HttpBridge) publishDLQ(reason, topic string, payload []byte) {
	event := map[string]any{
		"protocol": "http",
		"peer_id":  b.peerId,
		"reason":   reason,
		"topic":    topic,
		"payload":  base64.StdEncoding.EncodeToString(payload),
		"ts":       time.Now().UTC(),
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	_ = b.mqClient.Publish(context.Background(), b.topics.DlqBridge(), raw)
}
