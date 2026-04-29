package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dd-core/api"
	"dd-core/internal/adapter"
	"dd-core/internal/config"
	"dd-core/internal/model"
	"dd-core/internal/mq"
	"dd-core/internal/observability"
	"dd-core/internal/service"
)

func main() {
	cli := config.ParseCliArgs(os.Args[1:])

	cfg, err := config.LoadWithArgs(cli)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	setupLogging(cfg)

	slog.Info("starting dd-core",
		"tenant", cfg.Tenant,
		"peer_id", cfg.Peer.Id,
		"role", cfg.Peer.Role,
		"listen_addr", cfg.Server.ListenAddr,
	)

	mqClient, err := mq.NewMqttClient(mq.MqttConfig{
		BrokerUrl: cfg.Mq.Mqtt.BrokerUrl,
		ClientId:  cfg.Mq.Mqtt.ClientId,
		Username:  cfg.Mq.Mqtt.Username,
		Password:  cfg.Mq.Mqtt.Password,
	})
	if err != nil {
		slog.Error("failed to connect to MQTT broker", "error", err)
		os.Exit(1)
	}
	defer mqClient.Close()

	aclService := buildAclService(cfg)
	topics := service.NewDefaultTopicSet(cfg.Tenant)

	peerRegistry := service.NewPeerRegistryService(
		mqClient,
		topics,
		time.Duration(cfg.Discovery.LeaseTtlSec)*time.Second,
		service.WithAclService(aclService),
	)

	dataService := service.NewDdDataService(
		mqClient,
		time.Duration(cfg.Sync.DefaultTimeoutMs)*time.Millisecond,
		service.WithDataAclService(aclService),
	)

	ctx := context.Background()
	if err := peerRegistry.Start(ctx); err != nil {
		slog.Error("failed to start peer registry", "error", err)
		os.Exit(1)
	}

	streamService := service.NewStreamService(mqClient, topics)
	if err := streamService.Start(ctx); err != nil {
		slog.Error("failed to start stream service", "error", err)
		os.Exit(1)
	}

	broadcastInterval := time.Duration(cfg.Peer.Broadcast.IntervalSec) * time.Second
	if broadcastInterval <= 0 {
		broadcastInterval = 10 * time.Second
	}
	broadcastTtl := cfg.Peer.Broadcast.TtlSec
	if broadcastTtl <= 0 {
		broadcastTtl = 30
	}

	hubDiscovery := service.NewHubDiscoveryService(
		mqClient, topics,
		cfg.Peer.Id, cfg.Peer.Name, cfg.Peer.Role,
		broadcastInterval, broadcastTtl,
	)
	if err := hubDiscovery.Start(ctx); err != nil {
		slog.Error("failed to start hub discovery", "error", err)
		os.Exit(1)
	}

	authPeerService := service.NewAuthPeerService(mqClient, topics, nil, cfg.Peer.Auth.Secret)
	if cfg.Peer.Key != "" && cfg.Peer.Secret != "" {
		authPeerService.SetSecret(cfg.Peer.Key, cfg.Peer.Secret)
	}
	if err := authPeerService.Start(ctx); err != nil {
		slog.Error("failed to start auth peer service", "error", err)
		os.Exit(1)
	}
	if cfg.Peer.Role == "auth" {
		hubDiscovery.BroadcastAuth(cfg.Peer.Auth.VerifyTopicSuffix, "")
	}

	peerListService := service.NewPeerListService(
		mqClient, topics, peerRegistry, aclService,
		cfg.Discovery.MaxPageSize,
	)
	if err := peerListService.Start(ctx); err != nil {
		slog.Error("failed to start peer list service", "error", err)
		os.Exit(1)
	}

	resourceCatalog := service.NewResourceCatalog()

	if cfg.Bridges.Http.Enabled && cfg.Bridges.Http.TargetUrl != "" {
		httpBridge := adapter.NewHttpBridge(mqClient, topics, cfg.Peer.Id, cfg.Bridges.Http.TargetUrl)
		if err := httpBridge.Start(ctx); err != nil {
			slog.Error("failed to start http bridge", "error", err)
			os.Exit(1)
		}
		slog.Info("http bridge started", "peer_id", cfg.Peer.Id, "target_url", cfg.Bridges.Http.TargetUrl)
	}

	if cfg.Bridges.Coap.Enabled && cfg.Bridges.Coap.TargetUrl != "" {
		coapBridge := adapter.NewCoapBridge(mqClient, topics, cfg.Peer.Id, cfg.Bridges.Coap.TargetUrl)
		if err := coapBridge.Start(ctx); err != nil {
			slog.Error("failed to start coap bridge", "error", err)
			os.Exit(1)
		}
		slog.Info("coap bridge started", "peer_id", cfg.Peer.Id, "target_url", cfg.Bridges.Coap.TargetUrl)
	}

	if len(cfg.Bridges.MqttMappings) > 0 {
		mappings := make([]adapter.MqttMapping, len(cfg.Bridges.MqttMappings))
		for i, m := range cfg.Bridges.MqttMappings {
			mappings[i] = adapter.MqttMapping{Source: m.Source, Target: m.Target}
		}
		mqttBridge := adapter.NewMqttBridge(mqClient, mappings)
		if err := mqttBridge.Start(ctx); err != nil {
			slog.Error("failed to start mqtt bridge", "error", err)
			os.Exit(1)
		}
		slog.Info("mqtt bridge started", "mappings", len(mappings))
	}

	httpServer := api.NewServer(cfg, topics, peerRegistry, dataService, aclService, streamService, resourceCatalog)

	handler := observability.RequestLoggingMiddleware(httpServer.Handler())

	server := &http.Server{
		Addr:    cfg.Server.ListenAddr,
		Handler: handler,
	}

	go func() {
		slog.Info("HTTP API server starting", "addr", cfg.Server.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}()

	if cfg.Peer.Id != "" {
		go func() {
			peer := buildPeerInfo(cfg)
			if err := peerRegistry.RegisterPeer(ctx, peer); err != nil {
				slog.Error("auto-register peer failed", "error", err)
				return
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("shutting down", "signal", sig.String())

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown error", "error", err)
	}

	slog.Info("dd-core stopped")
}

func setupLogging(cfg *config.Config) {
	var level slog.Level
	switch cfg.Logging.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	if cfg.Logging.Format == "json" {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, opts)))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, opts)))
	}
}

func buildAclService(cfg *config.Config) *service.TopicAclService {
	acl := service.NewTopicAclService()
	rules := make([]service.TopicAclRule, 0, len(cfg.Acl.Rules))
	for _, r := range cfg.Acl.Rules {
		rules = append(rules, service.TopicAclRule{
			PeerId:  r.PeerId,
			Action:  service.AclAction(r.Action),
			Pattern: r.Pattern,
			Allow:   r.Allow,
		})
	}
	if len(rules) > 0 {
		acl.SetRules(rules)
	}
	return acl
}

func buildPeerInfo(cfg *config.Config) model.DdPeerInfo {
	return model.DdPeerInfo{
		Id:   cfg.Peer.Id,
		Name: cfg.Peer.Name,
		Role: cfg.Peer.Role,
	}
}
