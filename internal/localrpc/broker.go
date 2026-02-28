package localrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"Assembler/internal/core/network"
)

var (
	ErrForbiddenTopic   = errors.New("topic is not allowed for app")
	ErrSubscriptionGone = errors.New("subscription not found")
)

type NodeStatus struct {
	Transport           string
	PeerID              string
	ConnectedPeers      int
	ListenAddrs         []string
	ConnectedPeerIDs    []string
	ConnectedPeerAddrs  []string
	StartedAt           time.Time
	ActiveSubscriptions int
	MessagesPublished   int64
	MessagesInNetwork   int64
	MessagesInStream    int64
	MessagesFanout      int64
	DirectSends         int64
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
	direct      network.DirectMessenger
	store       *historyStore
	subs        map[string]*subscription
	topicCancel map[string]func()
	statusFn    statusProvider

	publishedLocal int64
	inNetwork      int64
	inStream       int64
	fanoutCount    int64
	directSent     int64
}

const directProtocol = "/assembler/localrpc/direct/1.0.0"

type appDiscoveryEnabler interface {
	EnsureAppDiscovery(appID string) error
}

type directWire struct {
	Topic   string `json:"topic"`
	Payload []byte `json:"payload"`
}

func newBroker(pubsub network.PubSub, store *historyStore, statusFn statusProvider) *broker {
	b := &broker{
		pubsub:      pubsub,
		store:       store,
		subs:        make(map[string]*subscription),
		topicCancel: make(map[string]func()),
		statusFn:    statusFn,
	}
	if d, ok := pubsub.(network.DirectMessenger); ok {
		b.direct = d
		d.RegisterDirectHandler(directProtocol, b.handleDirectInbound)
	}
	return b
}

func (b *broker) publish(appID, topic string, payload []byte, headers map[string]string) (MessageRecord, error) {
	if err := validateTopic(appID, topic); err != nil {
		return MessageRecord{}, err
	}
	if err := b.ensureTopicReader(appID, topic); err != nil {
		return MessageRecord{}, err
	}
	rec, err := b.store.append(topic, appID, payload, headers, "local_publish")
	if err != nil {
		return MessageRecord{}, err
	}
	atomic.AddInt64(&b.publishedLocal, 1)
	b.fanout(rec)
	if err := b.pubsub.Publish(topic, payload); err != nil {
		return MessageRecord{}, err
	}
	return rec, nil
}

func (b *broker) subscribe(appID string, topics []string, fromOffset int64) (string, error) {
	return b.subscribeWithMode(appID, topics, fromOffset, true)
}

func (b *broker) subscribeWithMode(appID string, topics []string, fromOffset int64, includeHistory bool) (string, error) {
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
		if err := b.ensureTopicReader(appID, topic); err != nil {
			return "", err
		}
		s.topics[topic] = struct{}{}
		if includeHistory {
			history := b.store.list(topic, fromOffset, 200)
			for _, rec := range history {
				s.queue <- rec
			}
		}
	}
	b.mu.Lock()
	b.subs[s.id] = s
	b.mu.Unlock()
	return s.id, nil
}

func (b *broker) next(appID, subscriptionID string, wait time.Duration) (MessageRecord, bool, error) {
	sub, err := b.getSub(appID, subscriptionID)
	if err != nil {
		return MessageRecord{}, false, err
	}
	if wait <= 0 {
		select {
		case msg, ok := <-sub.queue:
			if !ok {
				return MessageRecord{}, false, ErrSubscriptionGone
			}
			return msg, true, nil
		default:
			return MessageRecord{}, false, nil
		}
	}
	select {
	case msg, ok := <-sub.queue:
		if !ok {
			return MessageRecord{}, false, ErrSubscriptionGone
		}
		return msg, true, nil
	case <-time.After(wait):
		return MessageRecord{}, false, nil
	}
}

func (b *broker) fetchHistory(appID, topic string, fromOffset int64, limit int) ([]MessageRecord, error) {
	if err := validateTopic(appID, topic); err != nil {
		return nil, err
	}
	if err := b.ensureTopicReader(appID, topic); err != nil {
		return nil, err
	}
	return b.store.list(topic, fromOffset, limit), nil
}

func (b *broker) sendDirect(appID, peerID, topic string, payload []byte) error {
	if err := validateTopic(appID, topic); err != nil {
		return err
	}
	if strings.TrimSpace(peerID) == "" {
		return errors.New("peer_id required")
	}
	if b.direct == nil {
		return errors.New("direct stream is unavailable on current transport")
	}
	wire, err := json.Marshal(directWire{Topic: topic, Payload: payload})
	if err != nil {
		return err
	}
	if err := b.direct.SendDirect(context.Background(), peerID, directProtocol, wire); err != nil {
		return err
	}
	atomic.AddInt64(&b.directSent, 1)
	return nil
}

func (b *broker) getStatus() NodeStatus {
	var st NodeStatus
	if b.statusFn != nil {
		st = b.statusFn()
	}
	b.mu.RLock()
	st.ActiveSubscriptions = len(b.subs)
	b.mu.RUnlock()
	st.MessagesPublished = atomic.LoadInt64(&b.publishedLocal)
	st.MessagesInNetwork = atomic.LoadInt64(&b.inNetwork)
	st.MessagesInStream = atomic.LoadInt64(&b.inStream)
	st.MessagesFanout = atomic.LoadInt64(&b.fanoutCount)
	st.DirectSends = atomic.LoadInt64(&b.directSent)
	return st
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

func (b *broker) unsubscribe(appID, subscriptionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sub, ok := b.subs[subscriptionID]
	if !ok || sub.appID != appID {
		return
	}
	delete(b.subs, subscriptionID)
	close(sub.queue)
}

func (b *broker) ensureTopicReader(appID, topic string) error {
	if d, ok := b.pubsub.(appDiscoveryEnabler); ok {
		if err := d.EnsureAppDiscovery(appID); err != nil {
			return err
		}
	}
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
			atomic.AddInt64(&b.inNetwork, 1)
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
			atomic.AddInt64(&b.fanoutCount, 1)
		default:
		}
	}
}

func (b *broker) handleDirectInbound(_ string, payload []byte) {
	var wire directWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return
	}
	if strings.TrimSpace(wire.Topic) == "" {
		return
	}
	rec, err := b.store.append(wire.Topic, "", wire.Payload, nil, "stream")
	if err != nil {
		return
	}
	atomic.AddInt64(&b.inStream, 1)
	b.fanout(rec)
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
