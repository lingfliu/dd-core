package mq

import "testing"

func TestAliasTopicTransferOnly(t *testing.T) {
	c := &MqttClient{topicAliasEnabled: true}
	transfer := "dd/default/transfer/device-a/request"
	aliased := c.aliasTopic(transfer)
	if aliased == transfer {
		t.Fatal("expected transfer topic to be aliased")
	}
	if wantPrefix := "dd/default/ta/"; len(aliased) < len(wantPrefix) || aliased[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("unexpected aliased topic: %s", aliased)
	}
}

func TestAliasTopicSkipsWildcardsAndEvents(t *testing.T) {
	c := &MqttClient{topicAliasEnabled: true}
	cases := []string{
		"dd/default/transfer/+/response",
		"dd/default/event/sensor/publish",
		"external/topic",
	}
	for _, topic := range cases {
		if got := c.aliasTopic(topic); got != topic {
			t.Fatalf("topic %s should not be aliased, got %s", topic, got)
		}
	}
}
