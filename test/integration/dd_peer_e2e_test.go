package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"dd-core/internal/adapter"
	"dd-core/internal/model"
	"dd-core/internal/mq"
	"dd-core/internal/service"
	"dd-core/test/testutil"
)

type peerApiServer struct {
	name     string
	server   *httptest.Server
	mu       sync.Mutex
	getHits  int
	postHits int
	postCh   chan string
}

func newPeerApiServer(name string) *peerApiServer {
	p := &peerApiServer{
		name:   name,
		postCh: make(chan string, 8),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/resource", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			p.mu.Lock()
			p.getHits++
			p.mu.Unlock()
			_, _ = w.Write([]byte(fmt.Sprintf(`{"peer":"%s","method":"GET"}`, p.name)))
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			p.mu.Lock()
			p.postHits++
			p.mu.Unlock()
			select {
			case p.postCh <- string(body):
			default:
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{"peer":"%s","method":"POST","body":%q}`, p.name, string(body))))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	p.server = httptest.NewServer(mux)
	return p
}

func (p *peerApiServer) close() {
	p.server.Close()
}

func TestDdPeerABHubRegistryAndSyncAsyncAccess(t *testing.T) {
	ctx := context.Background()
	log := testutil.NewLogger(t, "dd-peer-e2e")
	mqClient := mq.NewMockClient()
	topics := service.NewDefaultTopicSet("default")
	log.Infof("initialized mock mq and topic set")

	peerAApi := newPeerApiServer("A")
	defer peerAApi.close()
	peerBApi := newPeerApiServer("B")
	defer peerBApi.close()
	log.Infof("started API servers for peer-a and peer-b")

	bridgeA := adapter.NewHttpBridge(mqClient, topics, "peer-a", peerAApi.server.URL)
	if err := bridgeA.Start(ctx); err != nil {
		t.Fatalf("start http bridge for peer-a failed: %v", err)
	}
	bridgeB := adapter.NewHttpBridge(mqClient, topics, "peer-b", peerBApi.server.URL)
	if err := bridgeB.Start(ctx); err != nil {
		t.Fatalf("start http bridge for peer-b failed: %v", err)
	}
	log.Infof("http bridges started for peer-a and peer-b")

	hub := service.NewPeerRegistryService(mqClient, topics, 10*time.Second)
	if err := hub.Start(ctx); err != nil {
		t.Fatalf("start hub registry failed: %v", err)
	}
	log.Infof("hub registry service started")

	regA := model.DdPeerRegisterEvent{
		Peer: model.DdPeerInfo{
			Id:   "peer-a",
			Name: "peer-a",
			Role: "edge",
		},
	}
	regB := model.DdPeerRegisterEvent{
		Peer: model.DdPeerInfo{
			Id:   "peer-b",
			Name: "peer-b",
			Role: "edge",
		},
	}
	rawA, _ := json.Marshal(regA)
	rawB, _ := json.Marshal(regB)
	mqClient.Emit(topics.PeerRegister, rawA)
	mqClient.Emit(topics.PeerRegister, rawB)
	log.Infof("peer register events emitted")

	reportA := model.DdPeerResourceReportEvent{PeerId: "peer-a", Role: "edge", Timestamp: time.Now().UTC()}
	reportA.Resources.Apis = []map[string]string{
		{"name": "peer-a.resource.get"},
		{"name": "peer-a.resource.post"},
	}
	reportB := model.DdPeerResourceReportEvent{PeerId: "peer-b", Role: "edge", Timestamp: time.Now().UTC()}
	reportB.Resources.Apis = []map[string]string{
		{"name": "peer-b.resource.get"},
		{"name": "peer-b.resource.post"},
	}
	rawReportA, _ := json.Marshal(reportA)
	rawReportB, _ := json.Marshal(reportB)
	mqClient.Emit(topics.PeerResourceReport, rawReportA)
	mqClient.Emit(topics.PeerResourceReport, rawReportB)
	log.Infof("peer resource report events emitted")

	hbA, _ := json.Marshal(model.DdPeerHeartbeatEvent{PeerId: "peer-a", Timestamp: time.Now().UTC()})
	hbB, _ := json.Marshal(model.DdPeerHeartbeatEvent{PeerId: "peer-b", Timestamp: time.Now().UTC()})
	mqClient.Emit(topics.PeerHeartbeat, hbA)
	mqClient.Emit(topics.PeerHeartbeat, hbB)
	log.Infof("peer heartbeat events emitted")

	allPeers := hub.GetPeers("")
	if len(allPeers) != 2 {
		t.Fatalf("expected 2 registered peers in hub, got %d", len(allPeers))
	}
	log.Infof("hub registry contains %d active peers", len(allPeers))

	peerAService := service.NewDdDataService(mqClient, 1500*time.Millisecond)
	peerAResponseTopic := "dd/default/transfer/peer-a/response"
	if err := peerAService.SubscribeSyncResponses(ctx, peerAResponseTopic); err != nil {
		t.Fatalf("subscribe sync responses for peer-a failed: %v", err)
	}

	syncReq := &model.DdMessage{
		Protocol: model.DdProtocolHttp,
		Resource: "peer-b.resource.get",
		Header: model.DdHeader{
			RequestId:    "req-a-to-b-get-1",
			SourcePeerId: "peer-a",
			TargetPeerId: "peer-b",
			ReplyTo:      peerAResponseTopic,
			TimeoutMs:    1000,
		},
		Headers: map[string]string{
			"method": http.MethodGet,
			"path":   "/resource",
		},
	}

	syncResp, err := peerAService.SendSync(ctx, "dd/default/transfer/peer-b/request", syncReq)
	if err != nil {
		t.Fatalf("sync request A->B failed: %v", err)
	}
	if !bytes.Contains(syncResp.Payload, []byte(`"peer":"B"`)) {
		t.Fatalf("unexpected sync response payload: %s", string(syncResp.Payload))
	}
	log.Infof("sync request A->B succeeded, payload=%s", string(syncResp.Payload))

	peerBService := service.NewDdDataService(mqClient, 1500*time.Millisecond)
	asyncReq := &model.DdMessage{
		Protocol: model.DdProtocolHttp,
		Resource: "peer-a.resource.post",
		Header: model.DdHeader{
			RequestId:    "req-b-to-a-post-1",
			SourcePeerId: "peer-b",
			TargetPeerId: "peer-a",
		},
		Headers: map[string]string{
			"method": http.MethodPost,
			"path":   "/resource",
		},
		Payload: []byte(`{"from":"peer-b","mode":"async"}`),
	}
	if err := peerBService.SendAsync(ctx, "dd/default/event/peer-a/publish", asyncReq); err != nil {
		t.Fatalf("async request B->A failed: %v", err)
	}
	log.Infof("async request B->A published")

	select {
	case got := <-peerAApi.postCh:
		if !bytes.Contains([]byte(got), []byte(`"mode":"async"`)) {
			t.Fatalf("unexpected async post body delivered to peer-a: %s", got)
		}
		log.Infof("async delivery to peer-a confirmed, body=%s", got)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting async delivery to peer-a POST API")
	}
}
