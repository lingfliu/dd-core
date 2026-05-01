package mq

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MqttConfig struct {
	BrokerUrl          string
	ClientId           string
	Username           string
	Password           string
	TLSEnabled         bool
	CaCertFile         string
	ClientCertFile     string
	ClientKeyFile      string
	InsecureSkipVerify bool
	TopicAliasEnabled  bool
}

type MqttClient struct {
	client            mqtt.Client
	topicAliasEnabled bool
}

func NewMqttClient(cfg MqttConfig) (*MqttClient, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.BrokerUrl).
		SetClientID(cfg.ClientId).
		SetUsername(cfg.Username).
		SetPassword(cfg.Password).
		SetConnectRetry(true).
		SetAutoReconnect(true).
		SetConnectRetryInterval(2 * time.Second)
	if cfg.TLSEnabled {
		tlsCfg, err := buildTLSConfig(cfg)
		if err != nil {
			return nil, err
		}
		opts.SetTLSConfig(tlsCfg)
	}

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(10 * time.Second) {
		return nil, fmt.Errorf("mqtt connect timeout")
	}
	if err := token.Error(); err != nil {
		return nil, err
	}

	return &MqttClient{
		client:            client,
		topicAliasEnabled: cfg.TopicAliasEnabled,
	}, nil
}

func buildTLSConfig(cfg MqttConfig) (*tls.Config, error) {
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}
	if cfg.CaCertFile != "" {
		ca, err := os.ReadFile(cfg.CaCertFile)
		if err != nil {
			return nil, fmt.Errorf("read ca cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("append ca cert failed")
		}
		tlsCfg.RootCAs = pool
	}
	if cfg.ClientCertFile != "" && cfg.ClientKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert/key: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return tlsCfg, nil
}

func (m *MqttClient) Publish(ctx context.Context, topic string, payload []byte) error {
	aliasTopic := m.aliasTopic(topic)
	token := m.client.Publish(aliasTopic, qosForTopic(aliasTopic), false, payload)
	if err := waitWithContext(ctx, token); err != nil {
		return err
	}
	return token.Error()
}

func qosForTopic(topic string) byte {
	// QoS layering strategy:
	// - Keep sync transfer request/response on QoS 1.
	// - Use QoS 0 for non-critical high-frequency control/event flows.
	if containsAny(topic,
		"/event/",
		"/peer/heartbeat",
		"/peer/resource/report",
		"/peer/hub/broadcast",
		"/peer/auth/broadcast",
	) {
		return 0
	}
	return 1
}

func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		if p != "" && strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func (m *MqttClient) Subscribe(ctx context.Context, topic string, handler MessageHandler) error {
	aliasTopic := m.aliasTopic(topic)
	token := m.client.Subscribe(aliasTopic, 1, func(_ mqtt.Client, msg mqtt.Message) {
		handler(msg.Topic(), msg.Payload())
	})
	if err := waitWithContext(ctx, token); err != nil {
		return err
	}
	return token.Error()
}

func (m *MqttClient) aliasTopic(topic string) string {
	if !m.topicAliasEnabled || topic == "" {
		return topic
	}
	// Only alias exact dd-core topics. Keep wildcard and third-party topics unchanged.
	if strings.Contains(topic, "#") || strings.Contains(topic, "+") || !strings.HasPrefix(topic, "dd/") {
		return topic
	}
	// Keep alias scope narrow to transfer topics to avoid surprising wildcard mapping behavior.
	if !strings.Contains(topic, "/transfer/") {
		return topic
	}
	parts := strings.Split(topic, "/")
	if len(parts) < 3 {
		return topic
	}
	tenant := parts[1]
	h := fnv.New64a()
	_, _ = h.Write([]byte(topic))
	sum := h.Sum(nil)
	return fmt.Sprintf("dd/%s/ta/%s", tenant, hex.EncodeToString(sum))
}

func (m *MqttClient) Close() error {
	m.client.Disconnect(1000)
	return nil
}

func waitWithContext(ctx context.Context, token mqtt.Token) error {
	done := make(chan struct{})
	go func() {
		token.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
