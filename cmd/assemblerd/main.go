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

type fileConfig struct {
	Transport               string   `json:"transport"`
	P2PListen               []string `json:"p2p_listen"`
	P2PBootstrap            []string `json:"p2p_bootstrap"`
	P2PMDNS                 *bool    `json:"p2p_mdns"`
	P2PKadDHT               *bool    `json:"p2p_kad_dht"`
	P2PKadApps              []string `json:"p2p_kad_apps"`
	P2PKadDiscoveryInterval int      `json:"p2p_kad_discovery_interval_sec"`
	P2PKadQueryTimeout      int      `json:"p2p_kad_query_timeout_sec"`
	P2PRendezvous           string   `json:"p2p_rendezvous"`
	P2PIdentityKey          string   `json:"p2p_identity_key"`
	P2PRecentPeers          string   `json:"p2p_recent_peers"`
	LocalRPCEnable          *bool    `json:"local_rpc_enable"`
	LocalRPCSock            string   `json:"local_rpc_sock"`
	LocalRPCRecords         string   `json:"local_rpc_records"`
	LocalRPCCursors         string   `json:"local_rpc_cursors"`
}

type daemonConfig struct {
	Transport               string
	P2PListen               []string
	P2PBootstrap            []string
	P2PMDNS                 bool
	P2PKadDHT               bool
	P2PKadApps              []string
	P2PKadDiscoveryInterval time.Duration
	P2PKadQueryTimeout      time.Duration
	P2PRendezvous           string
	P2PIdentityKey          string
	P2PRecentPeers          string
	LocalRPCEnable          bool
	LocalRPCSock            string
	LocalRPCRecords         string
	LocalRPCCursors         string
}

