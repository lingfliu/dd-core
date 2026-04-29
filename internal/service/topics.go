package service

import "fmt"

type TopicSet struct {
	PeerRegister         string
	PeerHeartbeat        string
	PeerQuery            string
	PeerResourceReport   string
	PeerUnregister       string
	HubBroadcast         string
	AuthBroadcast        string
	PeerResourceRegister string
	PeerAuthVerify       string
	PeerListQuery        string

	tenant string
}

func NewDefaultTopicSet(tenant string) TopicSet {
	base := fmt.Sprintf("dd/%s", tenant)
	return TopicSet{
		PeerRegister:         base + "/peer/register",
		PeerHeartbeat:        base + "/peer/heartbeat",
		PeerQuery:            base + "/peer/query",
		PeerResourceReport:   base + "/peer/resource/report",
		PeerUnregister:       base + "/peer/unregister",
		HubBroadcast:         base + "/peer/hub/broadcast",
		AuthBroadcast:        base + "/peer/auth/broadcast",
		PeerResourceRegister: base + "/peer/resource/register",
		PeerAuthVerify:       base + "/peer/auth/verify",
		PeerListQuery:        base + "/peer/list/query",
		tenant:               tenant,
	}
}

func (t TopicSet) TransferRequest(resource string) string {
	return fmt.Sprintf("dd/%s/transfer/%s/request", t.tenant, resource)
}

func (t TopicSet) TransferResponse(resource string) string {
	return fmt.Sprintf("dd/%s/transfer/%s/response", t.tenant, resource)
}

func (t TopicSet) EventPublish(resource string) string {
	return fmt.Sprintf("dd/%s/event/%s/publish", t.tenant, resource)
}

func (t TopicSet) StreamOpen(resource string) string {
	return fmt.Sprintf("dd/%s/stream/%s/open", t.tenant, resource)
}

func (t TopicSet) StreamClose(resource string) string {
	return fmt.Sprintf("dd/%s/stream/%s/close", t.tenant, resource)
}
