package localrpcclient

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/rpc"
	"sync"
	"time"
)

type MessageRecord struct {
	ID        string            `json:"id"`
	Topic     string            `json:"topic"`
	AppID     string            `json:"app_id"`
	Payload   []byte            `json:"payload"`
	Headers   map[string]string `json:"headers,omitempty"`
	Source    string            `json:"source"`
	CreatedAt time.Time         `json:"created_at"`
	Offset    int64             `json:"offset"`
}

type PublishArgs struct {
	AppID   string
	Topic   string
	Payload []byte
	Headers map[string]string
}

type PublishReply struct {
	MessageID string
	Offset    int64
	Accepted  bool
	Error     string
}

type SubscribeArgs struct {
	AppID      string
	Topics     []string
	FromOffset int64
}

type SubscribeReply struct {
	SubscriptionID string
	Error          string
}

type HistoryArgs struct {
	AppID      string
	Topic      string
	FromOffset int64
	Limit      int
}

type HistoryReply struct {
	Messages []MessageRecord
	Error    string
}

type StatusArgs struct{}

type StatusReply struct {
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
	Error               string
}

type SendDirectArgs struct {
	AppID   string
	PeerID  string
	Topic   string
	Payload []byte
}

type SendDirectReply struct {
	Sent  bool
	Error string
}

type StreamEvent struct {
	Type           string        `json:"type"`
	SubscriptionID string        `json:"subscription_id,omitempty"`
	Message        MessageRecord `json:"message,omitempty"`
	Error          string        `json:"error,omitempty"`
}

type Client struct {
	socketPath string
	timeout    time.Duration
}

func New(socketPath string) *Client {
	return &Client{socketPath: socketPath, timeout: 5 * time.Second}
}

func (c *Client) call(method string, args any, reply any) error {
	conn, err := net.DialTimeout("unix", c.socketPath, c.timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	cli := rpc.NewClient(conn)
	defer cli.Close()
	return cli.Call(method, args, reply)
}

func (c *Client) Publish(args PublishArgs) (PublishReply, error) {
	var out PublishReply
	err := c.call("P2P.Publish", args, &out)
	return out, err
}

func (c *Client) Subscribe(args SubscribeArgs) (SubscribeReply, error) {
	var out SubscribeReply
	err := c.call("P2P.Subscribe", args, &out)
	return out, err
}

func (c *Client) FetchHistory(args HistoryArgs) (HistoryReply, error) {
	var out HistoryReply
	err := c.call("P2P.FetchHistory", args, &out)
	return out, err
}

func (c *Client) GetStatus() (StatusReply, error) {
	var out StatusReply
	err := c.call("P2P.GetStatus", StatusArgs{}, &out)
	return out, err
}

func (c *Client) SendDirect(args SendDirectArgs) (SendDirectReply, error) {
	var out SendDirectReply
	err := c.call("P2P.SendDirect", args, &out)
	return out, err
}

func (c *Client) Stream(ctx context.Context, args SubscribeArgs) (<-chan StreamEvent, func(), error) {
	conn, err := net.DialTimeout("unix", c.socketPath+".stream", c.timeout)
	if err != nil {
		return nil, nil, err
	}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(map[string]any{
		"app_id":      args.AppID,
		"topics":      args.Topics,
		"from_offset": args.FromOffset,
		"live_only":   true,
	}); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	events := make(chan StreamEvent, 64)
	done := make(chan struct{})
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			_ = conn.Close()
			close(done)
		})
	}

	go func() {
		defer close(events)
		defer cancel()
		dec := json.NewDecoder(bufio.NewReader(conn))
		for {
			var evt StreamEvent
			if err := dec.Decode(&evt); err != nil {
				return
			}
			select {
			case events <- evt:
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-done:
		}
	}()
	return events, cancel, nil
}
