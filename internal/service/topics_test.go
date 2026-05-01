package service

import "testing"

func TestTransferResponseByRequestTopics(t *testing.T) {
	topics := NewDefaultTopicSet("default")
	if got, want := topics.TransferResponseByRequest("req-1"), "dd/default/transfer/response/req-1"; got != want {
		t.Fatalf("TransferResponseByRequest=%s want=%s", got, want)
	}
	if got, want := topics.TransferResponseStream(), "dd/default/transfer/+/response"; got != want {
		t.Fatalf("TransferResponseStream=%s want=%s", got, want)
	}
	if got, want := topics.TransferResponseByRequestStream(), "dd/default/transfer/response/+"; got != want {
		t.Fatalf("TransferResponseByRequestStream=%s want=%s", got, want)
	}
}