func main() {
	preConfigPath := detectConfigPath(os.Args[1:], filepath.Join("data", "assembler.json"))
	cfg, err := loadDaemonConfig(preConfigPath)
	if err != nil {
		log.Fatal(err)
	}

	configPath := flag.String("config", preConfigPath, "config file path")
	transport := flag.String("transport", cfg.Transport, "transport: memory|libp2p")
	p2pListen := flag.String("p2p-listen", strings.Join(cfg.P2PListen, ","), "comma-separated libp2p listen multiaddrs")
	p2pBootstrap := flag.String("p2p-bootstrap", strings.Join(cfg.P2PBootstrap, ","), "comma-separated bootstrap peer multiaddrs")
	p2pRendezvous := flag.String("p2p-rendezvous", cfg.P2PRendezvous, "libp2p mDNS rendezvous string")
	p2pMDNS := flag.Bool("p2p-mdns", cfg.P2PMDNS, "enable libp2p mDNS discovery")
	p2pKadDHT := flag.Bool("p2p-kad-dht", cfg.P2PKadDHT, "enable libp2p kademlia dht discovery")
	p2pKadApps := flag.String("p2p-kad-apps", strings.Join(cfg.P2PKadApps, ","), "comma-separated app ids allowed to use kademlia discovery (empty means all apps)")
	p2pKadDiscoveryEvery := flag.Duration("p2p-kad-discovery-interval", cfg.P2PKadDiscoveryInterval, "kademlia discovery interval")
	p2pKadQueryTimeout := flag.Duration("p2p-kad-query-timeout", cfg.P2PKadQueryTimeout, "kademlia discovery query timeout")
	p2pIdentityKey := flag.String("p2p-identity-key", cfg.P2PIdentityKey, "libp2p private key path for stable peer id")
	p2pRecentPeers := flag.String("p2p-recent-peers", cfg.P2PRecentPeers, "file path to persist recently connected peers")
	localRPCEnable := flag.Bool("local-rpc-enable", cfg.LocalRPCEnable, "enable local unix-socket RPC for app p2p access")
	localRPCSock := flag.String("local-rpc-sock", cfg.LocalRPCSock, "local rpc unix socket path")
	localRPCRecords := flag.String("local-rpc-records", cfg.LocalRPCRecords, "local rpc message store path")
	localRPCCursors := flag.String("local-rpc-cursors", cfg.LocalRPCCursors, "local rpc cursor store path")
	flag.Parse()
	_ = configPath

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

func defaultDaemonConfig() daemonConfig {
	return daemonConfig{
		Transport:               "libp2p",
		P2PListen:               []string{"/ip4/0.0.0.0/tcp/40001"},
		P2PBootstrap:            []string{"/ip4/3.65.204.231/tcp/40001/p2p/12D3KooWAaYG182TYGF5GTfWu5CZpiWbf5r6GJwfuSsYRsErA5YL"},
		P2PMDNS:                 true,
		P2PKadDHT:               true,
		P2PKadApps:              []string{"social"},
		P2PKadDiscoveryInterval: 20 * time.Second,
		P2PKadQueryTimeout:      10 * time.Second,
		P2PRendezvous:           "Assembler",
		P2PIdentityKey:          filepath.Join("data", "p2p_identity.key"),
		P2PRecentPeers:          filepath.Join("data", "recent_peers.json"),
		LocalRPCEnable:          true,
		LocalRPCSock:            filepath.Join("data", "assembler-p2p.sock"),
		LocalRPCRecords:         filepath.Join("data", "p2p_messages.jsonl"),
		LocalRPCCursors:         filepath.Join("data", "p2p_cursors.json"),
	}
}

func loadDaemonConfig(path string) (daemonConfig, error) {
	def := defaultDaemonConfig()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return def, nil
		}
		return daemonConfig{}, err
	}
	var fc fileConfig
	if err := json.Unmarshal(b, &fc); err != nil {
		return daemonConfig{}, err
	}
	if fc.Transport != "" {
		def.Transport = fc.Transport
	}
	if len(fc.P2PListen) > 0 {
		def.P2PListen = fc.P2PListen
	}
	if len(fc.P2PBootstrap) > 0 {
		def.P2PBootstrap = fc.P2PBootstrap
	}
	if fc.P2PMDNS != nil {
		def.P2PMDNS = *fc.P2PMDNS
	}
	if fc.P2PKadDHT != nil {
		def.P2PKadDHT = *fc.P2PKadDHT
	}
	if fc.P2PKadApps != nil {
		def.P2PKadApps = fc.P2PKadApps
	}
	if fc.P2PKadDiscoveryInterval > 0 {
		def.P2PKadDiscoveryInterval = time.Duration(fc.P2PKadDiscoveryInterval) * time.Second
	}
	if fc.P2PKadQueryTimeout > 0 {
		def.P2PKadQueryTimeout = time.Duration(fc.P2PKadQueryTimeout) * time.Second
	}
	if fc.P2PRendezvous != "" {
		def.P2PRendezvous = fc.P2PRendezvous
	}
	if fc.P2PIdentityKey != "" {
		def.P2PIdentityKey = fc.P2PIdentityKey
	}
	if fc.P2PRecentPeers != "" {
		def.P2PRecentPeers = fc.P2PRecentPeers
	}
	if fc.LocalRPCEnable != nil {
		def.LocalRPCEnable = *fc.LocalRPCEnable
	}
	if fc.LocalRPCSock != "" {
		def.LocalRPCSock = fc.LocalRPCSock
	}
	if fc.LocalRPCRecords != "" {
		def.LocalRPCRecords = fc.LocalRPCRecords
	}
	if fc.LocalRPCCursors != "" {
		def.LocalRPCCursors = fc.LocalRPCCursors
	}
	return def, nil
}

func detectConfigPath(args []string, fallback string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-config" && i+1 < len(args) {
			return strings.TrimSpace(args[i+1])
		}
		if strings.HasPrefix(a, "-config=") {
			return strings.TrimSpace(strings.TrimPrefix(a, "-config="))
		}
	}
	return fallback
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
