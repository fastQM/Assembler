package network

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	coreNetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	coreProtocol "github.com/libp2p/go-libp2p/core/protocol"
	mdns "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	routing "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	discoveryutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	ma "github.com/multiformats/go-multiaddr"
)

// Libp2pOptions configures the libp2p transport.
type Libp2pOptions struct {
	ListenAddrs              []string
	Bootstrap                []string
	Rendezvous               string
	EnableMDNS               bool
	EnableKadDHT             bool
	KadDiscoveryApps         []string
	KadDiscoveryInterval     time.Duration
	KadDiscoveryQueryTimeout time.Duration
	IdentityKeyFile          string
}

// Libp2pPubSub provides gossip-based pubsub over libp2p.
type Libp2pPubSub struct {
	ctx    context.Context
	cancel context.CancelFunc

	host host.Host
	ps   *pubsub.PubSub
	dht  *dht.IpfsDHT

	mu                       sync.Mutex
	topics                   map[string]*pubsub.Topic
	rendezvous               string
	enableKadDHT             bool
	kadDiscoveryInterval     time.Duration
	kadDiscoveryQueryTimeout time.Duration
	discoveryAllowApps       map[string]struct{}
	discoveryStarted         map[string]struct{}
	reconnectCancel          map[peer.ID]context.CancelFunc
	notifiee                 *connNotifiee
}

func NewLibp2pPubSub(parent context.Context, opts Libp2pOptions) (*Libp2pPubSub, error) {
	ctx, cancel := context.WithCancel(parent)

	listenAddrs := make([]ma.Multiaddr, 0, len(opts.ListenAddrs))
	for _, s := range opts.ListenAddrs {
		if s == "" {
			continue
		}
		a, err := ma.NewMultiaddr(s)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("invalid listen multiaddr %q: %w", s, err)
		}
		listenAddrs = append(listenAddrs, a)
	}
	if len(listenAddrs) == 0 {
		a, _ := ma.NewMultiaddr("/ip4/0.0.0.0/tcp/0")
		listenAddrs = append(listenAddrs, a)
	}

	libp2pOpts := []libp2p.Option{libp2p.ListenAddrs(listenAddrs...)}
	if opts.IdentityKeyFile != "" {
		key, err := loadOrCreateIdentityKey(opts.IdentityKeyFile)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("load identity key: %w", err)
		}
		libp2pOpts = append(libp2pOpts, libp2p.Identity(key))
	}

	h, err := libp2p.New(libp2pOpts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create host: %w", err)
	}

	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		_ = h.Close()
		cancel()
		return nil, fmt.Errorf("create gossipsub: %w", err)
	}

	p := &Libp2pPubSub{
		ctx:                      ctx,
		cancel:                   cancel,
		host:                     h,
		ps:                       ps,
		topics:                   make(map[string]*pubsub.Topic),
		rendezvous:               strings.TrimSpace(opts.Rendezvous),
		enableKadDHT:             opts.EnableKadDHT,
		kadDiscoveryInterval:     opts.KadDiscoveryInterval,
		kadDiscoveryQueryTimeout: opts.KadDiscoveryQueryTimeout,
		discoveryAllowApps:       make(map[string]struct{}),
		discoveryStarted:         make(map[string]struct{}),
		reconnectCancel:          make(map[peer.ID]context.CancelFunc),
	}
	p.notifiee = &connNotifiee{p: p}
	h.Network().Notify(p.notifiee)
	for _, appID := range opts.KadDiscoveryApps {
		appID = strings.TrimSpace(appID)
		if appID == "" {
			continue
		}
		p.discoveryAllowApps[appID] = struct{}{}
	}

	if opts.EnableMDNS {
		service := mdns.NewMdnsService(h, opts.Rendezvous, &mdnsNotifee{host: h})
		if err := service.Start(); err != nil {
			log.Printf("mdns start error: %v", err)
		}
	}

	for _, raw := range opts.Bootstrap {
		if raw == "" {
			continue
		}
		addr, err := ma.NewMultiaddr(raw)
		if err != nil {
			log.Printf("skip bootstrap addr %q: %v", raw, err)
			continue
		}
		info, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			log.Printf("skip bootstrap addr %q: %v", raw, err)
			continue
		}
		if err := h.Connect(ctx, *info); err != nil {
			log.Printf("bootstrap connect failed %s: %v", info.ID, err)
		} else {
			log.Printf("connected bootstrap peer %s", info.ID)
		}
	}

	if opts.EnableKadDHT {
		if opts.KadDiscoveryInterval <= 0 {
			opts.KadDiscoveryInterval = 20 * time.Second
		}
		if opts.KadDiscoveryQueryTimeout <= 0 {
			opts.KadDiscoveryQueryTimeout = 10 * time.Second
		}
		p.kadDiscoveryInterval = opts.KadDiscoveryInterval
		p.kadDiscoveryQueryTimeout = opts.KadDiscoveryQueryTimeout
		kad, err := dht.New(ctx, h, dht.Mode(dht.ModeAuto))
		if err != nil {
			log.Printf("kaddht init failed: %v", err)
		} else {
			p.dht = kad
			if err := kad.Bootstrap(ctx); err != nil {
				log.Printf("kaddht bootstrap failed: %v", err)
			} else {
				log.Printf("kaddht bootstrap ready")
			}
			for appID := range p.discoveryAllowApps {
				if err := p.EnsureAppDiscovery(appID); err != nil {
					log.Printf("kaddht app discovery init failed app=%s: %v", appID, err)
				}
			}
		}
	}

	return p, nil
}

