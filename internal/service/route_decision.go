package service

import (
	"dd-core/internal/config"
	"dd-core/internal/model"
)

type RouteMode string

const (
	RouteModeBroker RouteMode = "broker"
	RouteModeDirect RouteMode = "direct"
)

func DecideRoute(cfg *config.Config, msg *model.DdMessage) RouteMode {
	if msg == nil {
		return RouteModeBroker
	}
	if msg.Header.TargetPeerId == "" || cfg == nil {
		return RouteModeBroker
	}
	if msg.Header.TargetPeerId != cfg.Peer.Id {
		return RouteModeBroker
	}
	switch msg.Protocol {
	case model.DdProtocolHttp:
		if cfg.Bridges.Http.Enabled && cfg.Bridges.Http.TargetUrl != "" {
			return RouteModeDirect
		}
	case model.DdProtocolCoap:
		if cfg.Bridges.Coap.Enabled && cfg.Bridges.Coap.TargetUrl != "" {
			return RouteModeDirect
		}
	}
	return RouteModeBroker
}
