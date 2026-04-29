package api

import (
	"encoding/json"
	"net/http"
	"time"

	"dd-core/internal/config"
	"dd-core/internal/model"
	"dd-core/internal/service"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	cfg             *config.Config
	mux             *http.ServeMux
	topics          service.TopicSet
	peerRegistry    *service.PeerRegistryService
	dataService     *service.DdDataService
	aclService      *service.TopicAclService
	streamService   *service.StreamService
	resourceCatalog *service.ResourceCatalog
	startTime       time.Time
}

func NewServer(
	cfg *config.Config,
	topics service.TopicSet,
	peerRegistry *service.PeerRegistryService,
	dataService *service.DdDataService,
	aclService *service.TopicAclService,
	streamService *service.StreamService,
	resourceCatalog *service.ResourceCatalog,
) *Server {
	s := &Server{
		cfg:             cfg,
		mux:             http.NewServeMux(),
		topics:          topics,
		peerRegistry:    peerRegistry,
		dataService:     dataService,
		aclService:      aclService,
		streamService:   streamService,
		resourceCatalog: resourceCatalog,
		startTime:       time.Now(),
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	prefix := "/dd/api/v1"

	s.mux.HandleFunc("GET "+prefix+"/health", s.handleHealth)

	s.mux.HandleFunc("POST "+prefix+"/peers/register", s.handlePeerRegister)
	s.mux.HandleFunc("POST "+prefix+"/peers/{id}/heartbeat", s.handlePeerHeartbeat)
	s.mux.HandleFunc("POST "+prefix+"/peers/{id}/unregister", s.handlePeerUnregister)
	s.mux.HandleFunc("GET "+prefix+"/peers/{id}", s.handlePeerGet)
	s.mux.HandleFunc("GET "+prefix+"/peers", s.handlePeersQuery)
	s.mux.HandleFunc("GET "+prefix+"/resources/{resource}/providers", s.handleResourceProviders)
	s.mux.HandleFunc("GET "+prefix+"/resources", s.handleResourcesList)

	s.mux.HandleFunc("POST "+prefix+"/transfer/sync", s.handleTransferSync)
	s.mux.HandleFunc("POST "+prefix+"/transfer/async", s.handleTransferAsync)
	s.mux.HandleFunc("GET "+prefix+"/transfer/{requestId}", s.handleTransferStatus)
	s.mux.HandleFunc("POST "+prefix+"/transfer/stream/open", s.handleStreamOpen)
	s.mux.HandleFunc("POST "+prefix+"/transfer/stream/close", s.handleStreamClose)
	s.mux.HandleFunc("GET "+prefix+"/streams", s.handleStreamsList)

	s.mux.HandleFunc("GET "+prefix+"/metrics", s.handleMetrics)
	s.mux.Handle("GET /metrics", promhttp.Handler())
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, err *model.DdError) {
	s.writeJSON(w, errorToHTTPStatus(err.Code), err)
}

func errorToHTTPStatus(code model.DdErrorCode) int {
	switch code {
	case model.ErrInvalidEnvelope, model.ErrDuplicateRequest:
		return http.StatusBadRequest
	case model.ErrUnauthorizedPeer, model.ErrAclDenied:
		return http.StatusForbidden
	case model.ErrRouteNotFound, model.ErrPeerNotFound, model.ErrStreamNotFound:
		return http.StatusNotFound
	case model.ErrSyncTimeout:
		return http.StatusGatewayTimeout
	case model.ErrInternal, model.ErrInvalidConfig:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
