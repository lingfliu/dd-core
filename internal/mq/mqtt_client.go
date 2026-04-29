package mq

import (
	"context"
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MqttConfig struct {
	BrokerUrl string
	ClientId  string
	Username  string
	Password  string
}

type MqttClient struct {
	client mqtt.Client
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

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(10 * time.Second) {
		return nil, fmt.Errorf("mqtt connect timeout")
	}
	if err := token.Error(); err != nil {
		return nil, err
	}

	return &MqttClient{client: client}, nil
}

func (m *MqttClient) Publish(ctx context.Context, topic string, payload []byte) error {
	token := m.client.Publish(topic, 1, false, payload)
	if err := waitWithContext(ctx, token); err != nil {
		return err
	}
	return token.Error()
}

func (m *MqttClient) Subscribe(ctx context.Context, topic string, handler MessageHandler) error {
	token := m.client.Subscribe(topic, 1, func(_ mqtt.Client, msg mqtt.Message) {
		handler(msg.Topic(), msg.Payload())
	})
	if err := waitWithContext(ctx, token); err != nil {
		return err
	}
	return token.Error()
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
