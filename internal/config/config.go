package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server" json:"server"`
	Peer      PeerConfig      `yaml:"peer" json:"peer"`
	Mq        MqConfig        `yaml:"mq" json:"mq"`
	Tenant    string          `yaml:"tenant" json:"tenant"`
	Sync      SyncConfig      `yaml:"sync" json:"sync"`
	Discovery DiscoveryConfig `yaml:"discovery" json:"discovery"`
	Bridges   BridgeConfig    `yaml:"bridges" json:"bridges"`
	Acl       AclConfig       `yaml:"acl" json:"acl"`
	Logging   LoggingConfig   `yaml:"logging" json:"logging"`
}

type PeerConfig struct {
	Id     string `yaml:"id" json:"id"`
	Name   string `yaml:"name" json:"name"`
	Role   string `yaml:"role" json:"role"`
	Key    string `yaml:"key" json:"key"`
	Secret string `yaml:"secret" json:"secret"`

	Broadcast BroadcastConfig `yaml:"broadcast" json:"broadcast"`
	Auth      AuthPeerConfig  `yaml:"auth" json:"auth"`
}

type BroadcastConfig struct {
	IntervalSec int `yaml:"interval_sec" json:"interval_sec"`
	TtlSec      int `yaml:"ttl_sec" json:"ttl_sec"`
}

type AuthPeerConfig struct {
	Key               string `yaml:"key" json:"key"`
	Secret            string `yaml:"secret" json:"secret"`
	VerifyTopicSuffix string `yaml:"verify_topic_suffix" json:"verify_topic_suffix"`
}

type ServerConfig struct {
	ListenAddr string `yaml:"listen_addr" json:"listen_addr"`
}

type MqConfig struct {
	Provider string     `yaml:"provider" json:"provider"`
	Mqtt     MqttConfig `yaml:"mqtt" json:"mqtt"`
}

type MqttConfig struct {
	BrokerUrl string `yaml:"broker_url" json:"broker_url"`
	ClientId  string `yaml:"client_id" json:"client_id"`
	Username  string `yaml:"username" json:"username"`
	Password  string `yaml:"password" json:"password"`
}

type BridgeConfig struct {
	Http         HttpBridgeCfg    `yaml:"http" json:"http"`
	Coap         CoapBridgeCfg    `yaml:"coap" json:"coap"`
	MqttMappings []MqttMappingCfg `yaml:"mqtt_mappings" json:"mqtt_mappings"`
}

type HttpBridgeCfg struct {
	Enabled   bool   `yaml:"enabled" json:"enabled"`
	TargetUrl string `yaml:"target_url" json:"target_url"`
}

type CoapBridgeCfg struct {
	Enabled   bool   `yaml:"enabled" json:"enabled"`
	TargetUrl string `yaml:"target_url" json:"target_url"`
}

type MqttMappingCfg struct {
	Source string `yaml:"source" json:"source"`
	Target string `yaml:"target" json:"target"`
}

type SyncConfig struct {
	DefaultTimeoutMs int64 `yaml:"default_timeout_ms" json:"default_timeout_ms"`
}

type DiscoveryConfig struct {
	HeartbeatIntervalSec int `yaml:"heartbeat_interval_sec" json:"heartbeat_interval_sec"`
	LeaseTtlSec          int `yaml:"lease_ttl_sec" json:"lease_ttl_sec"`
	DefaultPageSize      int `yaml:"default_page_size" json:"default_page_size"`
	MaxPageSize          int `yaml:"max_page_size" json:"max_page_size"`
}

type AclConfig struct {
	Rules []AclRule `yaml:"rules" json:"rules"`
}

type AclRule struct {
	PeerId  string `yaml:"peer_id" json:"peer_id"`
	Action  string `yaml:"action" json:"action"`
	Pattern string `yaml:"pattern" json:"pattern"`
	Allow   bool   `yaml:"allow" json:"allow"`
}

type LoggingConfig struct {
	Level  string `yaml:"level" json:"level"`
	Format string `yaml:"format" json:"format"`
}

func Load(path string) (*Config, error) {
	cfg, err := LoadPath(path)
	if err != nil {
		return nil, err
	}
	applyEnvOverrides(cfg)
	return cfg, nil
}

func LoadPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(path))
	cfg := defaultConfig()

	switch ext {
	case ".json":
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("json parse: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("yaml parse: %w", err)
		}
	default:
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("yaml parse: %w", err)
		}
	}

	return cfg, nil
}

func LoadOrDefault(path string) *Config {
	cfg, err := Load(path)
	if err != nil {
		cfg = defaultConfig()
		applyEnvOverrides(cfg)
		return cfg
	}
	return cfg
}

func LoadOrDefaultPath(path string) *Config {
	cfg, err := LoadPath(path)
	if err != nil {
		cfg = defaultConfig()
		return cfg
	}
	applyEnvOverrides(cfg)
	return cfg
}

func MergeJson(cfg *Config, jsonStr string) error {
	if jsonStr == "" {
		return nil
	}
	override := &Config{}
	if err := json.Unmarshal([]byte(jsonStr), override); err != nil {
		return fmt.Errorf("merge json: %w", err)
	}
	mergeInto(cfg, override)
	return nil
}

