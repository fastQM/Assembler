package localrpc

import (
	"encoding/json"
	"errors"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"Assembler/internal/core/network"
)

func TestPublishSubscribeAndHistory(t *testing.T) {
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

	got, ok, err := b.next("demo", subReply.SubscriptionID, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("broker next: %v", err)
	}
	if !ok {
		t.Fatalf("expected streamed message")
	}
	if string(got.Payload) != "hello" {
		t.Fatalf("unexpected payload: %q", string(got.Payload))
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

func TestStreamPushDeliversMessage(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("/tmp", "assembler-localrpc-stream-*")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "p2p.sock")
	srv, err := NewServer(Config{
		SocketPath:  socket,
		RecordsPath: filepath.Join(dir, "messages.jsonl"),
		CursorPath:  filepath.Join(dir, "cursors.json"),
	}, network.NewMemoryPubSub(), nil)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := srv.Start(); err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			t.Skipf("skipping stream socket test in restricted environment: %v", err)
		}
		t.Fatalf("start server: %v", err)
	}
	defer srv.Close()

	streamConn, err := net.Dial("unix", socket+".stream")
	if err != nil {
		t.Fatalf("dial stream: %v", err)
	}
	defer streamConn.Close()

	enc := json.NewEncoder(streamConn)
	dec := json.NewDecoder(streamConn)
	if err := enc.Encode(streamSubscribeRequest{
		AppID:      "demo",
		Topics:     []string{"app.demo.chat"},
		FromOffset: 0,
	}); err != nil {
		t.Fatalf("encode subscribe: %v", err)
	}
	var ready streamEvent
	if err := dec.Decode(&ready); err != nil {
		t.Fatalf("decode ready: %v", err)
	}
	if ready.Type != "ready" {
		t.Fatalf("expected ready, got %q (%s)", ready.Type, ready.Error)
	}

	rpcConn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatalf("dial rpc: %v", err)
	}
	defer rpcConn.Close()
	client := rpc.NewClient(rpcConn)
	defer client.Close()

	var pubReply PublishReply
	if err := client.Call("P2P.Publish", PublishArgs{
		AppID:   "demo",
		Topic:   "app.demo.chat",
		Payload: []byte("hello-stream"),
	}, &pubReply); err != nil {
		t.Fatalf("rpc publish: %v", err)
	}
	if !pubReply.Accepted {
		t.Fatalf("publish rejected: %s", pubReply.Error)
	}

	_ = streamConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var evt streamEvent
		if err := dec.Decode(&evt); err != nil {
			t.Fatalf("decode stream event: %v", err)
		}
		if evt.Type != "message" {
			continue
		}
		if string(evt.Message.Payload) != "hello-stream" {
			t.Fatalf("unexpected payload: %q", string(evt.Message.Payload))
		}
		return
	}
}
