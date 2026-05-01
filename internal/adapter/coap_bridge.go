package adapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
	"dd-core/internal/observability"
	"dd-core/internal/service"
)

type CoapBridge struct {
	mqClient    mq.Client
	topics      service.TopicSet
	peerId      string
	targetUrl   string
	retries     int
	workerCount int
	queueSize   int
	reqCh       chan coapTask
	stopCh      chan struct{}
	wg          sync.WaitGroup
	stopOnce    sync.Once
	cbMu        sync.Mutex
	cbFails     int
	cbOpenUntil time.Time
}

type coapTask struct {
	topic   string
	payload []byte
	isSync  bool
}

var coapMsgIDCounter uint32

const maxCoapPayloadBytes = 1024

func NewCoapBridge(mqClient mq.Client, topics service.TopicSet, peerId string, targetUrl string) *CoapBridge {
	b := &CoapBridge{
		mqClient:    mqClient,
		topics:      topics,
		peerId:      peerId,
		targetUrl:   targetUrl,
		retries:     2,
		workerCount: 8,
		queueSize:   256,
		stopCh:      make(chan struct{}),
	}
	b.reqCh = make(chan coapTask, b.queueSize)
	return b
}

func (b *CoapBridge) Protocol() model.DdProtocol {
	return model.DdProtocolCoap
}

func (b *CoapBridge) Stop() error {
	b.stopOnce.Do(func() {
		close(b.stopCh)
		close(b.reqCh)
		b.wg.Wait()
	})
	return nil
}

func (b *CoapBridge) Start(ctx context.Context) error {
	for i := 0; i < b.workerCount; i++ {
		b.wg.Add(1)
		go b.worker()
	}
	requestTopic := b.topics.TransferRequest(b.peerId)
	if err := b.mqClient.Subscribe(ctx, requestTopic, func(topic string, payload []byte) {
		b.enqueueTask(coapTask{topic: topic, payload: payload, isSync: true})
	}); err != nil {
		return fmt.Errorf("subscribe request topic %s: %w", requestTopic, err)
	}

	eventTopic := b.topics.EventPublish(b.peerId)
	if err := b.mqClient.Subscribe(ctx, eventTopic, func(topic string, payload []byte) {
		b.enqueueTask(coapTask{topic: topic, payload: payload, isSync: false})
	}); err != nil {
		return fmt.Errorf("subscribe event topic %s: %w", eventTopic, err)
	}

	slog.Info("coap bridge subscribed",
		"peer_id", b.peerId,
		"request_topic", requestTopic,
		"event_topic", eventTopic,
		"target_url", b.targetUrl,
	)
	return nil
}

func (b *CoapBridge) worker() {
	defer b.wg.Done()
	for task := range b.reqCh {
		if task.isSync {
			b.handleSyncRequest(task.topic, task.payload)
		} else {
			b.handleAsyncEvent(task.topic, task.payload)
		}
	}
}

func (b *CoapBridge) enqueueTask(task coapTask) {
	select {
	case <-b.stopCh:
		return
	default:
	}
	select {
	case b.reqCh <- task:
	default:
		reason := "async_queue_full"
		mode := "async"
		if task.isSync {
			reason = "sync_queue_full"
			mode = "sync"
		}
		observability.BridgeRequestsTotal.WithLabelValues("coap", mode, "queue_full").Inc()
		b.publishDLQ(reason, task.topic, task.payload)
	}
}

