package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"ClawdCity/internal/clawdcity"
	"ClawdCity/internal/core/network"
)

func TestClawdCityInstallStartHealth(t *testing.T) {
	pubsub := network.NewMemoryPubSub()
	city, err := clawdcity.New(pubsub)
	if err != nil {
		t.Fatalf("new city: %v", err)
	}
	server := NewServer(city)
	mux := http.NewServeMux()
	server.Register(mux)

	marketReq := httptest.NewRequest(http.MethodGet, "/api/clawdcity/market/apps", nil)
	marketRec := httptest.NewRecorder()
	mux.ServeHTTP(marketRec, marketReq)
	if marketRec.Code != http.StatusOK {
		t.Fatalf("market failed: %d %s", marketRec.Code, marketRec.Body.String())
	}
	if !bytes.Contains(marketRec.Body.Bytes(), []byte(`"app_id":"social-web"`)) {
		t.Fatalf("social-web should be in market: %s", marketRec.Body.String())
	}

	installReq := httptest.NewRequest(http.MethodPost, "/api/clawdcity/control/install", bytes.NewReader([]byte(`{"app_id":"social-web"}`)))
	installRec := httptest.NewRecorder()
	mux.ServeHTTP(installRec, installReq)
	if installRec.Code != http.StatusOK {
		t.Fatalf("install failed: %d %s", installRec.Code, installRec.Body.String())
	}

	startReq := httptest.NewRequest(http.MethodPost, "/api/clawdcity/control/apps/social-web/start", nil)
	startRec := httptest.NewRecorder()
	mux.ServeHTTP(startRec, startReq)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start failed: %d %s", startRec.Code, startRec.Body.String())
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/api/clawdcity/control/apps/social-web/health", nil)
	healthRec := httptest.NewRecorder()
	mux.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("health failed: %d %s", healthRec.Code, healthRec.Body.String())
	}
}

func TestClawdCityNodeEndpoint(t *testing.T) {
	pubsub := network.NewMemoryPubSub()
	city, err := clawdcity.New(pubsub)
	if err != nil {
		t.Fatalf("new city: %v", err)
	}
	server := NewServer(city)
	server.SetNodeInfoProvider(func() NodeInfo {
		return NodeInfo{
			NodeName:  "ClawdCity",
			HTTPAddr:  ":8080",
			Transport: "memory",
		}
	})
	mux := http.NewServeMux()
	server.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/clawdcity/node", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("node endpoint failed: %d %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"transport":"memory"`)) {
		t.Fatalf("unexpected node response: %s", rec.Body.String())
	}
}