func (p *Libp2pPubSub) EnsureAppDiscovery(appID string) error {
	if !p.enableKadDHT || p.dht == nil {
		return nil
	}
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil
	}
	if len(p.discoveryAllowApps) > 0 {
		if _, ok := p.discoveryAllowApps[appID]; !ok {
			return nil
		}
	}
	namespace := p.discoveryNamespace(appID)
	p.mu.Lock()
	if _, ok := p.discoveryStarted[namespace]; ok {
		p.mu.Unlock()
		return nil
	}
	p.discoveryStarted[namespace] = struct{}{}
	p.mu.Unlock()

	rd := routing.NewRoutingDiscovery(p.dht)
	go p.advertiseLoop(rd, namespace)
	go p.findPeersLoop(rd, namespace, p.kadDiscoveryInterval, p.kadDiscoveryQueryTimeout)
	log.Printf("kaddht discovery enabled for app=%s namespace=%s", appID, namespace)
	return nil
}

func (p *Libp2pPubSub) Publish(topic string, payload []byte) error {
	t, err := p.getOrJoinTopic(topic)
	if err != nil {
		return err
	}
	return t.Publish(p.ctx, payload)
}

func (p *Libp2pPubSub) Subscribe(topic string) (<-chan Message, func(), error) {
	t, err := p.getOrJoinTopic(topic)
	if err != nil {
		return nil, nil, err
	}
	sub, err := t.Subscribe()
	if err != nil {
		return nil, nil, err
	}

	out := make(chan Message, 64)
	subCtx, subCancel := context.WithCancel(p.ctx)
	go func() {
		defer close(out)
		for {
			msg, err := sub.Next(subCtx)
			if err != nil {
				return
			}
			select {
			case out <- Message{Topic: topic, Payload: append([]byte(nil), msg.Data...)}:
			default:
			}
		}
	}()

	cancel := func() {
		subCancel()
		sub.Cancel()
	}
	return out, cancel, nil
}

func (p *Libp2pPubSub) Close() error {
	p.cancel()
	if p.dht != nil {
		_ = p.dht.Close()
	}
	if p.notifiee != nil {
		p.host.Network().StopNotify(p.notifiee)
	}
	p.mu.Lock()
	for _, cancel := range p.reconnectCancel {
		cancel()
	}
	p.reconnectCancel = make(map[peer.ID]context.CancelFunc)
	defer p.mu.Unlock()
	for _, t := range p.topics {
		_ = t.Close()
	}
	return p.host.Close()
}

func (p *Libp2pPubSub) PeerID() string {
	return p.host.ID().String()
}

func (p *Libp2pPubSub) ListenAddrs() []string {
	out := make([]string, 0, len(p.host.Addrs()))
	for _, addr := range p.host.Addrs() {
		out = append(out, fmt.Sprintf("%s/p2p/%s", addr.String(), p.host.ID().String()))
	}
	return out
}

func (p *Libp2pPubSub) ConnectedPeers() []string {
	peers := p.host.Network().Peers()
	out := make([]string, 0, len(peers))
	for _, pid := range peers {
		out = append(out, pid.String())
	}
	return out
}

func (p *Libp2pPubSub) ConnectedPeerAddrs() []string {
	peers := p.host.Network().Peers()
	seen := make(map[string]struct{}, 16)
	out := make([]string, 0, len(peers))
	for _, pid := range peers {
		for _, addr := range p.host.Peerstore().Addrs(pid) {
			full := fmt.Sprintf("%s/p2p/%s", addr.String(), pid.String())
			if _, ok := seen[full]; ok {
				continue
			}
			seen[full] = struct{}{}
			out = append(out, full)
		}
	}
	return out
}

func (p *Libp2pPubSub) getOrJoinTopic(name string) (*pubsub.Topic, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.topics[name]; ok {
		return t, nil
	}
	t, err := p.ps.Join(name)
	if err != nil {
		return nil, err
	}
	p.topics[name] = t
	return t, nil
}

func (p *Libp2pPubSub) SendDirect(ctx context.Context, peerID string, protocol string, payload []byte) error {
	pid, err := peer.Decode(peerID)
	if err != nil {
		return fmt.Errorf("decode peer id: %w", err)
	}
	s, err := p.host.NewStream(ctx, pid, coreProtocol.ID(protocol))
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	defer s.Close()
	if _, err := s.Write(payload); err != nil {
		return fmt.Errorf("stream write: %w", err)
	}
	return nil
}

