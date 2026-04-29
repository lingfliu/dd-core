package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadYaml(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  listen_addr: ":9090"
tenant: "test-tenant"
sync:
  default_timeout_ms: 5000
discovery:
  lease_ttl_sec: 60
mq:
  mqtt:
    broker_url: "tcp://10.0.0.1:1883"
    client_id: "test-client"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Server.ListenAddr != ":9090" {
		t.Fatalf("expected :9090, got %s", cfg.Server.ListenAddr)
	}
	if cfg.Tenant != "test-tenant" {
		t.Fatalf("expected test-tenant, got %s", cfg.Tenant)
	}
	if cfg.Sync.DefaultTimeoutMs != 5000 {
		t.Fatalf("expected 5000, got %d", cfg.Sync.DefaultTimeoutMs)
	}
	if cfg.Discovery.LeaseTtlSec != 60 {
		t.Fatalf("expected 60, got %d", cfg.Discovery.LeaseTtlSec)
	}
	if cfg.Mq.Mqtt.BrokerUrl != "tcp://10.0.0.1:1883" {
		t.Fatalf("unexpected broker url: %s", cfg.Mq.Mqtt.BrokerUrl)
	}
}

func TestLoadJson(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	content := `{
  "server": {"listen_addr": ":9090"},
  "tenant": "json-tenant",
  "peer": {"id": "p1", "name": "peer-one", "role": "hub"},
  "sync": {"default_timeout_ms": 5000}
}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadPath(path)
	if err != nil {
		t.Fatalf("LoadPath failed: %v", err)
	}
	if cfg.Server.ListenAddr != ":9090" {
		t.Fatalf("expected :9090, got %s", cfg.Server.ListenAddr)
	}
	if cfg.Tenant != "json-tenant" {
		t.Fatalf("expected json-tenant, got %s", cfg.Tenant)
	}
	if cfg.Peer.Id != "p1" {
		t.Fatalf("expected peer id p1, got %s", cfg.Peer.Id)
	}
	if cfg.Peer.Role != "hub" {
		t.Fatalf("expected role hub, got %s", cfg.Peer.Role)
	}
}

func TestLoadPathDefaultExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := "tenant: no-ext"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadPath(path)
	if err != nil {
		t.Fatalf("LoadPath failed: %v", err)
	}
	if cfg.Tenant != "no-ext" {
		t.Fatalf("expected no-ext, got %s", cfg.Tenant)
	}
}

func TestDefaults(t *testing.T) {
	cfg := LoadOrDefault("/nonexistent/config.yaml")
	if cfg.Server.ListenAddr != ":8080" {
		t.Fatalf("expected :8080, got %s", cfg.Server.ListenAddr)
	}
	if cfg.Tenant != "default" {
		t.Fatalf("expected default, got %s", cfg.Tenant)
	}
	if cfg.Sync.DefaultTimeoutMs != 3000 {
		t.Fatalf("expected 3000, got %d", cfg.Sync.DefaultTimeoutMs)
	}
	if cfg.Peer.Role != "edge" {
		t.Fatalf("expected edge, got %s", cfg.Peer.Role)
	}
}

func TestEnvOverrides(t *testing.T) {
	os.Setenv("DD_SERVER_LISTEN_ADDR", ":7070")
	os.Setenv("DD_TENANT", "env-tenant")
	defer func() {
		os.Unsetenv("DD_SERVER_LISTEN_ADDR")
		os.Unsetenv("DD_TENANT")
	}()

	cfg := LoadOrDefault("/nonexistent/config.yaml")
	if cfg.Server.ListenAddr != ":7070" {
		t.Fatalf("expected :7070, got %s", cfg.Server.ListenAddr)
	}
	if cfg.Tenant != "env-tenant" {
		t.Fatalf("expected env-tenant, got %s", cfg.Tenant)
	}
}

func TestEnvName(t *testing.T) {
	cfg := defaultConfig()
	cfg.Tenant = "Default"
	if cfg.EnvName() != "default" {
		t.Fatalf("expected default, got %s", cfg.EnvName())
	}
}

func TestMergeJson(t *testing.T) {
	cfg := defaultConfig()
	err := MergeJson(cfg, `{"tenant":"merged","peer":{"id":"p99","role":"hub"}}`)
	if err != nil {
		t.Fatalf("MergeJson failed: %v", err)
	}
	if cfg.Tenant != "merged" {
		t.Fatalf("expected merged, got %s", cfg.Tenant)
	}
	if cfg.Peer.Id != "p99" {
		t.Fatalf("expected p99, got %s", cfg.Peer.Id)
	}
	if cfg.Peer.Role != "hub" {
		t.Fatalf("expected hub, got %s", cfg.Peer.Role)
	}
	if cfg.Server.ListenAddr != ":8080" {
		t.Fatalf("expected :8080 default, got %s", cfg.Server.ListenAddr)
	}
}

func TestCliArgsParsing(t *testing.T) {
	args := []string{
		"--config=config.yaml",
		"--role=hub",
		"--peer-id=hub-01",
		"--peer-name=My Hub",
		"--tenant=cli-tenant",
		"--listen-addr=:9090",
		"--mqtt-broker=tcp://10.0.0.1:1883",
		"--log-level=debug",
		"--log-format=text",
		"--config-json={\"sync\":{\"default_timeout_ms\":5000}}",
	}

	cli := ParseCliArgs(args)
	if cli.Role != "hub" {
		t.Fatalf("expected hub, got %s", cli.Role)
	}
	if cli.PeerId != "hub-01" {
		t.Fatalf("expected hub-01, got %s", cli.PeerId)
	}
	if cli.Tenant != "cli-tenant" {
		t.Fatalf("expected cli-tenant, got %s", cli.Tenant)
	}
	if cli.ListenAddr != ":9090" {
		t.Fatalf("expected :9090, got %s", cli.ListenAddr)
	}
	if cli.MqttBroker != "tcp://10.0.0.1:1883" {
		t.Fatalf("unexpected broker: %s", cli.MqttBroker)
	}
	if cli.LogLevel != "debug" {
		t.Fatalf("expected debug, got %s", cli.LogLevel)
	}
	if cli.LogFormat != "text" {
		t.Fatalf("expected text, got %s", cli.LogFormat)
	}
}

func TestCliPriority(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  listen_addr: ":8080"
tenant: "file-tenant"
peer:
  id: "file-id"
  role: "edge"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("DD_TENANT", "env-tenant")
	os.Setenv("DD_PEER_ID", "env-id")
	defer func() {
		os.Unsetenv("DD_TENANT")
		os.Unsetenv("DD_PEER_ID")
	}()

	cli := CliConfig{
		ConfigPath: path,
		Tenant:     "cli-tenant",
		Role:       "hub",
	}
	cfg, err := LoadWithArgs(cli)
	if err != nil {
		t.Fatalf("LoadWithArgs failed: %v", err)
	}

	if cfg.Tenant != "cli-tenant" {
		t.Fatalf("CLI should override ENV and file: expected cli-tenant, got %s", cfg.Tenant)
	}
	if cfg.Peer.Role != "hub" {
		t.Fatalf("CLI should override file: expected hub, got %s", cfg.Peer.Role)
	}
	if cfg.Peer.Id != "env-id" {
		t.Fatalf("ENV should override file for peer id: expected env-id, got %s", cfg.Peer.Id)
	}
	if cfg.Server.ListenAddr != ":8080" {
		t.Fatalf("file value should be used: expected :8080, got %s", cfg.Server.ListenAddr)
	}
}

func TestCliConfigJsonMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  listen_addr: ":8080"
tenant: "file-tenant"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cli := CliConfig{
		ConfigPath: path,
		ConfigJson: `{"tenant":"json-tenant","peer":{"id":"json-id"}}`,
	}
	cfg, err := LoadWithArgs(cli)
	if err != nil {
		t.Fatalf("LoadWithArgs failed: %v", err)
	}
	if cfg.Tenant != "json-tenant" {
		t.Fatalf("config-json should override file: expected json-tenant, got %s", cfg.Tenant)
	}
	if cfg.Peer.Id != "json-id" {
		t.Fatalf("config-json should set peer id: expected json-id, got %s", cfg.Peer.Id)
	}
}

func TestCliConfigJsonAndFlag(t *testing.T) {
	cli := CliConfig{
		ConfigJson: `{"tenant":"json-tenant"}`,
		Tenant:     "flag-tenant",
	}
	cfg, err := LoadWithArgs(cli)
	if err != nil {
		t.Fatalf("LoadWithArgs failed: %v", err)
	}
	if cfg.Tenant != "flag-tenant" {
		t.Fatalf("CLI flag should override config-json: expected flag-tenant, got %s", cfg.Tenant)
	}
}
