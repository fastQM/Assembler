package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	engineA := runtime.NewEngine(pubsub)
	engineA.RegisterAdapter(poker.NewAdapter())
	cityA, err := clawdcity.New(pubsub)
	if err != nil {
		t.Fatalf("new city: %v", err)
	}
	engineB := runtime.NewEngine(pubsub)
	engineB.RegisterAdapter(poker.NewAdapter())
	cityB, err := clawdcity.New(pubsub)
	if err != nil {
		t.Fatalf("new city: %v", err)
	}

	serverA := NewServer(engineA, cityA, tetrisroom.NewManager(pubsub))
	muxA := http.NewServeMux()
	serverA.Register(muxA)
	serverB := NewServer(engineB, cityB, tetrisroom.NewManager(pubsub))
	muxB := http.NewServeMux()
	serverB.Register(muxB)

	reg := func(mux *http.ServeMux, player string) {
		body := []byte(`{"player_id":"` + player + `","app_id":"tetris","version":"0.1.0"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/tetris/register", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("register %s failed: %d %s", player, rec.Code, rec.Body.String())
		}
	}
	reg(muxA, "p1")
	reg(muxB, "p2")

	req1 := httptest.NewRequest(http.MethodPost, "/api/tetris/ready", bytes.NewReader([]byte(`{"player_id":"p1","ping_ms":50}`)))
	rec1 := httptest.NewRecorder()
	muxA.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("p1 ready failed: %d", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/tetris/ready", bytes.NewReader([]byte(`{"player_id":"p2","ping_ms":20}`)))
	rec2 := httptest.NewRecorder()
	muxB.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("p2 ready failed: %d body=%s", rec2.Code, rec2.Body.String())
	}

	var body string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		roomReq := httptest.NewRequest(http.MethodGet, "/api/tetris/player/p1", nil)
		roomRec := httptest.NewRecorder()
		muxA.ServeHTTP(roomRec, roomReq)
		if roomRec.Code != http.StatusOK {
			t.Fatalf("get player failed: %d", roomRec.Code)
		}
		body = roomRec.Body.String()
		if bytes.Contains([]byte(body), []byte(`"room_id":"`)) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !bytes.Contains([]byte(body), []byte(`"room_id":"`)) {
		t.Fatalf("expected player in room, got %s", body)
	}

	roomReq2 := httptest.NewRequest(http.MethodGet, "/api/tetris/player/p2", nil)
	roomRec2 := httptest.NewRecorder()
	muxB.ServeHTTP(roomRec2, roomReq2)
	if roomRec2.Code != http.StatusOK {
		t.Fatalf("get p2 failed: %d", roomRec2.Code)
	}
	if !bytes.Contains(roomRec2.Body.Bytes(), []byte(`"room_id":"`)) {
		t.Fatalf("expected p2 in room, got %s", roomRec2.Body.String())
	}

	// Push one state_sync input from p1 and verify room state endpoint.
	var p1 struct {
		Player struct {
			RoomID string `json:"room_id"`
		} `json:"player"`
	}
	if err := json.Unmarshal([]byte(body), &p1); err != nil {
		t.Fatalf("unmarshal p1 body: %v", err)
	}
	if p1.Player.RoomID == "" {
		t.Fatalf("missing room id in p1 body: %s", body)
	}

	stateIn := []byte(`{"player_id":"p1","source":"human","action":"state_sync","payload":{"board":["..........",".........."],"score":10,"lines":1,"level":1,"game_over":false}}`)
	inReq := httptest.NewRequest(http.MethodPost, "/api/tetris/room/"+p1.Player.RoomID+"/input", bytes.NewReader(stateIn))
	inRec := httptest.NewRecorder()
	muxA.ServeHTTP(inRec, inReq)
	if inRec.Code != http.StatusOK {
		t.Fatalf("state input failed: %d %s", inRec.Code, inRec.Body.String())
	}

	stateReq := httptest.NewRequest(http.MethodGet, "/api/tetris/room/"+p1.Player.RoomID+"/state", nil)
	stateRec := httptest.NewRecorder()
	muxA.ServeHTTP(stateRec, stateReq)
	if stateRec.Code != http.StatusOK {
		t.Fatalf("room state failed: %d %s", stateRec.Code, stateRec.Body.String())
	}
	if !bytes.Contains(stateRec.Body.Bytes(), []byte(`"states"`)) || !bytes.Contains(stateRec.Body.Bytes(), []byte(`"p1"`)) {
		t.Fatalf("unexpected room state body: %s", stateRec.Body.String())
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
