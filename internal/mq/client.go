package mq

import "context"

type Action string

const (
	ActionPublish   Action = "pub"
	ActionSubscribe Action = "sub"
)

type MessageHandler func(topic string, payload []byte)

type Client interface {
	Publish(ctx context.Context, topic string, payload []byte) error
	Subscribe(ctx context.Context, topic string, handler MessageHandler) error
	Close() error
}
