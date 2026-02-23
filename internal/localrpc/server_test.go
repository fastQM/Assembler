package localrpc

import (
	"path/filepath"
	"testing"

	"Assembler/internal/core/network"
)

func TestPublishSubscribeHistoryAndAck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := newHistoryStore(filepath.Join(dir, "messages.jsonl"), filepath.Join(dir, "cursors.json"))
	if err != nil {
		t.Fatalf("new history store: %v", err)
	}
	b := newBroker(network.NewMemoryPubSub(), store, nil)
	api := &API{b: b}

	var subReply SubscribeReply
	if err := api.Subscribe(SubscribeArgs{AppID: "demo", Topics: []string{"app.demo.chat"}}, &subReply); err != nil {
		t.Fatalf("subscribe rpc: %v", err)
	}
	if subReply.Error != "" {
		t.Fatalf("subscribe failed: %s", subReply.Error)
	}

	var pubReply PublishReply
	if err := api.Publish(PublishArgs{AppID: "demo", Topic: "app.demo.chat", Payload: []byte("hello")}, &pubReply); err != nil {
		t.Fatalf("publish rpc: %v", err)
	}
	if !pubReply.Accepted {
		t.Fatalf("publish rejected: %s", pubReply.Error)
	}

	var pullReply PullReply
	if err := api.Pull(PullArgs{AppID: "demo", SubscriptionID: subReply.SubscriptionID, MaxItems: 10, WaitMillis: 50}, &pullReply); err != nil {
		t.Fatalf("pull rpc: %v", err)
	}
	if pullReply.Error != "" {
		t.Fatalf("pull failed: %s", pullReply.Error)
	}
	if len(pullReply.Messages) == 0 {
		t.Fatalf("expected at least one message")
	}

	first := pullReply.Messages[0]
	if string(first.Payload) != "hello" {
		t.Fatalf("unexpected payload: %q", string(first.Payload))
	}

	var ackReply AckReply
	if err := api.Ack(AckArgs{
		AppID:          "demo",
		SubscriptionID: subReply.SubscriptionID,
		Topic:          "app.demo.chat",
		Offset:         first.Offset,
	}, &ackReply); err != nil {
		t.Fatalf("ack rpc: %v", err)
	}
	if !ackReply.OK {
		t.Fatalf("ack failed: %s", ackReply.Error)
	}

	var histReply HistoryReply
	if err := api.FetchHistory(HistoryArgs{AppID: "demo", Topic: "app.demo.chat", FromOffset: 0, Limit: 10}, &histReply); err != nil {
		t.Fatalf("history rpc: %v", err)
	}
	if histReply.Error != "" {
		t.Fatalf("history failed: %s", histReply.Error)
	}
	if len(histReply.Messages) == 0 {
		t.Fatalf("expected history")
	}
}

func TestTopicAcl(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := newHistoryStore(filepath.Join(dir, "messages.jsonl"), filepath.Join(dir, "cursors.json"))
	if err != nil {
		t.Fatalf("new history store: %v", err)
	}
	b := newBroker(network.NewMemoryPubSub(), store, nil)
	api := &API{b: b}

	var reply PublishReply
	if err := api.Publish(PublishArgs{AppID: "demo", Topic: "app.other.chat", Payload: []byte("x")}, &reply); err != nil {
		t.Fatalf("publish rpc: %v", err)
	}
	if reply.Error == "" {
		t.Fatalf("expected acl error")
	}
}
