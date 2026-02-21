package localrpc

import (
	"fmt"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"time"

	"ClawdCity/internal/core/network"
)

type Config struct {
	SocketPath  string
	RecordsPath string
	CursorPath  string
}

type Server struct {
	cfg    Config
	ln     net.Listener
	rpcSrv *rpc.Server
	broker *broker
}

type API struct {
	b *broker
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

type PullArgs struct {
	AppID          string
	SubscriptionID string
	MaxItems       int
	WaitMillis     int
}

type PullReply struct {
	Messages []MessageRecord
	Error    string
}

type AckArgs struct {
	AppID          string
	SubscriptionID string
	Topic          string
	Offset         int64
}

type AckReply struct {
	OK    bool
	Error string
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
	Transport      string
	PeerID         string
	ConnectedPeers int
	Error          string
}

func NewServer(cfg Config, pubsub network.PubSub, statusFn statusProvider) (*Server, error) {
	store, err := newHistoryStore(cfg.RecordsPath, cfg.CursorPath)
	if err != nil {
		return nil, fmt.Errorf("init rpc store: %w", err)
	}
	b := newBroker(pubsub, store, statusFn)
	r := rpc.NewServer()
	if err := r.RegisterName("P2P", &API{b: b}); err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, rpcSrv: r, broker: b}, nil
}

func (s *Server) Start() error {
	if err := os.MkdirAll(filepath.Dir(s.cfg.SocketPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(s.cfg.SocketPath)
	ln, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return err
	}
	s.ln = ln
	if err := os.Chmod(s.cfg.SocketPath, 0o600); err != nil {
		return err
	}
	go s.acceptLoop()
	return nil
}

func (s *Server) Close() error {
	s.broker.close()
	if s.ln != nil {
		_ = s.ln.Close()
	}
	_ = os.Remove(s.cfg.SocketPath)
	return nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.rpcSrv.ServeConn(conn)
	}
}

func (a *API) Publish(args PublishArgs, reply *PublishReply) error {
	rec, err := a.b.publish(args.AppID, args.Topic, args.Payload, args.Headers)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.MessageID = rec.ID
	reply.Offset = rec.Offset
	reply.Accepted = true
	return nil
}

func (a *API) Subscribe(args SubscribeArgs, reply *SubscribeReply) error {
	subID, err := a.b.subscribe(args.AppID, args.Topics, args.FromOffset)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.SubscriptionID = subID
	return nil
}

func (a *API) Pull(args PullArgs, reply *PullReply) error {
	wait := time.Duration(args.WaitMillis) * time.Millisecond
	if wait > 30*time.Second {
		wait = 30 * time.Second
	}
	items, err := a.b.pull(args.AppID, args.SubscriptionID, args.MaxItems, wait)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Messages = items
	return nil
}

func (a *API) Ack(args AckArgs, reply *AckReply) error {
	if err := a.b.ack(args.AppID, args.SubscriptionID, args.Topic, args.Offset); err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.OK = true
	return nil
}

func (a *API) FetchHistory(args HistoryArgs, reply *HistoryReply) error {
	items, err := a.b.fetchHistory(args.AppID, args.Topic, args.FromOffset, args.Limit)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	reply.Messages = items
	return nil
}

func (a *API) GetStatus(_ StatusArgs, reply *StatusReply) error {
	st := a.b.getStatus()
	reply.Transport = st.Transport
	reply.PeerID = st.PeerID
	reply.ConnectedPeers = st.ConnectedPeers
	return nil
}
