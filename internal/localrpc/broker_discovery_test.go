package localrpc

import (
	"path/filepath"
	"sync"
	"testing"

	"Assembler/internal/core/network"
)

type discoverySpyPubSub struct {
	mu         sync.Mutex
	discovery  []string
	subscribed []string
}

func (s *discoverySpyPubSub) EnsureAppDiscovery(appID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discovery = append(s.discovery, appID)
	return nil
}

func (s *discoverySpyPubSub) Publish(string, []byte) error { return nil }

func (s *discoverySpyPubSub) Subscribe(topic string) (<-chan network.Message, func(), error) {
	s.mu.Lock()
	s.subscribed = append(s.subscribed, topic)
	s.mu.Unlock()
	ch := make(chan network.Message)
	cancel := func() { close(ch) }
	return ch, cancel, nil
}

func TestBrokerEnsuresAppDiscovery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := newHistoryStore(filepath.Join(dir, "messages.jsonl"), filepath.Join(dir, "cursors.json"))
	if err != nil {
		t.Fatalf("new history store: %v", err)
	}
	spy := &discoverySpyPubSub{}
	b := newBroker(spy, store, nil)
	api := &API{b: b}

	var subReply SubscribeReply
	if err := api.Subscribe(SubscribeArgs{AppID: "social", Topics: []string{"app.social.v1.global.presence"}}, &subReply); err != nil {
		t.Fatalf("subscribe rpc: %v", err)
	}
	if subReply.Error != "" {
		t.Fatalf("subscribe failed: %s", subReply.Error)
	}

	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.discovery) == 0 || spy.discovery[0] != "social" {
		t.Fatalf("expected discovery to be ensured for social app, got %+v", spy.discovery)
	}
}
