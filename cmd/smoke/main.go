package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	baseURL := flag.String("base", "http://127.0.0.1:8080", "server base url")
	flag.Parse()

	client := &http.Client{}
	sessionID, err := createSession(client, *baseURL)
	must(err)
	fmt.Printf("session: %s\n", sessionID)

	must(action(client, *baseURL, sessionID, map[string]any{"player_id": "alice", "type": "join", "amount": 1000}))
	must(action(client, *baseURL, sessionID, map[string]any{"player_id": "bob", "type": "join", "amount": 1000}))
	must(action(client, *baseURL, sessionID, map[string]any{"player_id": "alice", "type": "start_hand"}))

	aliceSeed := "alice-seed"
	bobSeed := "bob-seed"
	must(action(client, *baseURL, sessionID, map[string]any{"player_id": "alice", "type": "commit", "data": map[string]any{"hash": hash(aliceSeed)}}))
	must(action(client, *baseURL, sessionID, map[string]any{"player_id": "bob", "type": "commit", "data": map[string]any{"hash": hash(bobSeed)}}))
	must(action(client, *baseURL, sessionID, map[string]any{"player_id": "alice", "type": "reveal", "data": map[string]any{"seed": aliceSeed}}))
	must(action(client, *baseURL, sessionID, map[string]any{"player_id": "bob", "type": "reveal", "data": map[string]any{"seed": bobSeed}}))

	view, err := get(client, *baseURL+"/api/sessions/"+sessionID+"/view?player_id=alice")
	must(err)
	fmt.Printf("view: %s\n", view)
	fmt.Println("smoke test passed")
}

func createSession(client *http.Client, baseURL string) (string, error) {
	body := map[string]any{
		"game_id": "poker",
		"params":  map[string]any{"small_blind": 10, "big_blind": 20, "max_players": 6},
	}
	resp, err := postJSON(client, baseURL+"/api/sessions", body)
	if err != nil {
		return "", err
	}
	var data struct {
		SessionID string `json:"session_id"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(resp, &data); err != nil {
		return "", err
	}
	if data.Error != "" {
		return "", errors.New(data.Error)
	}
	if data.SessionID == "" {
		return "", fmt.Errorf("missing session_id")
	}
	return data.SessionID, nil
}

func action(client *http.Client, baseURL, sid string, payload map[string]any) error {
	resp, err := postJSON(client, baseURL+"/api/sessions/"+sid+"/actions", payload)
	if err != nil {
		return err
	}
	if bytes.Contains(resp, []byte(`"error"`)) {
		return fmt.Errorf("action failed: %s", string(resp))
	}
	return nil
}

func postJSON(client *http.Client, url string, payload any) ([]byte, error) {
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func get(client *http.Client, url string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	return string(b), nil
}

func hash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("%x", sum[:])
}

func must(err error) {
	if err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}
}
