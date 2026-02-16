package tetrisroom

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"ClawdCity/internal/core/network"
)

const (
	ControlHuman = "human"
	ControlAgent = "agent"

	SourceHuman = "human"
	SourceAgent = "agent"
)

var (
	ErrPlayerExists         = errors.New("player already exists")
	ErrPlayerNotFound       = errors.New("player not found")
	ErrRoomNotFound         = errors.New("room not found")
	ErrAlreadyInRoom        = errors.New("player already in room")
	ErrInvalidControlMode   = errors.New("invalid control mode")
	ErrControlModeMismatch  = errors.New("input source does not match control mode")
	ErrPlayerNotInRoom      = errors.New("player not in room")
	ErrPlayerNotRoomMember  = errors.New("player is not room member")
	ErrPingRequiredForReady = errors.New("ping_ms required and must be >= 0")
)

type Player struct {
	ID          string    `json:"id"`
	AppID       string    `json:"app_id"`
	Version     string    `json:"version"`
	PingMS      int       `json:"ping_ms"`
	Ready       bool      `json:"ready"`
	RoomID      string    `json:"room_id,omitempty"`
	ControlMode string    `json:"control_mode"`
	AgentID     string    `json:"agent_id,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Room struct {
	ID        string    `json:"id"`
	AppID     string    `json:"app_id"`
	Version   string    `json:"version"`
	HostID    string    `json:"host_id"`
	PlayerIDs []string  `json:"player_ids"`
	CreatedAt time.Time `json:"created_at"`
}

type InputEvent struct {
	PlayerID string         `json:"player_id"`
	Source   string         `json:"source"`
	Action   string         `json:"action"`
	Payload  map[string]any `json:"payload,omitempty"`
	Tick     int64          `json:"tick,omitempty"`
	At       time.Time      `json:"at"`
}

type Event struct {
	Type   string         `json:"type"`
	RoomID string         `json:"room_id,omitempty"`
	Player *Player        `json:"player,omitempty"`
	Room   *Room          `json:"room,omitempty"`
	Input  *InputEvent    `json:"input,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
	At     time.Time      `json:"at"`
}

// Manager manages matchmaking and room lifecycle.
type Manager struct {
	mu      sync.RWMutex
	pubsub  network.PubSub
	players map[string]*Player
	rooms   map[string]*Room
	seq     atomic.Int64
}

func NewManager(pubsub network.PubSub) *Manager {
	return &Manager{
		pubsub:  pubsub,
		players: make(map[string]*Player),
		rooms:   make(map[string]*Room),
	}
}

func (m *Manager) RegisterPlayer(id, appID, version string) (*Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.players[id]; ok {
		return nil, ErrPlayerExists
	}
	p := &Player{
		ID:          id,
		AppID:       appID,
		Version:     version,
		ControlMode: ControlHuman,
		UpdatedAt:   time.Now().UTC(),
	}
	m.players[id] = p
	cp := *p
	return &cp, nil
}

func (m *Manager) UpsertPlayer(id, appID, version string) (*Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.players[id]
	if !ok {
		p = &Player{ID: id, ControlMode: ControlHuman}
		m.players[id] = p
	}
	if p.RoomID != "" && (appID != "" && appID != p.AppID || version != "" && version != p.Version) {
		return nil, ErrAlreadyInRoom
	}
	if appID != "" {
		p.AppID = appID
	}
	if version != "" {
		p.Version = version
	}
	if p.ControlMode == "" {
		p.ControlMode = ControlHuman
	}
	p.UpdatedAt = time.Now().UTC()
	cp := *p
	return &cp, nil
}

func (m *Manager) SetReady(playerID string, pingMS int) (*Room, error) {
	if pingMS < 0 {
		return nil, ErrPingRequiredForReady
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.players[playerID]
	if !ok {
		return nil, ErrPlayerNotFound
	}
	if p.RoomID != "" {
		return nil, ErrAlreadyInRoom
	}
	p.Ready = true
	p.PingMS = pingMS
	p.UpdatedAt = time.Now().UTC()

	m.publishPlayerLocked("player_ready", p)
	room := m.tryMatchLocked(p.AppID, p.Version)
	if room == nil {
		return nil, nil
	}
	cp := *room
	return &cp, nil
}

func (m *Manager) tryMatchLocked(appID, version string) *Room {
	candidates := make([]*Player, 0, 4)
	for _, p := range m.players {
		if p.Ready && p.RoomID == "" && p.AppID == appID && p.Version == version {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) < 2 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].PingMS == candidates[j].PingMS {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].PingMS < candidates[j].PingMS
	})
	members := candidates[:2]
	host := members[0]

	roomID := fmt.Sprintf("room_%d", m.seq.Add(1))
	room := &Room{
		ID:        roomID,
		AppID:     appID,
		Version:   version,
		HostID:    host.ID,
		PlayerIDs: []string{members[0].ID, members[1].ID},
		CreatedAt: time.Now().UTC(),
	}
	m.rooms[roomID] = room
	for _, member := range members {
		member.RoomID = roomID
		member.Ready = false
		member.ControlMode = ControlHuman
		member.AgentID = ""
		member.UpdatedAt = time.Now().UTC()
	}

	m.publishRoomLocked("room_assigned", room, map[string]any{"reason": "all_ready", "host_ping_ms": host.PingMS})
	return room
}