func (b *CoapBridge) handleSyncRequest(topic string, payload []byte) {
	start := time.Now()
	var msg model.DdMessage
	if err := model.DecodeDdMessage(payload, &msg); err != nil {
		slog.Warn("failed to unmarshal coap sync request", "error", err)
		observability.BridgeRequestsTotal.WithLabelValues("coap", "sync", "invalid").Inc()
		b.publishDLQ("sync_unmarshal_failed", topic, payload)
		return
	}
	msg.Normalize()
	msg.Mode = model.DdTransferModeSync
	if msg.Header.TimeoutMs <= 0 {
		msg.Header.TimeoutMs = 5000
	}
	if err := msg.Validate(); err != nil {
		slog.Warn("invalid coap sync request", "error", err)
		observability.BridgeRequestsTotal.WithLabelValues("coap", "sync", "invalid").Inc()
		b.publishDLQ("sync_validate_failed", topic, payload)
		return
	}

	if msg.Protocol != model.DdProtocolCoap {
		return
	}

	respBody := b.doCoap(&msg)
	elapsed := time.Since(start).Milliseconds()
	observability.BridgeLatencyMs.WithLabelValues("coap", "sync").Observe(float64(elapsed))
	if isCoapFailureResponse(respBody) {
		observability.BridgeRequestsTotal.WithLabelValues("coap", "sync", "error").Inc()
	} else {
		observability.BridgeRequestsTotal.WithLabelValues("coap", "sync", "ok").Inc()
	}

	replyTopic := msg.Header.ReplyTo
	if replyTopic == "" {
		replyTopic = b.topics.TransferResponse(msg.Header.SourcePeerId)
	}

	resp := model.DdMessage{
		Mode:     model.DdTransferModeSync,
		Protocol: model.DdProtocolCoap,
		Resource: msg.Resource,
		Header: model.DdHeader{
			RequestId:     "resp-" + msg.Header.RequestId,
			CorrelationId: msg.Header.RequestId,
			SourcePeerId:  b.peerId,
			TargetPeerId:  msg.Header.SourcePeerId,
			TraceId:       msg.Header.TraceId,
		},
		Payload:   respBody,
		CreatedAt: time.Now().UTC(),
	}
	resp.Normalize()

	raw, err := model.EncodeDdMessage(&resp, len(resp.Payload) >= 128)
	if err != nil {
		slog.Warn("failed to marshal coap sync response", "error", err)
		return
	}

	if err := b.mqClient.Publish(context.Background(), replyTopic, raw); err != nil {
		slog.Warn("failed to publish coap sync response", "error", err, "topic", replyTopic)
		observability.BridgeRequestsTotal.WithLabelValues("coap", "sync", "publish_error").Inc()
		b.publishDLQ("sync_reply_publish_failed", replyTopic, raw)
	}
}

func (b *CoapBridge) handleAsyncEvent(topic string, payload []byte) {
	start := time.Now()
	var msg model.DdMessage
	if err := model.DecodeDdMessage(payload, &msg); err != nil {
		slog.Warn("failed to unmarshal coap async event", "error", err)
		observability.BridgeRequestsTotal.WithLabelValues("coap", "async", "invalid").Inc()
		b.publishDLQ("async_unmarshal_failed", topic, payload)
		return
	}
	msg.Normalize()
	msg.Mode = model.DdTransferModeAsync
	if err := msg.Validate(); err != nil {
		slog.Warn("invalid coap async event", "error", err)
		observability.BridgeRequestsTotal.WithLabelValues("coap", "async", "invalid").Inc()
		b.publishDLQ("async_validate_failed", topic, payload)
		return
	}

	if msg.Protocol != model.DdProtocolCoap {
		return
	}

	respBody := b.doCoap(&msg)
	elapsed := time.Since(start).Milliseconds()
	observability.BridgeLatencyMs.WithLabelValues("coap", "async").Observe(float64(elapsed))
	if isCoapFailureResponse(respBody) {
		observability.BridgeRequestsTotal.WithLabelValues("coap", "async", "error").Inc()
	} else {
		observability.BridgeRequestsTotal.WithLabelValues("coap", "async", "ok").Inc()
	}
}

func (b *CoapBridge) doCoap(msg *model.DdMessage) []byte {
	if b.isCircuitOpen() {
		return []byte(`{"error":"coap_circuit_open"}`)
	}
	if len(msg.Payload) > maxCoapPayloadBytes {
		return []byte(`{"error":"coap_payload_too_large"}`)
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

	reqPkt := buildCoapRequest(method, path, msg.Payload)

	addr, err := net.ResolveUDPAddr("udp", b.targetUrl)
	if err != nil {
		slog.Warn("coap resolve addr failed", "error", err, "target", b.targetUrl)
		return []byte(`{"error":"coap_resolve_failed"}`)
	}

	timeout := 5 * time.Second
	if msg.Header.TimeoutMs > 0 {
		timeout = time.Duration(msg.Header.TimeoutMs) * time.Millisecond
	}
	for attempt := 1; attempt <= b.retries; attempt++ {
		conn, dialErr := net.DialUDP("udp", nil, addr)
		if dialErr != nil {
			b.markFailure()
			if attempt < b.retries {
				observability.BridgeRetriesTotal.WithLabelValues("coap").Inc()
				time.Sleep(100 * time.Millisecond)
				continue
			}
			slog.Warn("coap dial failed", "error", dialErr, "target", b.targetUrl, "attempt", attempt)
			return []byte(`{"error":"coap_dial_failed"}`)
		}

		if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
			conn.Close()
			return []byte(`{"error":"coap_deadline_failed"}`)
		}
		if _, err := conn.Write(reqPkt); err != nil {
			conn.Close()
			b.markFailure()
			if attempt < b.retries {
				observability.BridgeRetriesTotal.WithLabelValues("coap").Inc()
				time.Sleep(100 * time.Millisecond)
				continue
			}
			slog.Warn("coap write failed", "error", err, "method", method, "path", path, "attempt", attempt)
			return []byte(`{"error":"coap_write_failed"}`)
		}

		buf := make([]byte, 1500)
		n, readErr := conn.Read(buf)
		conn.Close()
		if readErr != nil {
			if errors.Is(readErr, net.ErrClosed) {
				return []byte(`{"error":"coap_conn_closed"}`)
			}
			b.markFailure()
			if attempt < b.retries {
				observability.BridgeRetriesTotal.WithLabelValues("coap").Inc()
				time.Sleep(100 * time.Millisecond)
				continue
			}
			slog.Warn("coap read failed", "error", readErr, "method", method, "path", path, "attempt", attempt)
			return []byte(`{"error":"coap_read_failed"}`)
		}

		payload := parseCoapPayload(buf[:n])
		b.markSuccess()
		slog.Info("coap bridge call completed",
			"method", method,
			"path", path,
			"peer_id", b.peerId,
			"resp_size", len(payload),
			"attempt", attempt,
		)
		return payload
	}
	b.markFailure()
	return []byte(`{"error":"coap_retry_exhausted"}`)
}

