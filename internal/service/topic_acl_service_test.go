package service

import "testing"

func TestTopicAclAllowAndDenyPrecedence(t *testing.T) {
	acl := NewTopicAclService()
	acl.SetRules([]TopicAclRule{
		{PeerId: "peer-a", Action: AclActionPub, Pattern: "dd/default/transfer/#", Allow: true},
		{PeerId: "peer-a", Action: AclActionPub, Pattern: "dd/default/transfer/secret/#", Allow: false},
	})

	if !acl.Authorize("peer-a", AclActionPub, "dd/default/transfer/order/request") {
		t.Fatal("expected allow for normal transfer topic")
	}
	if acl.Authorize("peer-a", AclActionPub, "dd/default/transfer/secret/request") {
		t.Fatal("expected deny precedence for secret topic")
	}
}

func TestTopicAclMqttWildcardMatching(t *testing.T) {
	acl := NewTopicAclService()
	acl.SetRules([]TopicAclRule{
		{PeerId: "peer-b", Action: AclActionSub, Pattern: "dd/+/event/+/publish", Allow: true},
	})
	if !acl.Authorize("peer-b", AclActionSub, "dd/default/event/sensor.publish/publish") {
		t.Fatal("expected wildcard match allow")
	}
	if acl.Authorize("peer-b", AclActionSub, "dd/default/event/sensor.publish/data") {
		t.Fatal("expected non-match deny")
	}
}
