package adapter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"dd-core/internal/model"
	"dd-core/internal/mq"
)

type MqttBridge struct {
	mqClient mq.Client
	mappings []MqttMapping
}

type MqttMapping struct {
	Source string
	Target string
}

func NewMqttBridge(mqClient mq.Client, mappings []MqttMapping) *MqttBridge {
	return &MqttBridge{
		mqClient: mqClient,
		mappings: mappings,
	}
}

func (b *MqttBridge) Protocol() model.DdProtocol {
	return model.DdProtocolMq
}

func (b *MqttBridge) Start(ctx context.Context) error {
	for i, m := range b.mappings {
		source := m.Source
		target := m.Target
		idx := i
		sourcePrefix := stripWildcard(source)
		targetPrefix := stripWildcard(target)

		if err := b.mqClient.Subscribe(ctx, source, func(topic string, payload []byte) {
			destTopic := strings.Replace(topic, sourcePrefix, targetPrefix, 1)

			if err := b.mqClient.Publish(context.Background(), destTopic, payload); err != nil {
				slog.Warn("mqtt bridge forward failed",
					"source", source,
					"target", target,
					"dest_topic", destTopic,
					"error", err,
				)
				return
			}
			slog.Info("mqtt bridge forwarded",
				"source_topic", topic,
				"dest_topic", destTopic,
				"size", len(payload),
			)
		}); err != nil {
			return fmt.Errorf("subscribe mapping[%d] %s: %w", idx, source, err)
		}

		slog.Info("mqtt bridge mapping subscribed",
			"index", idx,
			"source", source,
			"target", target,
		)
	}
	return nil
}

func stripWildcard(pattern string) string {
	pattern = strings.TrimSuffix(pattern, "#")
	pattern = strings.TrimSuffix(pattern, "+")
	return pattern
}
