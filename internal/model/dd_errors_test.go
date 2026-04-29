package model

import "testing"

func TestDdErrorFormat(t *testing.T) {
	err := ErrSyncTimeoutMsg("req-1")
	if err.Code != ErrSyncTimeout {
		t.Fatalf("expected code %s, got %s", ErrSyncTimeout, err.Code)
	}
	if err.Error() != "[DD-4081] sync timeout: request_id=req-1" {
		t.Fatalf("unexpected error string: %s", err.Error())
	}
}

func TestDdErrorHelperFuncs(t *testing.T) {
	cases := []struct {
		err  *DdError
		code DdErrorCode
	}{
		{ErrInvalidEnvelopeMsg("bad data"), ErrInvalidEnvelope},
		{ErrUnauthorizedPeerMsg("peer-x"), ErrUnauthorizedPeer},
		{ErrDuplicateRequestMsg("dup-1"), ErrDuplicateRequest},
		{ErrAclDeniedMsg("peer-x", "pub", "dd/t"), ErrAclDenied},
		{ErrPeerNotFoundMsg("missing"), ErrPeerNotFound},
	}

	for _, c := range cases {
		if c.err.Code != c.code {
			t.Errorf("expected code %s, got %s", c.code, c.err.Code)
		}
		if c.err.Message == "" {
			t.Errorf("expected non-empty message for code %s", c.code)
		}
	}
}
