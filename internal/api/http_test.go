package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"ClawdCity/internal/clawdcity"
	"ClawdCity/internal/core/network"
)

func TestClawdCityInstallStartInvoke(t *testing.T) {
	pubsub := network.NewMemoryPubSub()
	city, err := clawdcity.New(pubsub)
	if err != nil {
		t.Fatalf("new city: %v", err)
	}
	server := NewServer(city)
	mux := http.NewServeMux()
	server.Register(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/api/clawdcity/control/installed", nil)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("installed failed: %d %s", listRec.Code, listRec.Body.String())
	}
	if !bytes.Contains(listRec.Body.Bytes(), []byte(`"app_id":"appmarket"`)) {
		t.Fatalf("appmarket should be preinstalled: %s", listRec.Body.String())
	}

	invokeReqBody := []byte(`{"method":"about","params":{}}`)
	invokeReq := httptest.NewRequest(http.MethodPost, "/api/clawdcity/control/apps/appmarket/invoke", bytes.NewReader(invokeReqBody))
	invokeRec := httptest.NewRecorder()
	mux.ServeHTTP(invokeRec, invokeReq)
	if invokeRec.Code != http.StatusOK {
		t.Fatalf("invoke failed: %d %s", invokeRec.Code, invokeRec.Body.String())
	}
	if !bytes.Contains(invokeRec.Body.Bytes(), []byte(`"name":"AppMarket"`)) {
		t.Fatalf("unexpected invoke response: %s", invokeRec.Body.String())
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
