package model

import "fmt"

type DdErrorCode string

const (
	ErrInvalidEnvelope   DdErrorCode = "DD-4001"
	ErrUnauthorizedPeer  DdErrorCode = "DD-4002"
	ErrRouteNotFound     DdErrorCode = "DD-4041"
	ErrSyncTimeout       DdErrorCode = "DD-4081"
	ErrRetryExhausted    DdErrorCode = "DD-4091"
	ErrStreamNotFound    DdErrorCode = "DD-4092"
	ErrStreamIdleTimeout DdErrorCode = "DD-4093"
	ErrInternal          DdErrorCode = "DD-5001"
	ErrDuplicateRequest  DdErrorCode = "DD-4003"
	ErrAclDenied         DdErrorCode = "DD-4004"
	ErrPeerNotFound      DdErrorCode = "DD-4042"
	ErrPeerOffline       DdErrorCode = "DD-4043"
	ErrInvalidConfig     DdErrorCode = "DD-5002"
)

type DdError struct {
	Code    DdErrorCode `json:"code"`
	Message string      `json:"message"`
}

func (e *DdError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func NewDdError(code DdErrorCode, msg string) *DdError {
	return &DdError{Code: code, Message: msg}
}

func NewDdErrorf(code DdErrorCode, format string, args ...any) *DdError {
	return &DdError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func ErrInvalidEnvelopeMsg(msg string) *DdError {
	return NewDdError(ErrInvalidEnvelope, msg)
}

func ErrUnauthorizedPeerMsg(peerId string) *DdError {
	return NewDdErrorf(ErrUnauthorizedPeer, "peer %s unauthorized", peerId)
}

func ErrSyncTimeoutMsg(requestId string) *DdError {
	return NewDdErrorf(ErrSyncTimeout, "sync timeout: request_id=%s", requestId)
}

func ErrDuplicateRequestMsg(requestId string) *DdError {
	return NewDdErrorf(ErrDuplicateRequest, "duplicate request_id: %s", requestId)
}

func ErrAclDeniedMsg(peerId, action, topic string) *DdError {
	return NewDdErrorf(ErrAclDenied, "acl denied: peer=%s action=%s topic=%s", peerId, action, topic)
}

func ErrPeerNotFoundMsg(peerId string) *DdError {
	return NewDdErrorf(ErrPeerNotFound, "peer not found: %s", peerId)
}
