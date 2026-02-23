package market

import (
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"ClawdCity/internal/assembler/control"
	"ClawdCity/internal/assembler/execution"
	"ClawdCity/internal/core/network"
)

const Topic = "assembler.market"

var ErrAppIDRequired = errors.New("app id required")

type Listing struct {
	AppID         string                  `json:"app_id"`
	Publisher     string                  `json:"publisher"`
	Manifest      control.Manifest        `json:"manifest"`
	DefaultPolicy execution.SandboxPolicy `json:"default_policy"`
	CreatedAt     time.Time               `json:"created_at"`
}

type Event struct {
	Type    string    `json:"type"`
	AppID   string    `json:"app_id"`
	At      time.Time `json:"at"`
	Listing Listing   `json:"listing"`
}

type Manager struct {
	mu     sync.RWMutex
	pubsub network.PubSub
	apps   map[string]Listing
	topic  string
	cancel func()
}

func NewManager(pubsub network.PubSub) *Manager {
	m := &Manager{pubsub: pubsub, apps: map[string]Listing{}, topic: Topic}
	m.startSync()
	return m
}

func (m *Manager) Publish(listing Listing) (Listing, error) {
	if strings.TrimSpace(listing.AppID) == "" {
		return Listing{}, ErrAppIDRequired
	}
	if listing.Manifest.AppID == "" {
		listing.Manifest.AppID = listing.AppID
	}
	if listing.CreatedAt.IsZero() {
		listing.CreatedAt = time.Now().UTC()
	}

	m.mu.Lock()
	m.apps[listing.AppID] = listing
	m.mu.Unlock()

	evt := Event{Type: "published", AppID: listing.AppID, At: time.Now().UTC(), Listing: listing}
	b, _ := json.Marshal(evt)
	_ = m.pubsub.Publish(m.topic, b)
	return listing, nil
}

func (m *Manager) List(kind string, tag string) []Listing {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Listing, 0, len(m.apps))
	for _, a := range m.apps {
		if kind != "" && a.Manifest.Kind != kind {
			continue
		}
		if tag != "" && !contains(a.Manifest.Tags, tag) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func (m *Manager) Get(appID string) (Listing, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.apps[appID]
	return v, ok
}

func (m *Manager) Subscribe() (<-chan network.Message, func(), error) {
	return m.pubsub.Subscribe(m.topic)
}

func (m *Manager) startSync() {
	ch, cancel, err := m.pubsub.Subscribe(m.topic)
	if err != nil {
		log.Printf("market sync subscribe failed: %v", err)
		return
	}
	m.cancel = cancel
	go func() {
		for msg := range ch {
			var evt Event
			if err := json.Unmarshal(msg.Payload, &evt); err != nil {
				continue
			}
			if strings.TrimSpace(evt.AppID) == "" {
				continue
			}
			switch evt.Type {
			case "published":
				m.mu.Lock()
				current, ok := m.apps[evt.AppID]
				if !ok || evt.Listing.CreatedAt.After(current.CreatedAt) || evt.Listing.CreatedAt.Equal(current.CreatedAt) {
					m.apps[evt.AppID] = evt.Listing
				}
				m.mu.Unlock()
			}
		}
	}()
}

func contains(list []string, key string) bool {
	for _, v := range list {
		if v == key {
			return true
		}
	}
	return false
}
