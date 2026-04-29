package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
	"dd-core/internal/service"
)

type CoapBridge struct {
	mqClient  mq.Client
	topics    service.TopicSet
	peerId    string
	targetUrl string
}

func NewCoapBridge(mqClient mq.Client, topics service.TopicSet, peerId string, targetUrl string) *CoapBridge {
	return &CoapBridge{
		mqClient:  mqClient,
		topics:    topics,
		peerId:    peerId,
		targetUrl: targetUrl,
	}
}

func (b *CoapBridge) Protocol() model.DdProtocol {
	return model.DdProtocolCoap
}

func (b *CoapBridge) Start(ctx context.Context) error {
	requestTopic := b.topics.TransferRequest(b.peerId)
	if err := b.mqClient.Subscribe(ctx, requestTopic, b.handleSyncRequest); err != nil {
		return fmt.Errorf("subscribe request topic %s: %w", requestTopic, err)
	}

	eventTopic := b.topics.EventPublish(b.peerId)
	if err := b.mqClient.Subscribe(ctx, eventTopic, b.handleAsyncEvent); err != nil {
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

func (b *CoapBridge) handleSyncRequest(_ string, payload []byte) {
	var msg model.DdMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		slog.Warn("failed to unmarshal coap sync request", "error", err)
		return
	}

	if msg.Protocol != model.DdProtocolCoap {
		return
	}

	respBody := b.doCoap(&msg)

	replyTopic := msg.Header.ReplyTo
	if replyTopic == "" {
		replyTopic = fmt.Sprintf("dd/default/transfer/%s/response", msg.Header.SourcePeerId)
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
		},
		Payload:   respBody,
		CreatedAt: time.Now().UTC(),
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		slog.Warn("failed to marshal coap sync response", "error", err)
		return
	}

	if err := b.mqClient.Publish(context.Background(), replyTopic, raw); err != nil {
		slog.Warn("failed to publish coap sync response", "error", err, "topic", replyTopic)
	}
}

func (b *CoapBridge) handleAsyncEvent(_ string, payload []byte) {
	var msg model.DdMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		slog.Warn("failed to unmarshal coap async event", "error", err)
		return
	}

	if msg.Protocol != model.DdProtocolCoap {
		return
	}

	b.doCoap(&msg)
}

func (b *CoapBridge) doCoap(msg *model.DdMessage) []byte {
	method := msg.Headers["method"]
	if method == "" {
		method = "GET"
	}
	path := msg.Headers["path"]
	if path == "" {
		path = "/"
	}

	reqPkt := buildCoapRequest(method, path, msg.Payload)

	addr, err := net.ResolveUDPAddr("udp", b.targetUrl)
	if err != nil {
		slog.Warn("coap resolve addr failed", "error", err, "target", b.targetUrl)
		return []byte(`{"error":"coap_resolve_failed"}`)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		slog.Warn("coap dial failed", "error", err, "target", b.targetUrl)
		return []byte(`{"error":"coap_dial_failed"}`)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return []byte(`{"error":"coap_deadline_failed"}`)
	}

	if _, err := conn.Write(reqPkt); err != nil {
		slog.Warn("coap write failed", "error", err, "method", method, "path", path)
		return []byte(`{"error":"coap_write_failed"}`)
	}

	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		slog.Warn("coap read failed", "error", err, "method", method, "path", path)
		return []byte(`{"error":"coap_read_failed"}`)
	}

	payload := parseCoapPayload(buf[:n])

	slog.Info("coap bridge call completed",
		"method", method,
		"path", path,
		"peer_id", b.peerId,
		"resp_size", len(payload),
	)
	return payload
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

	msgId := uint16(time.Now().UnixNano() % 65536)

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
	delta := num
	extended := false
	result := make([]byte, 0)

	if delta >= 13 {
		extended = true
	}

	if extended {
		result = append(result, byte(13)<<4|byte(len(val)-1))
		result = append(result, byte(delta-13))
	} else {
		result = append(result, byte(delta)<<4|byte(len(val)-1))
	}

	result = append(result, []byte(val)...)
	return result
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
		length := int(data[pos]&0x0f) + 1
		pos++

		if delta == 13 && pos < len(data) {
			delta = int(data[pos]) + 13
			pos++
		} else if delta == 14 && pos+1 < len(data) {
			delta = int(data[pos])<<8 | int(data[pos+1]) + 269
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
