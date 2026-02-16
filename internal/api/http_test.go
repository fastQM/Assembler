package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ClawdCity/internal/clawdcity"
	"ClawdCity/internal/core/network"
	"ClawdCity/internal/games/poker"
	"ClawdCity/internal/runtime"
	"ClawdCity/internal/tetrisroom"
)

func TestCreateSessionAndList(t *testing.T) {
	pubsub := network.NewMemoryPubSub()
	engine := runtime.NewEngine(pubsub)
	engine.RegisterAdapter(poker.NewAdapter())
	city, err := clawdcity.New(pubsub)
	if err != nil {
		t.Fatalf("new city: %v", err)
	}
	server := NewServer(engine, city, tetrisroom.NewManager(pubsub))

	mux := http.NewServeMux()
	server.Register(mux)

	createBody := map[string]any{
		"game_id": "poker",
		"params":  map[string]any{"small_blind": 10, "big_blind": 20, "max_players": 6},
	}
	createJSON, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(createJSON))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body=%s", createRec.Code, createRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d", listRec.Code)
	}
	if !bytes.Contains(listRec.Body.Bytes(), []byte("poker")) {
		t.Fatalf("list response missing poker: %s", listRec.Body.String())
	}
}

func TestHashEndpoint(t *testing.T) {
	pubsub := network.NewMemoryPubSub()
	engine := runtime.NewEngine(pubsub)
	city, err := clawdcity.New(pubsub)
	if err != nil {
		t.Fatalf("new city: %v", err)
	}
	server := NewServer(engine, city, tetrisroom.NewManager(pubsub))

	mux := http.NewServeMux()
	server.Register(mux)

	body := []byte(`{"seed":"abc"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/hash", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("ba7816bf")) {
		t.Fatalf("unexpected hash response: %s", rec.Body.String())
	}
}

func TestClawdCityInstallStartInvoke(t *testing.T) {
	pubsub := network.NewMemoryPubSub()
	engine := runtime.NewEngine(pubsub)
	engine.RegisterAdapter(poker.NewAdapter())
	city, err := clawdcity.New(pubsub)
	if err != nil {
		t.Fatalf("new city: %v", err)
	}
	server := NewServer(engine, city, tetrisroom.NewManager(pubsub))
	mux := http.NewServeMux()
	server.Register(mux)

	installReqBody := []byte(`{"app_id":"counter-game"}`)
	installReq := httptest.NewRequest(http.MethodPost, "/api/clawdcity/control/install", bytes.NewReader(installReqBody))
	installRec := httptest.NewRecorder()
	mux.ServeHTTP(installRec, installReq)
	if installRec.Code != http.StatusOK {
		t.Fatalf("install failed: %d %s", installRec.Code, installRec.Body.String())
	}

	startReq := httptest.NewRequest(http.MethodPost, "/api/clawdcity/control/apps/counter-game/start", nil)
	startRec := httptest.NewRecorder()
	mux.ServeHTTP(startRec, startReq)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start failed: %d %s", startRec.Code, startRec.Body.String())
	}

	invokeReqBody := []byte(`{"method":"inc","params":{"player":"alice"}}`)
	invokeReq := httptest.NewRequest(http.MethodPost, "/api/clawdcity/control/apps/counter-game/invoke", bytes.NewReader(invokeReqBody))
	invokeRec := httptest.NewRecorder()
	mux.ServeHTTP(invokeRec, invokeReq)
	if invokeRec.Code != http.StatusOK {
		t.Fatalf("invoke failed: %d %s", invokeRec.Code, invokeRec.Body.String())
	}
	if !bytes.Contains(invokeRec.Body.Bytes(), []byte(`"value":1`)) {
		t.Fatalf("unexpected invoke response: %s", invokeRec.Body.String())
	}
}

func TestTetrisRoomReadyAndControl(t *testing.T) {
	pubsub := network.NewMemoryPubSub()
	engine := runtime.NewEngine(pubsub)
	engine.RegisterAdapter(poker.NewAdapter())
	city, err := clawdcity.New(pubsub)
	if err != nil {
		t.Fatalf("new city: %v", err)
	}
	tetris := tetrisroom.NewManager(pubsub)
	server := NewServer(engine, city, tetris)
	mux := http.NewServeMux()
	server.Register(mux)

	reg := func(player string) {
		body := []byte(`{"player_id":"` + player + `","app_id":"tetris","version":"0.1.0"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/tetris/register", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("register %s failed: %d %s", player, rec.Code, rec.Body.String())
		}
	}
	reg("p1")
	reg("p2")

	req1 := httptest.NewRequest(http.MethodPost, "/api/tetris/ready", bytes.NewReader([]byte(`{"player_id":"p1","ping_ms":50}`)))
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("p1 ready failed: %d", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/tetris/ready", bytes.NewReader([]byte(`{"player_id":"p2","ping_ms":20}`)))
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("p2 ready failed: %d body=%s", rec2.Code, rec2.Body.String())
	}
	if !bytes.Contains(rec2.Body.Bytes(), []byte(`"matched":true`)) {
		t.Fatalf("expected matched response, got %s", rec2.Body.String())
	}
	if !bytes.Contains(rec2.Body.Bytes(), []byte(`"host_id":"p2"`)) {
		t.Fatalf("expected lower ping p2 as host, got %s", rec2.Body.String())
	}

	roomReq := httptest.NewRequest(http.MethodGet, "/api/tetris/player/p1", nil)
	roomRec := httptest.NewRecorder()
	mux.ServeHTTP(roomRec, roomReq)
	if roomRec.Code != http.StatusOK {
		t.Fatalf("get player failed: %d", roomRec.Code)
	}
	body := roomRec.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"room_id":"`)) {
		t.Fatalf("expected player in room, got %s", body)
	}
}

func TestClawdCityNodeEndpoint(t *testing.T) {
	pubsub := network.NewMemoryPubSub()
	engine := runtime.NewEngine(pubsub)
	city, err := clawdcity.New(pubsub)
	if err != nil {
		t.Fatalf("new city: %v", err)
	}
	server := NewServer(engine, city, tetrisroom.NewManager(pubsub))
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
