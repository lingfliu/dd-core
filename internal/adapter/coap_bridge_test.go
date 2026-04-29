package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
	"dd-core/internal/service"
)

type coapTestServer struct {
	conn     *net.UDPConn
	response []byte
}

func startCoapTestServer(t *testing.T, response []byte) *coapTestServer {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve addr: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}

	s := &coapTestServer{conn: conn, response: response}
	go s.serve()
	return s
}

func (s *coapTestServer) serve() {
	buf := make([]byte, 1500)
	for {
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		resp := buildCoapResponse(buf[:n], s.response)
		s.conn.WriteTo(resp, addr)
	}
}

func (s *coapTestServer) addr() string {
	return fmt.Sprintf("127.0.0.1:%d", s.conn.LocalAddr().(*net.UDPAddr).Port)
}

func (s *coapTestServer) close() {
	s.conn.Close()
}

func buildCoapResponse(req []byte, payload []byte) []byte {
	if len(req) < 4 {
		return nil
	}
	msgId := uint16(req[2])<<8 | uint16(req[3])

	resp := make([]byte, 0, len(payload)+16)
	resp = append(resp, 0x60|(req[0]&0x0f))
	resp = append(resp, 0x45)
	resp = append(resp, byte(msgId>>8), byte(msgId&0xff))

	if len(payload) > 0 {
		resp = append(resp, 0xff)
		resp = append(resp, payload...)
	}
	return resp
}

func TestCoapBridgeSyncRequest(t *testing.T) {
	server := startCoapTestServer(t, []byte(`{"temp":22.5}`))
	defer server.close()

	mock := mq.NewMockClient()
	topics := service.NewDefaultTopicSet("default")

	bridge := NewCoapBridge(mock, topics, "coap-peer", server.addr())
	if err := bridge.Start(context.Background()); err != nil {
		t.Fatalf("start coap bridge failed: %v", err)
	}

	responseCh := make(chan *model.DdMessage, 1)
	responseTopic := "dd/default/transfer/coap-peer/response"
	if err := mock.Subscribe(context.Background(), responseTopic, func(_ string, payload []byte) {
		var msg model.DdMessage
		json.Unmarshal(payload, &msg)
		responseCh <- &msg
	}); err != nil {
		t.Fatalf("subscribe response failed: %v", err)
	}

	syncReq := model.DdMessage{
		Mode:     model.DdTransferModeSync,
		Protocol: model.DdProtocolCoap,
		Resource: "sensor.temp",
		Header: model.DdHeader{
			RequestId:    "coap-req-1",
			SourcePeerId: "caller",
			ReplyTo:      responseTopic,
		},
		Headers: map[string]string{
			"method": "GET",
			"path":   "/sensor/temp",
		},
	}
	raw, _ := json.Marshal(syncReq)

	requestTopic := topics.TransferRequest("coap-peer")
	if err := mock.Publish(context.Background(), requestTopic, raw); err != nil {
		t.Fatalf("publish request failed: %v", err)
	}

	select {
	case resp := <-responseCh:
		if resp.Header.CorrelationId != "coap-req-1" {
			t.Fatalf("expected correlation_id=coap-req-1, got %s", resp.Header.CorrelationId)
		}
		if string(resp.Payload) != `{"temp":22.5}` {
			t.Fatalf("unexpected payload: %s", string(resp.Payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for coap sync response")
	}
}

func TestCoapBridgeIgnoresNonCoapProtocol(t *testing.T) {
	server := startCoapTestServer(t, []byte(`ok`))
	defer server.close()

	mock := mq.NewMockClient()
	topics := service.NewDefaultTopicSet("default")

	bridge := NewCoapBridge(mock, topics, "coap-peer", server.addr())
	if err := bridge.Start(context.Background()); err != nil {
		t.Fatalf("start coap bridge failed: %v", err)
	}

	httpReq := model.DdMessage{
		Mode:     model.DdTransferModeAsync,
		Protocol: model.DdProtocolHttp,
		Resource: "test",
		Header: model.DdHeader{
			RequestId: "http-req-1",
		},
	}
	raw, _ := json.Marshal(httpReq)

	eventTopic := topics.EventPublish("coap-peer")
	if err := mock.Publish(context.Background(), eventTopic, raw); err != nil {
		t.Fatalf("publish event failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
}

func TestCoapBridgeProtocol(t *testing.T) {
	bridge := NewCoapBridge(mq.NewMockClient(), service.NewDefaultTopicSet("t"), "p1", "127.0.0.1:5683")
	if bridge.Protocol() != model.DdProtocolCoap {
		t.Fatalf("expected coap protocol, got %s", bridge.Protocol())
	}
}