func isCoapFailureResponse(resp []byte) bool {
	switch string(resp) {
	case `{"error":"coap_dial_failed"}`,
		`{"error":"coap_write_failed"}`,
		`{"error":"coap_read_failed"}`,
		`{"error":"coap_retry_exhausted"}`,
		`{"error":"coap_payload_too_large"}`,
		`{"error":"coap_circuit_open"}`,
		`{"error":"coap_conn_closed"}`:
		return true
	default:
		return false
	}
}

func (b *CoapBridge) markFailure() {
	b.cbMu.Lock()
	defer b.cbMu.Unlock()
	b.cbFails++
	if b.cbFails >= 3 {
		b.cbOpenUntil = time.Now().Add(5 * time.Second)
	}
}

func (b *CoapBridge) markSuccess() {
	b.cbMu.Lock()
	defer b.cbMu.Unlock()
	b.cbFails = 0
	b.cbOpenUntil = time.Time{}
}

func (b *CoapBridge) isCircuitOpen() bool {
	b.cbMu.Lock()
	defer b.cbMu.Unlock()
	return time.Now().Before(b.cbOpenUntil)
}

func (b *CoapBridge) publishDLQ(reason, topic string, payload []byte) {
	event := map[string]any{
		"protocol": "coap",
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

func buildCoapRequest(method, path string, payload []byte) []byte {
	var code byte
	switch method {
	case "GET":
		code = 0x01
	case "POST":
		code = 0x02
	case "PUT":
		code = 0x03
	case "DELETE":
		code = 0x04
	default:
		code = 0x01
	}

	msgId := uint16(atomic.AddUint32(&coapMsgIDCounter, 1) & 0xffff)

	pkt := make([]byte, 0, 256)
	pkt = append(pkt, 0x40)
	pkt = append(pkt, code)
	pkt = append(pkt, byte(msgId>>8), byte(msgId&0xff))

	pkt = append(pkt, encodeCoapOption(11, path)...)

	if len(payload) > 0 {
		pkt = append(pkt, 0xff)
		pkt = append(pkt, payload...)
	}

	return pkt
}

func encodeCoapOption(num int, val string) []byte {
	deltaNibble, deltaExt := encodeCoapOptionNibble(num)
	valBytes := []byte(val)
	lengthNibble, lengthExt := encodeCoapOptionNibble(len(valBytes))

	result := make([]byte, 0, 1+len(deltaExt)+len(lengthExt)+len(valBytes))
	result = append(result, byte(deltaNibble<<4|lengthNibble))
	result = append(result, deltaExt...)
	result = append(result, lengthExt...)
	result = append(result, valBytes...)
	return result
}

func encodeCoapOptionNibble(v int) (int, []byte) {
	switch {
	case v < 13:
		return v, nil
	case v < 269:
		return 13, []byte{byte(v - 13)}
	default:
		n := v - 269
		return 14, []byte{byte(n >> 8), byte(n & 0xff)}
	}
}

func parseCoapPayload(data []byte) []byte {
	if len(data) < 4 {
		return nil
	}

	tkl := data[0] & 0x0f
	pos := 4 + int(tkl)

	code := data[1]
	codeClass := code >> 5
	codeDetail := code & 0x1f

	if pos >= len(data) {
		return nil
	}

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
			delta = int(data[pos]) + 13
			pos++
		} else if delta == 14 && pos+1 < len(data) {
			delta = (int(data[pos])<<8 | int(data[pos+1])) + 269
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

	_ = codeClass
	_ = codeDetail
	return nil
}
