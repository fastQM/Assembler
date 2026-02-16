package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ClawdCity/internal/clawdcity"
	"ClawdCity/internal/clawdcity/control"
	"ClawdCity/internal/clawdcity/execution"
	"ClawdCity/internal/clawdcity/market"
	"ClawdCity/internal/runtime"
	"ClawdCity/internal/tetrisroom"
)

type Server struct {
	engine *runtime.Engine
	city   *clawdcity.City
	tetris *tetrisroom.Manager
}

func NewServer(engine *runtime.Engine, city *clawdcity.City, tetris *tetrisroom.Manager) *Server {
	return &Server{engine: engine, city: city, tetris: tetris}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/hash", s.handleHash)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/", s.handleSessionDetail)
	mux.HandleFunc("/api/clawdcity/market/apps", s.handleCityMarketApps)
	mux.HandleFunc("/api/clawdcity/market/stream", s.handleCityMarketStream)
	mux.HandleFunc("/api/clawdcity/control/installed", s.handleCityInstalled)
	mux.HandleFunc("/api/clawdcity/control/install", s.handleCityInstall)
	mux.HandleFunc("/api/clawdcity/control/apps/", s.handleCityAppDetail)
	mux.HandleFunc("/api/tetris/register", s.handleTetrisRegister)
	mux.HandleFunc("/api/tetris/ready", s.handleTetrisReady)
	mux.HandleFunc("/api/tetris/player/", s.handleTetrisPlayer)
	mux.HandleFunc("/api/tetris/room/", s.handleTetrisRoom)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleHash(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Seed string `json:"seed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.Seed) == "" {
		writeError(w, http.StatusBadRequest, "missing seed")
		return
	}
	sum := sha256.Sum256([]byte(req.Seed))
	writeJSON(w, http.StatusOK, map[string]any{"hash": fmt.Sprintf("%x", sum[:])})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": s.engine.ListSessions()})
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		GameID string         `json:"game_id"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.GameID == "" {
		writeError(w, http.StatusBadRequest, "missing game_id")
		return
	}
	id, err := s.engine.CreateSession(req.GameID, req.Params)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session_id": id})
}

func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "missing session id")
		return
	}
	sessionID := parts[0]
	subPath := ""
	if len(parts) > 1 {
		subPath = parts[1]
	}

	switch {
	case subPath == "stream" && r.Method == http.MethodGet:
		s.handleStream(w, r, sessionID)
	case subPath == "actions" && r.Method == http.MethodPost:
		s.handleAction(w, r, sessionID)
	case subPath == "view" && r.Method == http.MethodGet:
		s.handleView(w, r, sessionID)
	case subPath == "events" && r.Method == http.MethodGet:
		s.handleEvents(w, r, sessionID)
	case r.Method == http.MethodOptions:
		writeNoContent(w)
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	var action runtime.Action
	if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
		writeError(w, http.StatusBadRequest, "invalid action json")
		return
	}
	events, err := s.engine.SubmitAction(sessionID, action)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, runtime.ErrSessionNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handleView(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	playerID := r.URL.Query().Get("player_id")
	if playerID == "" {
		writeError(w, http.StatusBadRequest, "missing player_id")
		return
	}
	view, err := s.engine.View(sessionID, playerID)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, runtime.ErrSessionNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"view": view})
}

func (s *Server) handleEvents(w http.ResponseWriter, _ *http.Request, sessionID string) {
	events, err := s.engine.Events(sessionID)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, runtime.ErrSessionNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request, sessionID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	ch, cancel, err := s.engine.Subscribe(sessionID)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, runtime.ErrSessionNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(msg.Payload)); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleCityMarketApps(w http.ResponseWriter, r *http.Request) {
	if s.city == nil {
		writeError(w, http.StatusServiceUnavailable, "clawdcity unavailable")
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
		writeError(w, http.StatusServiceUnavailable, "clawdcity unavailable")
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
		writeError(w, http.StatusServiceUnavailable, "clawdcity unavailable")
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
		writeError(w, http.StatusServiceUnavailable, "clawdcity unavailable")
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
		writeError(w, http.StatusServiceUnavailable, "clawdcity unavailable")
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/clawdcity/control/apps/")
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

func marketListingManifest(appID string, raw map[string]any) control.Manifest {
	manifest := control.Manifest{
		AppID:       appID,
		Name:        stringFromMap(raw, "name"),
		Version:     stringFromMap(raw, "version"),
		Kind:        stringFromMap(raw, "kind"),
		Description: stringFromMap(raw, "description"),
		Factory:     stringFromMap(raw, "factory"),
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
