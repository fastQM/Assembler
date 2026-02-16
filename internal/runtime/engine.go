package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"ClawdCity/internal/core/network"
)

var (
	ErrAdapterNotFound = errors.New("adapter not found")
	ErrSessionNotFound = errors.New("session not found")
)

type session struct {
	mu      sync.RWMutex
	id      string
	gameID  string
	adapter Adapter
	state   any
	events  []Event
	nextSeq int64
}

type SessionInfo struct {
	ID     string `json:"id"`
	GameID string `json:"game_id"`
	Events int    `json:"events"`
}

// Engine hosts generic sessions and routes all I/O through adapters.
type Engine struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
	sessions map[string]*session
	pubsub   network.PubSub
	counter  atomic.Int64
}

func NewEngine(pubsub network.PubSub) *Engine {
	return &Engine{
		adapters: make(map[string]Adapter),
		sessions: make(map[string]*session),
		pubsub:   pubsub,
	}
}

func (e *Engine) RegisterAdapter(a Adapter) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.adapters[a.ID()] = a
}

func (e *Engine) CreateSession(gameID string, params map[string]any) (string, error) {
	e.mu.RLock()
	adapter, ok := e.adapters[gameID]
	e.mu.RUnlock()
	if !ok {
		return "", ErrAdapterNotFound
	}
	state, err := adapter.Init(params)
	if err != nil {
		return "", fmt.Errorf("init state: %w", err)
	}
	sessionID := fmt.Sprintf("s_%d", e.counter.Add(1))

	s := &session{
		id:      sessionID,
		gameID:  gameID,
		adapter: adapter,
		state:   state,
		events:  make([]Event, 0, 64),
		nextSeq: 1,
	}

	e.mu.Lock()
	e.sessions[sessionID] = s
	e.mu.Unlock()

	return sessionID, nil
}

func (e *Engine) SubmitAction(sessionID string, action Action) ([]Event, error) {
	s, err := e.getSession(sessionID)
	if err != nil {
		return nil, err
	}
	if action.At.IsZero() {
		action.At = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.adapter.ValidateAction(s.state, action); err != nil {
		return nil, err
	}
	next, events, err := s.adapter.ApplyAction(s.state, action)
	if err != nil {
		return nil, err
	}
	for i := range events {
		events[i].Seq = s.nextSeq
		events[i].SessionID = sessionID
		if events[i].At.IsZero() {
			events[i].At = action.At
		}
		s.nextSeq++
	}
	s.state = next
	s.events = append(s.events, events...)

	for _, evt := range events {
		data, _ := json.Marshal(evt)
		_ = e.pubsub.Publish(topicForSession(sessionID), data)
	}

	copyOut := append([]Event(nil), events...)
	return copyOut, nil
}

func (e *Engine) View(sessionID, playerID string) (any, error) {
	s, err := e.getSession(sessionID)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.adapter.View(s.state, playerID)
}

func (e *Engine) Events(sessionID string) ([]Event, error) {
	s, err := e.getSession(sessionID)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]Event(nil), s.events...)
	return out, nil
}

func (e *Engine) Subscribe(sessionID string) (<-chan network.Message, func(), error) {
	if _, err := e.getSession(sessionID); err != nil {
		return nil, nil, err
	}
	return e.pubsub.Subscribe(topicForSession(sessionID))
}

func (e *Engine) ListSessions() []SessionInfo {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]SessionInfo, 0, len(e.sessions))
	for _, s := range e.sessions {
		s.mu.RLock()
		out = append(out, SessionInfo{
			ID:     s.id,
			GameID: s.gameID,
			Events: len(s.events),
		})
		s.mu.RUnlock()
	}
	return out
}

func topicForSession(sessionID string) string {
	return "session." + sessionID
}

func (e *Engine) getSession(sessionID string) (*session, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, ok := e.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return s, nil
}
