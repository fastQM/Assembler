package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ClawdCity/internal/api"
	"ClawdCity/internal/clawdcity"
	"ClawdCity/internal/core/network"
	"ClawdCity/internal/localrpc"
)

func main() {
	addr := flag.String("addr", ":8080", "http listen address")
	transport := flag.String("transport", "memory", "transport: memory|libp2p")
	p2pListen := flag.String("p2p-listen", "/ip4/0.0.0.0/tcp/0", "comma-separated libp2p listen multiaddrs")
	p2pBootstrap := flag.String("p2p-bootstrap", "/ip4/3.65.204.231/tcp/40001/p2p/12D3KooWAaYG182TYGF5GTfWu5CZpiWbf5r6GJwfuSsYRsErA5YL", "comma-separated bootstrap peer multiaddrs")
	p2pRendezvous := flag.String("p2p-rendezvous", "ClawdCity", "libp2p mDNS rendezvous string")
	p2pMDNS := flag.Bool("p2p-mdns", true, "enable libp2p mDNS discovery")
	p2pIdentityKey := flag.String("p2p-identity-key", filepath.Join("data", "p2p_identity.key"), "libp2p private key path for stable peer id")
	p2pRecentPeers := flag.String("p2p-recent-peers", filepath.Join("data", "recent_peers.json"), "file path to persist recently connected peers")
	localRPCEnable := flag.Bool("local-rpc-enable", true, "enable local unix-socket RPC for app p2p access")
	localRPCSock := flag.String("local-rpc-sock", filepath.Join("data", "clawdcity-p2p.sock"), "local rpc unix socket path")
	localRPCRecords := flag.String("local-rpc-records", filepath.Join("data", "p2p_messages.jsonl"), "local rpc message store path")
	localRPCCursors := flag.String("local-rpc-cursors", filepath.Join("data", "p2p_cursors.json"), "local rpc cursor store path")
	flag.Parse()

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
			ListenAddrs:     splitCSV(*p2pListen),
			Bootstrap:       bootstrap,
			Rendezvous:      *p2pRendezvous,
			EnableMDNS:      *p2pMDNS,
			IdentityKeyFile: *p2pIdentityKey,
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

	if *localRPCEnable {
		rpcServer, err := localrpc.NewServer(localrpc.Config{
			SocketPath:  *localRPCSock,
			RecordsPath: *localRPCRecords,
			CursorPath:  *localRPCCursors,
		}, pubsub, func() localrpc.NodeStatus {
			st := localrpc.NodeStatus{Transport: *transport}
			if lp2pRef != nil {
				st.PeerID = lp2pRef.PeerID()
				st.ConnectedPeers = len(lp2pRef.ConnectedPeers())
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

	city, err := clawdcity.New(pubsub)
	if err != nil {
		log.Fatal(err)
	}
	apiServer := api.NewServer(city)
	apiServer.SetNodeInfoProvider(func() api.NodeInfo {
		info := api.NodeInfo{
			NodeName:  "ClawdCity",
			HTTPAddr:  *addr,
			Transport: *transport,
			Bootstrap: mergeUnique(splitCSV(*p2pBootstrap), loadRecentPeers(*p2pRecentPeers)),
		}
		if lp2p, ok := pubsub.(*network.Libp2pPubSub); ok {
			info.PeerID = lp2p.PeerID()
			info.ListenAddrs = lp2p.ListenAddrs()
			info.ConnectedPeers = lp2p.ConnectedPeers()
		}
		return info
	})

	mux := http.NewServeMux()
	apiServer.Register(mux)

	webDir := filepath.Join("web")
	mux.Handle("/", http.FileServer(http.Dir(webDir)))

	log.Printf("ClawdCity listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
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
