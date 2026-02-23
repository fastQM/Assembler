package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"Assembler/internal/core/network"
	"Assembler/internal/localrpc"
)

func main() {
	transport := flag.String("transport", "libp2p", "transport: memory|libp2p")
	p2pListen := flag.String("p2p-listen", "/ip4/0.0.0.0/tcp/0", "comma-separated libp2p listen multiaddrs")
	p2pBootstrap := flag.String("p2p-bootstrap", "/ip4/3.65.204.231/tcp/40001/p2p/12D3KooWAaYG182TYGF5GTfWu5CZpiWbf5r6GJwfuSsYRsErA5YL", "comma-separated bootstrap peer multiaddrs")
	p2pRendezvous := flag.String("p2p-rendezvous", "Assembler", "libp2p mDNS rendezvous string")
	p2pMDNS := flag.Bool("p2p-mdns", true, "enable libp2p mDNS discovery")
	p2pKadDHT := flag.Bool("p2p-kad-dht", true, "enable libp2p kademlia dht discovery")
	p2pKadApps := flag.String("p2p-kad-apps", "social", "comma-separated app ids allowed to use kademlia discovery (empty means all apps)")
	p2pKadDiscoveryEvery := flag.Duration("p2p-kad-discovery-interval", 20*time.Second, "kademlia discovery interval")
	p2pKadQueryTimeout := flag.Duration("p2p-kad-query-timeout", 10*time.Second, "kademlia discovery query timeout")
	p2pIdentityKey := flag.String("p2p-identity-key", filepath.Join("data", "p2p_identity.key"), "libp2p private key path for stable peer id")
	p2pRecentPeers := flag.String("p2p-recent-peers", filepath.Join("data", "recent_peers.json"), "file path to persist recently connected peers")
	localRPCEnable := flag.Bool("local-rpc-enable", true, "enable local unix-socket RPC for app p2p access")
	localRPCSock := flag.String("local-rpc-sock", filepath.Join("data", "assembler-p2p.sock"), "local rpc unix socket path")
	localRPCRecords := flag.String("local-rpc-records", filepath.Join("data", "p2p_messages.jsonl"), "local rpc message store path")
	localRPCCursors := flag.String("local-rpc-cursors", filepath.Join("data", "p2p_cursors.json"), "local rpc cursor store path")
	flag.Parse()

	startedAt := time.Now().UTC()
	var (
		pubsub network.PubSub
		closer func()
	)
	switch *transport {
	case "memory":
		pubsub = network.NewMemoryPubSub()
	case "libp2p":
		bootstrap := splitCSV(*p2pBootstrap)
		savedPeers := loadRecentPeers(*p2pRecentPeers)
		bootstrap = mergeUnique(bootstrap, savedPeers)

		lp2p, err := network.NewLibp2pPubSub(context.Background(), network.Libp2pOptions{
			ListenAddrs:              splitCSV(*p2pListen),
			Bootstrap:                bootstrap,
			Rendezvous:               *p2pRendezvous,
			EnableMDNS:               *p2pMDNS,
			EnableKadDHT:             *p2pKadDHT,
			KadDiscoveryApps:         splitCSV(*p2pKadApps),
			KadDiscoveryInterval:     *p2pKadDiscoveryEvery,
			KadDiscoveryQueryTimeout: *p2pKadQueryTimeout,
			IdentityKeyFile:          *p2pIdentityKey,
		})
		if err != nil {
			log.Fatal(err)
		}
		pubsub = lp2p
		closer = func() {
			saveRecentPeers(*p2pRecentPeers, lp2p.ConnectedPeerAddrs())
			_ = lp2p.Close()
		}
		log.Printf("libp2p peer id: %s", lp2p.PeerID())
		for _, a := range lp2p.ListenAddrs() {
			log.Printf("libp2p listen: %s", a)
		}
		if len(savedPeers) > 0 {
			log.Printf("loaded %d recent peer seed(s)", len(savedPeers))
		}
		go persistPeersLoop(*p2pRecentPeers, lp2p)
	default:
		log.Fatalf("unsupported transport: %s", *transport)
	}
	if closer != nil {
		defer closer()
	}
	var lp2pRef *network.Libp2pPubSub
	if v, ok := pubsub.(*network.Libp2pPubSub); ok {
		lp2pRef = v
	}

	var rpcServer *localrpc.Server
	if *localRPCEnable {
		var err error
		rpcServer, err = localrpc.NewServer(localrpc.Config{
			SocketPath:  *localRPCSock,
			RecordsPath: *localRPCRecords,
			CursorPath:  *localRPCCursors,
		}, pubsub, func() localrpc.NodeStatus {
			st := localrpc.NodeStatus{
				Transport: *transport,
				StartedAt: startedAt,
			}
			if lp2pRef != nil {
				st.PeerID = lp2pRef.PeerID()
				st.ListenAddrs = lp2pRef.ListenAddrs()
				st.ConnectedPeerIDs = lp2pRef.ConnectedPeers()
				st.ConnectedPeerAddrs = lp2pRef.ConnectedPeerAddrs()
				st.ConnectedPeers = len(st.ConnectedPeerIDs)
			}
			return st
		})
		if err != nil {
			log.Fatal(err)
		}
		if err := rpcServer.Start(); err != nil {
			log.Fatal(err)
		}
		defer func() { _ = rpcServer.Close() }()
		log.Printf("local rpc enabled at unix://%s", *localRPCSock)
	}

	log.Printf("assemblerd running (rpc only, no http)")
	waitForSignal()
}

func waitForSignal() {
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}

func splitCSV(in string) []string {
	if strings.TrimSpace(in) == "" {
		return nil
	}
	raw := strings.Split(in, ",")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func persistPeersLoop(path string, lp2p *network.Libp2pPubSub) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		saveRecentPeers(path, lp2p.ConnectedPeerAddrs())
	}
}

func loadRecentPeers(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return nil
	}
	var peers []string
	if err := json.Unmarshal(b, &peers); err != nil {
		log.Printf("invalid recent peers file %s: %v", path, err)
		return nil
	}
	return peers
}

func saveRecentPeers(path string, peers []string) {
	if len(peers) == 0 {
		return
	}
	peers = mergeUnique(nil, peers)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("mkdir recent peers dir failed: %v", err)
		return
	}
	b, err := json.MarshalIndent(peers, "", "  ")
	if err != nil {
		log.Printf("marshal recent peers failed: %v", err)
		return
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		log.Printf("write recent peers failed: %v", err)
	}
}

func mergeUnique(base, extra []string) []string {
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	appendOne := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range base {
		appendOne(s)
	}
	for _, s := range extra {
		appendOne(s)
	}
	return out
}