func (m *Manager) GetPlayer(playerID string) (*Player, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.players[playerID]
	if !ok {
		return nil, ErrPlayerNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *Manager) GetRoom(roomID string) (*Room, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, ErrRoomNotFound
	}
	cp := *r
	cp.PlayerIDs = append([]string(nil), r.PlayerIDs...)
	return &cp, nil
}

func (m *Manager) ToggleControl(roomID, playerID, toMode, agentID string) (*Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if toMode != ControlHuman && toMode != ControlAgent {
		return nil, ErrInvalidControlMode
	}
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, ErrRoomNotFound
	}
	if !contains(r.PlayerIDs, playerID) {
		return nil, ErrPlayerNotRoomMember
	}
	p, ok := m.players[playerID]
	if !ok {
		return nil, ErrPlayerNotFound
	}
	if p.RoomID != roomID {
		return nil, ErrPlayerNotInRoom
	}
	from := p.ControlMode
	p.ControlMode = toMode
	if toMode == ControlAgent {
		p.AgentID = agentID
	} else {
		p.AgentID = ""
	}
	p.UpdatedAt = time.Now().UTC()

	m.publishRoomLocked("control_switch_applied", r, map[string]any{
		"player_id": playerID,
		"from_mode": from,
		"to_mode":   toMode,
		"agent_id":  p.AgentID,
	})

	cp := *p
	return &cp, nil
}

func (m *Manager) SubmitInput(roomID string, in InputEvent) error {
	m.mu.RLock()
	r, ok := m.rooms[roomID]
	if !ok {
		m.mu.RUnlock()
		return ErrRoomNotFound
	}
	if !contains(r.PlayerIDs, in.PlayerID) {
		m.mu.RUnlock()
		return ErrPlayerNotRoomMember
	}
	p, ok := m.players[in.PlayerID]
	if !ok {
		m.mu.RUnlock()
		return ErrPlayerNotFound
	}
	if p.RoomID != roomID {
		m.mu.RUnlock()
		return ErrPlayerNotInRoom
	}
	if p.ControlMode == ControlHuman && in.Source != SourceHuman {
		m.mu.RUnlock()
		return ErrControlModeMismatch
	}
	if p.ControlMode == ControlAgent && in.Source != SourceAgent {
		m.mu.RUnlock()
		return ErrControlModeMismatch
	}
	m.mu.RUnlock()

	if in.At.IsZero() {
		in.At = time.Now().UTC()
	}
	b, _ := json.Marshal(Event{Type: "room_input", RoomID: roomID, Input: &in, At: in.At})
	return m.pubsub.Publish(topicForRoom(roomID), b)
}

func (m *Manager) SubscribeRoom(roomID string) (<-chan network.Message, func(), error) {
	return m.pubsub.Subscribe(topicForRoom(roomID))
}

func (m *Manager) publishPlayerLocked(eventType string, p *Player) {
	cp := *p
	b, _ := json.Marshal(Event{Type: eventType, Player: &cp, At: time.Now().UTC()})
	_ = m.pubsub.Publish("tetris.player", b)
}

func (m *Manager) publishRoomLocked(eventType string, r *Room, meta map[string]any) {
	cp := *r
	cp.PlayerIDs = append([]string(nil), r.PlayerIDs...)
	evt := Event{Type: eventType, RoomID: r.ID, Room: &cp, Meta: meta, At: time.Now().UTC()}
	b, _ := json.Marshal(evt)
	_ = m.pubsub.Publish(topicForRoom(r.ID), b)
	_ = m.pubsub.Publish("tetris.room", b)
}

func topicForRoom(roomID string) string {
	return "tetris.room." + roomID
}

func contains(items []string, id string) bool {
	for _, item := range items {
		if item == id {
			return true
		}
	}
	return false
}