func mergeInto(base, override *Config) {
	if override.Server.ListenAddr != "" {
		base.Server.ListenAddr = override.Server.ListenAddr
	}
	if override.Peer.Id != "" {
		base.Peer.Id = override.Peer.Id
	}
	if override.Peer.Name != "" {
		base.Peer.Name = override.Peer.Name
	}
	if override.Peer.Role != "" {
		base.Peer.Role = override.Peer.Role
	}
	if override.Peer.Key != "" {
		base.Peer.Key = override.Peer.Key
	}
	if override.Peer.Secret != "" {
		base.Peer.Secret = override.Peer.Secret
	}
	if override.Tenant != "" {
		base.Tenant = override.Tenant
	}
	if override.Mq.Provider != "" {
		base.Mq.Provider = override.Mq.Provider
	}
	if override.Mq.Mqtt.BrokerUrl != "" {
		base.Mq.Mqtt.BrokerUrl = override.Mq.Mqtt.BrokerUrl
	}
	if override.Mq.Mqtt.ClientId != "" {
		base.Mq.Mqtt.ClientId = override.Mq.Mqtt.ClientId
	}
	if override.Mq.Mqtt.Username != "" {
		base.Mq.Mqtt.Username = override.Mq.Mqtt.Username
	}
	if override.Mq.Mqtt.Password != "" {
		base.Mq.Mqtt.Password = override.Mq.Mqtt.Password
	}
	if override.Sync.DefaultTimeoutMs > 0 {
		base.Sync.DefaultTimeoutMs = override.Sync.DefaultTimeoutMs
	}
	if override.Discovery.HeartbeatIntervalSec > 0 {
		base.Discovery.HeartbeatIntervalSec = override.Discovery.HeartbeatIntervalSec
	}
	if override.Discovery.LeaseTtlSec > 0 {
		base.Discovery.LeaseTtlSec = override.Discovery.LeaseTtlSec
	}
	if override.Discovery.DefaultPageSize > 0 {
		base.Discovery.DefaultPageSize = override.Discovery.DefaultPageSize
	}
	if override.Discovery.MaxPageSize > 0 {
		base.Discovery.MaxPageSize = override.Discovery.MaxPageSize
	}
	if override.Peer.Broadcast.IntervalSec > 0 {
		base.Peer.Broadcast.IntervalSec = override.Peer.Broadcast.IntervalSec
	}
	if override.Peer.Broadcast.TtlSec > 0 {
		base.Peer.Broadcast.TtlSec = override.Peer.Broadcast.TtlSec
	}
	if override.Logging.Level != "" {
		base.Logging.Level = override.Logging.Level
	}
	if override.Logging.Format != "" {
		base.Logging.Format = override.Logging.Format
	}
	if override.Bridges.Http.TargetUrl != "" {
		base.Bridges.Http.TargetUrl = override.Bridges.Http.TargetUrl
	}
	base.Bridges.Http.Enabled = base.Bridges.Http.Enabled || override.Bridges.Http.Enabled
	if override.Bridges.Coap.TargetUrl != "" {
		base.Bridges.Coap.TargetUrl = override.Bridges.Coap.TargetUrl
	}
	base.Bridges.Coap.Enabled = base.Bridges.Coap.Enabled || override.Bridges.Coap.Enabled
	if len(override.Bridges.MqttMappings) > 0 {
		base.Bridges.MqttMappings = override.Bridges.MqttMappings
	}
	if len(override.Acl.Rules) > 0 {
		base.Acl.Rules = override.Acl.Rules
	}
}

func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr: ":8080",
		},
		Peer: PeerConfig{
			Role: "edge",
			Broadcast: BroadcastConfig{
				IntervalSec: 10,
				TtlSec:      30,
			},
		},
		Mq: MqConfig{
			Provider: "mqtt",
			Mqtt: MqttConfig{
				BrokerUrl: "tcp://localhost:1883",
				ClientId:  "dd-core-hub",
			},
		},
		Tenant: "default",
		Sync: SyncConfig{
			DefaultTimeoutMs: 3000,
		},
		Discovery: DiscoveryConfig{
			HeartbeatIntervalSec: 10,
			LeaseTtlSec:          30,
			DefaultPageSize:      20,
			MaxPageSize:          100,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("DD_SERVER_LISTEN_ADDR"); v != "" {
		cfg.Server.ListenAddr = v
	}
	if v := os.Getenv("DD_PEER_ID"); v != "" {
		cfg.Peer.Id = v
	}
	if v := os.Getenv("DD_PEER_NAME"); v != "" {
		cfg.Peer.Name = v
	}
	if v := os.Getenv("DD_PEER_ROLE"); v != "" {
		cfg.Peer.Role = v
	}
	if v := os.Getenv("DD_MQ_PROVIDER"); v != "" {
		cfg.Mq.Provider = v
	}
	if v := os.Getenv("DD_MQTT_BROKER_URL"); v != "" {
		cfg.Mq.Mqtt.BrokerUrl = v
	}
	if v := os.Getenv("DD_MQTT_CLIENT_ID"); v != "" {
		cfg.Mq.Mqtt.ClientId = v
	}
	if v := os.Getenv("DD_MQTT_USERNAME"); v != "" {
		cfg.Mq.Mqtt.Username = v
	}
	if v := os.Getenv("DD_MQTT_PASSWORD"); v != "" {
		cfg.Mq.Mqtt.Password = v
	}
	if v := os.Getenv("DD_TENANT"); v != "" {
		cfg.Tenant = v
	}
	if v := os.Getenv("DD_SYNC_DEFAULT_TIMEOUT_MS"); v != "" {
		if ms := parseInt64(v); ms > 0 {
			cfg.Sync.DefaultTimeoutMs = ms
		}
	}
	if v := os.Getenv("DD_DISCOVERY_LEASE_TTL_SEC"); v != "" {
		if ttl := parseInt(v); ttl > 0 {
			cfg.Discovery.LeaseTtlSec = ttl
		}
	}
	if v := os.Getenv("DD_LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("DD_LOG_FORMAT"); v != "" {
		cfg.Logging.Format = v
	}
}

func parseInt64(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

func parseInt(s string) int {
	return int(parseInt64(s))
}

func (c *Config) EnvName() string {
	s := c.Tenant
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	return s
}