func (p *Libp2pPubSub) RegisterDirectHandler(protocol string, fn func(peerID string, payload []byte)) {
	if fn == nil {
		return
	}
	p.host.SetStreamHandler(coreProtocol.ID(protocol), func(s coreNetwork.Stream) {
		defer s.Close()
		payload, err := io.ReadAll(io.LimitReader(s, 4<<20))
		if err != nil {
			return
		}
		fn(s.Conn().RemotePeer().String(), payload)
	})
}

func (p *Libp2pPubSub) advertiseLoop(rd discovery.Discovery, rendezvous string) {
	if strings.TrimSpace(rendezvous) == "" {
		return
	}
	discoveryutil.Advertise(p.ctx, rd, rendezvous)
	<-p.ctx.Done()
}

func (p *Libp2pPubSub) findPeersLoop(rd discovery.Discovery, rendezvous string, every, queryTimeout time.Duration) {
	if strings.TrimSpace(rendezvous) == "" {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	run := func() {
		ctx, cancel := context.WithTimeout(p.ctx, queryTimeout)
		defer cancel()
		peerCh, err := rd.FindPeers(ctx, rendezvous)
		if err != nil {
			log.Printf("kaddht find peers error: %v", err)
			return
		}
		for info := range peerCh {
			if info.ID == "" || info.ID == p.host.ID() {
				continue
			}
			if err := p.host.Connect(p.ctx, info); err != nil {
				continue
			}
		}
	}
	run()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

func (p *Libp2pPubSub) discoveryNamespace(appID string) string {
	base := strings.TrimSpace(p.rendezvous)
	if base == "" {
		base = "assembler"
	}
	return base + ".app." + appID
}

func (p *Libp2pPubSub) startReconnect(pid peer.ID) {
	if pid == "" || pid == p.host.ID() {
		return
	}
	if len(p.host.Network().ConnsToPeer(pid)) > 0 {
		return
	}

	p.mu.Lock()
	if _, exists := p.reconnectCancel[pid]; exists {
		p.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Minute)
	p.reconnectCancel[pid] = cancel
	p.mu.Unlock()

	log.Printf("peer disconnected %s; start reconnect every 30s for up to 5m", pid)

	go func() {
		defer func() {
			p.mu.Lock()
			delete(p.reconnectCancel, pid)
			p.mu.Unlock()
			cancel()
		}()

		tryConnect := func() bool {
			if p.ctx.Err() != nil || ctx.Err() != nil {
				return true
			}
			if len(p.host.Network().ConnsToPeer(pid)) > 0 {
				return true
			}
			addrs := p.host.Peerstore().Addrs(pid)
			if len(addrs) == 0 {
				return false
			}
			info := peer.AddrInfo{ID: pid, Addrs: addrs}
			attemptCtx, attemptCancel := context.WithTimeout(ctx, 10*time.Second)
			defer attemptCancel()
			if err := p.host.Connect(attemptCtx, info); err != nil {
				log.Printf("reconnect failed %s: %v", pid, err)
				return false
			}
			log.Printf("reconnect success %s", pid)
			return true
		}

		if tryConnect() {
			return
		}

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-p.ctx.Done():
				return
			case <-ctx.Done():
				log.Printf("reconnect give up %s after 5m", pid)
				return
			case <-ticker.C:
				if tryConnect() {
					return
				}
			}
		}
	}()
}

func (p *Libp2pPubSub) stopReconnect(pid peer.ID) {
	p.mu.Lock()
	cancel, ok := p.reconnectCancel[pid]
	if ok {
		delete(p.reconnectCancel, pid)
	}
	p.mu.Unlock()
	if ok {
		cancel()
	}
}

type connNotifiee struct {
	p *Libp2pPubSub
}

func (n *connNotifiee) Listen(coreNetwork.Network, ma.Multiaddr)      {}
func (n *connNotifiee) ListenClose(coreNetwork.Network, ma.Multiaddr) {}
func (n *connNotifiee) OpenedStream(coreNetwork.Network, coreNetwork.Stream) {
}
func (n *connNotifiee) ClosedStream(coreNetwork.Network, coreNetwork.Stream) {
}

func (n *connNotifiee) Connected(_ coreNetwork.Network, c coreNetwork.Conn) {
	n.p.stopReconnect(c.RemotePeer())
}

func (n *connNotifiee) Disconnected(_ coreNetwork.Network, c coreNetwork.Conn) {
	pid := c.RemotePeer()
	if pid == "" || pid == n.p.host.ID() {
		return
	}
	if len(n.p.host.Network().ConnsToPeer(pid)) > 0 {
		return
	}
	n.p.startReconnect(pid)
}

type mdnsNotifee struct {
	host host.Host
}

func (n *mdnsNotifee) HandlePeerFound(info peer.AddrInfo) {
	if err := n.host.Connect(context.Background(), info); err != nil {
		log.Printf("mdns connect failed %s: %v", info.ID, err)
	}
}

func loadOrCreateIdentityKey(path string) (crypto.PrivKey, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		key, err := crypto.UnmarshalPrivateKey(b)
		if err != nil {
			return nil, fmt.Errorf("unmarshal private key: %w", err)
		}
		return key, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir key dir: %w", err)
	}
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	raw, err := crypto.MarshalPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}
	return key, nil
}
