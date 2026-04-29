package config

import (
	"flag"
	"fmt"
)

type CliConfig struct {
	ConfigPath string
	ConfigJson string
	Role       string
	PeerId     string
	PeerName   string
	Tenant     string
	ListenAddr string
	MqttBroker string
	LogLevel   string
	LogFormat  string
}

func ParseCliArgs(args []string) CliConfig {
	fs := flag.NewFlagSet("dd-core", flag.ContinueOnError)
	c := CliConfig{}

	fs.StringVar(&c.ConfigPath, "config", "config.yaml", "path to config file (.yaml/.yml/.json)")
	fs.StringVar(&c.ConfigJson, "config-json", "", "inline JSON config (merged on top of file config)")
	fs.StringVar(&c.Role, "role", "", "peer role: edge|term|hub|edge_hub")
	fs.StringVar(&c.PeerId, "peer-id", "", "unique peer identifier")
	fs.StringVar(&c.PeerName, "peer-name", "", "peer display name")
	fs.StringVar(&c.Tenant, "tenant", "", "tenant name")
	fs.StringVar(&c.ListenAddr, "listen-addr", "", "HTTP API listen address")
	fs.StringVar(&c.MqttBroker, "mqtt-broker", "", "MQTT broker URL (e.g. tcp://localhost:1883)")
	fs.StringVar(&c.LogLevel, "log-level", "", "log level: debug|info|warn|error")
	fs.StringVar(&c.LogFormat, "log-format", "", "log format: json|text")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(fs.Output(), "flag parse error: %v\n", err)
	}
	return c
}

func (c CliConfig) applyCliOverrides(cfg *Config) {
	if c.Role != "" {
		cfg.Peer.Role = c.Role
	}
	if c.PeerId != "" {
		cfg.Peer.Id = c.PeerId
	}
	if c.PeerName != "" {
		cfg.Peer.Name = c.PeerName
	}
	if c.Tenant != "" {
		cfg.Tenant = c.Tenant
	}
	if c.ListenAddr != "" {
		cfg.Server.ListenAddr = c.ListenAddr
	}
	if c.MqttBroker != "" {
		cfg.Mq.Mqtt.BrokerUrl = c.MqttBroker
	}
	if c.LogLevel != "" {
		cfg.Logging.Level = c.LogLevel
	}
	if c.LogFormat != "" {
		cfg.Logging.Format = c.LogFormat
	}
}

func LoadWithArgs(cli CliConfig) (*Config, error) {
	cfg, err := LoadPath(cli.ConfigPath)
	if err != nil {
		cfg = defaultConfig()
	}

	applyEnvOverrides(cfg)

	if cli.ConfigJson != "" {
		if mergeErr := MergeJson(cfg, cli.ConfigJson); mergeErr != nil {
			return nil, mergeErr
		}
	}

	cli.applyCliOverrides(cfg)

	return cfg, nil
}
