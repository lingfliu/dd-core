package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"dd-core/internal/model"
	"dd-core/internal/mq"
)

func TestStreamServiceOpenAndClose(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")
	svc := NewStreamService(mock, topics)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start stream service failed: %v", err)
	}

	evt := model.StreamOpenEvent{
		StreamId:      "strm-test-1",
		Resource:      "video.rtsp.camera1",
		Profile:       model.StreamProfileMinimal,
		SourcePeerId:  "peer-a",
		TargetPeerIds: []string{"peer-b"},
		IdleTimeoutMs: 10000,
	}
	raw, _ := json.Marshal(evt)
	mock.Emit(topics.StreamOpen(evt.Resource), raw)

	session, ok := svc.GetSession("strm-test-1")
	if !ok {
		t.Fatal("expected stream session to exist")
	}
	if session.Status != model.StreamStatusActive {
		t.Fatalf("expected active status, got %s", session.Status)
	}
	if session.Profile != model.StreamProfileMinimal {
		t.Fatalf("expected minimal profile, got %s", session.Profile)
	}

	closeEvt := model.StreamCloseEvent{
		StreamId: "strm-test-1",
		Reason:   "done",
	}
	rawClose, _ := json.Marshal(closeEvt)
	mock.Emit(topics.StreamClose("*"), rawClose)

	session, ok = svc.GetSession("strm-test-1")
	if !ok {
		t.Fatal("expected stream session to still exist after close")
	}
	if session.Status != model.StreamStatusClosed {
		t.Fatalf("expected closed status, got %s", session.Status)
	}
}

func TestStreamServiceDefaultProfile(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")
	svc := NewStreamService(mock, topics)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start stream service failed: %v", err)
	}

	evt := model.StreamOpenEvent{
		StreamId: "strm-no-profile",
		Resource: "audio.stream",
	}
	raw, _ := json.Marshal(evt)
	mock.Emit(topics.StreamOpen(evt.Resource), raw)

	session, ok := svc.GetSession("strm-no-profile")
	if !ok {
		t.Fatal("expected stream session to exist")
	}
	if session.Profile != model.StreamProfileFull {
		t.Fatalf("expected full profile as default, got %s", session.Profile)
	}
}

func TestStreamServiceSweepIdle(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")
	svc := NewStreamService(mock, topics)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start stream service failed: %v", err)
	}

	evt := model.StreamOpenEvent{
		StreamId:      "strm-idle-1",
		Resource:      "video.stream",
		IdleTimeoutMs: 100,
	}
	raw, _ := json.Marshal(evt)
	mock.Emit(topics.StreamOpen(evt.Resource), raw)

	svc.SweepIdle(time.Now().UTC().Add(200*time.Millisecond), time.Second)

	session, ok := svc.GetSession("strm-idle-1")
	if !ok {
		t.Fatal("expected stream session to exist")
	}
	if session.Status != model.StreamStatusTimeout {
		t.Fatalf("expected timeout status, got %s", session.Status)
	}
}

func TestStreamServiceList(t *testing.T) {
	mock := mq.NewMockClient()
	topics := NewDefaultTopicSet("default")
	svc := NewStreamService(mock, topics)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start stream service failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		evt := model.StreamOpenEvent{
			StreamId: "strm-" + string(rune('A'+i)),
			Resource: "test.stream",
		}
		raw, _ := json.Marshal(evt)
		mock.Emit(topics.StreamOpen(evt.Resource), raw)
	}

	sessions := svc.ListSessions()
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}
}
