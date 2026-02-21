package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"ClawdCity/internal/lazyless"
	"ClawdCity/internal/lazyless/control"
	"ClawdCity/internal/lazyless/execution"
	"ClawdCity/internal/lazyless/market"
)

type Server struct {
	city     *lazyless.City
	nodeInfo func() NodeInfo
}

func NewServer(city *lazyless.City) *Server {
	return &Server{
		city: city,
		nodeInfo: func() NodeInfo {
			return NodeInfo{}
		},
	}
}

type NodeInfo struct {
	NodeName       string   `json:"node_name"`
	HTTPAddr       string   `json:"http_addr"`
	Transport      string   `json:"transport"`
	PeerID         string   `json:"peer_id,omitempty"`
	ListenAddrs    []string `json:"listen_addrs,omitempty"`
	Bootstrap      []string `json:"bootstrap,omitempty"`
	ConnectedPeers []string `json:"connected_peers,omitempty"`
}

func (s *Server) SetNodeInfoProvider(provider func() NodeInfo) {
	if provider == nil {
		return
	}
	s.nodeInfo = provider
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/lazyless/market/apps", s.handleCityMarketApps)
	mux.HandleFunc("/api/lazyless/market/stream", s.handleCityMarketStream)
	mux.HandleFunc("/api/lazyless/control/installed", s.handleCityInstalled)
	mux.HandleFunc("/api/lazyless/control/install", s.handleCityInstall)
	mux.HandleFunc("/api/lazyless/control/apps/", s.handleCityAppDetail)
	mux.HandleFunc("/api/lazyless/node", s.handleCityNode)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCityMarketApps(w http.ResponseWriter, r *http.Request) {
	if s.city == nil {
		writeError(w, http.StatusServiceUnavailable, "lazyless unavailable")
		return
	}
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	switch r.Method {
	case http.MethodGet:
		kind := r.URL.Query().Get("kind")
		tag := r.URL.Query().Get("tag")
		writeJSON(w, http.StatusOK, map[string]any{"apps": s.city.ListMarket(kind, tag)})
	case http.MethodPost:
		var req struct {
			AppID         string                  `json:"app_id"`
			Publisher     string                  `json:"publisher"`
			Manifest      map[string]any          `json:"manifest"`
			DefaultPolicy execution.SandboxPolicy `json:"default_policy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		manifest := marketListingManifest(req.AppID, req.Manifest)
		listing, err := s.city.Publish(market.Listing{
			AppID:         req.AppID,
			Publisher:     req.Publisher,
			Manifest:      manifest,
			DefaultPolicy: req.DefaultPolicy,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"listing": listing})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCityMarketStream(w http.ResponseWriter, r *http.Request) {
	if s.city == nil {
		writeError(w, http.StatusServiceUnavailable, "lazyless unavailable")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	ch, cancel, err := s.city.SubscribeMarket()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if _, err := fmt.Fprintf(w, "event: market\ndata: %s\n\n", string(msg.Payload)); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleCityInstalled(w http.ResponseWriter, r *http.Request) {
	if s.city == nil {
		writeError(w, http.StatusServiceUnavailable, "lazyless unavailable")
		return
	}
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"installed": s.city.ListInstalled()})
}

func (s *Server) handleCityInstall(w http.ResponseWriter, r *http.Request) {
	if s.city == nil {
		writeError(w, http.StatusServiceUnavailable, "lazyless unavailable")
		return
	}
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		AppID  string                  `json:"app_id"`
		Policy execution.SandboxPolicy `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.city.InstallFromMarket(req.AppID, req.Policy); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"installed": req.AppID})
}

func (s *Server) handleCityAppDetail(w http.ResponseWriter, r *http.Request) {
	if s.city == nil {
		writeError(w, http.StatusServiceUnavailable, "lazyless unavailable")
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/lazyless/control/apps/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) < 2 {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	appID := parts[0]
	action := parts[1]

	switch {
	case action == "start" && r.Method == http.MethodPost:
		err := s.city.Start(context.Background(), appID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"started": appID})
	case action == "stop" && r.Method == http.MethodPost:
		err := s.city.Stop(context.Background(), appID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"stopped": appID})
	case action == "health" && r.Method == http.MethodGet:
		health, err := s.city.Health(context.Background(), appID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"health": health})
	case action == "invoke" && r.Method == http.MethodPost:
		var req struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		out, err := s.city.Invoke(context.Background(), appID, req.Method, req.Params)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"result": out})
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func (s *Server) handleCityNode(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, s.nodeInfo())
}

func marketListingManifest(appID string, raw map[string]any) control.Manifest {
	manifest := control.Manifest{
		AppID:       appID,
		Name:        stringFromMap(raw, "name"),
		Version:     stringFromMap(raw, "version"),
		Kind:        stringFromMap(raw, "kind"),
		Description: stringFromMap(raw, "description"),
		Factory:     stringFromMap(raw, "factory"),
		LaunchURL:   stringFromMap(raw, "launch_url"),
	}
	if tags, ok := raw["tags"].([]any); ok {
		manifest.Tags = make([]string, 0, len(tags))
		for _, t := range tags {
			if s, ok := t.(string); ok {
				manifest.Tags = append(manifest.Tags, s)
			}
		}
	}
	return manifest
}

func stringFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func writeNoContent(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	w.WriteHeader(http.StatusNoContent)
}
