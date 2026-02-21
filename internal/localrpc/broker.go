package localrpc

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"ClawdCity/internal/core/network"
)

var (
	ErrForbiddenTopic   = errors.New("topic is not allowed for app")
	ErrSubscriptionGone = errors.New("subscription not found")
)

type NodeStatus struct {
	Transport      string
	PeerID         string
	ConnectedPeers int
}

type statusProvider func() NodeStatus

type subscription struct {
	id     string
	appID  string
	topics map[string]struct{}
	queue  chan MessageRecord
}

type broker struct {
	mu          sync.RWMutex
	pubsub      network.PubSub
	store       *historyStore
	subs        map[string]*subscription
	topicCancel map[string]func()
	statusFn    statusProvider
}

func newBroker(pubsub network.PubSub, store *historyStore, statusFn statusProvider) *broker {
	return &broker{
		pubsub:      pubsub,
		store:       store,
		subs:        make(map[string]*subscription),
		topicCancel: make(map[string]func()),
		statusFn:    statusFn,
	}
}

func (b *broker) publish(appID, topic string, payload []byte, headers map[string]string) (MessageRecord, error) {
	if err := validateTopic(appID, topic); err != nil {
		return MessageRecord{}, err
	}
	if err := b.ensureTopicReader(topic); err != nil {
		return MessageRecord{}, err
	}
	rec, err := b.store.append(topic, appID, payload, headers, "local_publish")
	if err != nil {
		return MessageRecord{}, err
	}
	b.fanout(rec)
	if err := b.pubsub.Publish(topic, payload); err != nil {
		return MessageRecord{}, err
	}
	return rec, nil
}

func (b *broker) subscribe(appID string, topics []string, fromOffset int64) (string, error) {
	if appID == "" {
		return "", errors.New("app_id required")
	}
	if len(topics) == 0 {
		return "", errors.New("topics required")
	}
	s := &subscription{
		id:     fmt.Sprintf("sub-%d", time.Now().UnixNano()),
		appID:  appID,
		topics: make(map[string]struct{}, len(topics)),
		queue:  make(chan MessageRecord, 512),
	}
	for _, topic := range topics {
		if err := validateTopic(appID, topic); err != nil {
			return "", err
		}
		if err := b.ensureTopicReader(topic); err != nil {
			return "", err
		}
		s.topics[topic] = struct{}{}
		history := b.store.list(topic, fromOffset, 200)
		for _, rec := range history {
			s.queue <- rec
		}
	}
	b.mu.Lock()
	b.subs[s.id] = s
	b.mu.Unlock()
	return s.id, nil
}

func (b *broker) pull(appID, subscriptionID string, maxItems int, wait time.Duration) ([]MessageRecord, error) {
	sub, err := b.getSub(appID, subscriptionID)
	if err != nil {
		return nil, err
	}
	if maxItems <= 0 {
		maxItems = 50
	}
	if maxItems > 500 {
		maxItems = 500
	}

	items := make([]MessageRecord, 0, maxItems)
	if wait > 0 {
		select {
		case msg := <-sub.queue:
			items = append(items, msg)
		case <-time.After(wait):
			return items, nil
		}
	}
	for len(items) < maxItems {
		select {
		case msg := <-sub.queue:
			items = append(items, msg)
		default:
			return items, nil
		}
	}
	return items, nil
}

func (b *broker) ack(appID, subscriptionID, topic string, offset int64) error {
	if err := validateTopic(appID, topic); err != nil {
		return err
	}
	if _, err := b.getSub(appID, subscriptionID); err != nil {
		return err
	}
	if offset <= 0 {
		return errors.New("offset must be > 0")
	}
	return b.store.saveCursor(appID, subscriptionID, topic, offset)
}

func (b *broker) fetchHistory(appID, topic string, fromOffset int64, limit int) ([]MessageRecord, error) {
	if err := validateTopic(appID, topic); err != nil {
		return nil, err
	}
	if err := b.ensureTopicReader(topic); err != nil {
		return nil, err
	}
	return b.store.list(topic, fromOffset, limit), nil
}

func (b *broker) getStatus() NodeStatus {
	if b.statusFn == nil {
		return NodeStatus{}
	}
	return b.statusFn()
}

func (b *broker) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, cancel := range b.topicCancel {
		cancel()
	}
	b.topicCancel = make(map[string]func())
	for id, sub := range b.subs {
		close(sub.queue)
		delete(b.subs, id)
	}
}

func (b *broker) getSub(appID, subscriptionID string) (*subscription, error) {
	b.mu.RLock()
	sub, ok := b.subs[subscriptionID]
	b.mu.RUnlock()
	if !ok || sub.appID != appID {
		return nil, ErrSubscriptionGone
	}
	return sub, nil
}

func (b *broker) ensureTopicReader(topic string) error {
	b.mu.Lock()
	if _, ok := b.topicCancel[topic]; ok {
		b.mu.Unlock()
		return nil
	}
	ch, cancel, err := b.pubsub.Subscribe(topic)
	if err != nil {
		b.mu.Unlock()
		return err
	}
	b.topicCancel[topic] = cancel
	b.mu.Unlock()

	go func() {
		for msg := range ch {
			rec, err := b.store.append(topic, "", msg.Payload, nil, "network")
			if err != nil {
				continue
			}
			b.fanout(rec)
		}
	}()
	return nil
}

func (b *broker) fanout(rec MessageRecord) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, sub := range b.subs {
		if _, ok := sub.topics[rec.Topic]; !ok {
			continue
		}
		select {
		case sub.queue <- rec:
		default:
		}
	}
}

func validateTopic(appID, topic string) error {
	if appID == "" {
		return errors.New("app_id required")
	}
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return errors.New("topic required")
	}
	prefix := "app." + appID
	if topic == prefix || strings.HasPrefix(topic, prefix+".") {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrForbiddenTopic, topic)
}
