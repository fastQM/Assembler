package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"ClawdCity/internal/api"
	"ClawdCity/internal/clawdcity"
	"ClawdCity/internal/core/network"
	"ClawdCity/internal/games/poker"
	"ClawdCity/internal/runtime"
	"ClawdCity/internal/tetrisroom"
)

func main() {
	addr := flag.String("addr", ":8080", "http listen address")
	transport := flag.String("transport", "memory", "transport: memory|libp2p")
	p2pListen := flag.String("p2p-listen", "/ip4/0.0.0.0/tcp/0", "comma-separated libp2p listen multiaddrs")
	p2pBootstrap := flag.String("p2p-bootstrap", "", "comma-separated bootstrap peer multiaddrs")
	p2pRendezvous := flag.String("p2p-rendezvous", "ClawdCity", "libp2p mDNS rendezvous string")
	p2pMDNS := flag.Bool("p2p-mdns", true, "enable libp2p mDNS discovery")
	flag.Parse()

	var (
		pubsub network.PubSub
		closer func()
	)
	switch *transport {
	case "memory":
		pubsub = network.NewMemoryPubSub()
	case "libp2p":
		lp2p, err := network.NewLibp2pPubSub(context.Background(), network.Libp2pOptions{
			ListenAddrs: splitCSV(*p2pListen),
			Bootstrap:   splitCSV(*p2pBootstrap),
			Rendezvous:  *p2pRendezvous,
			EnableMDNS:  *p2pMDNS,
		})
		if err != nil {
			log.Fatal(err)
		}
		pubsub = lp2p
		closer = func() { _ = lp2p.Close() }
		log.Printf("libp2p peer id: %s", lp2p.PeerID())
		for _, a := range lp2p.ListenAddrs() {
			log.Printf("libp2p listen: %s", a)
		}
	default:
		log.Fatalf("unsupported transport: %s", *transport)
	}
	if closer != nil {
		defer closer()
	}

	engine := runtime.NewEngine(pubsub)
	engine.RegisterAdapter(poker.NewAdapter())
	city, err := clawdcity.New(pubsub)
	if err != nil {
		log.Fatal(err)
	}
	tetris := tetrisroom.NewManager(pubsub)

	mux := http.NewServeMux()
	api.NewServer(engine, city, tetris).Register(mux)

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
