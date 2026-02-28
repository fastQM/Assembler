package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"Assembler-Apps/internal/appmodule"
	socialmodule "Assembler-Apps/internal/appmodule/social"
	"Assembler-Apps/internal/social"
	"Assembler-Apps/internal/socialapi"
)

func main() {
	addr := flag.String("addr", ":8090", "http listen address")
	socialRPCSock := flag.String("social-rpc-sock", filepath.Join("..", "Assembler", "data", "assembler-p2p.sock"), "assembler local rpc unix socket path")
	socialPassphrase := flag.String("social-passphrase", os.Getenv("SOCIAL_KEY_PASSPHRASE"), "optional social key passphrase for startup unlock")
	flag.Parse()

	socialManager, err := social.NewManager(social.Config{
		DataDir:       filepath.Join("data", "social"),
		RPCSocketPath: *socialRPCSock,
		Passphrase:    *socialPassphrase,
	})
	if err != nil {
		log.Fatalf("init social manager failed: %v", err)
	}
	socialServer := socialapi.NewServer(socialManager)
	registry := appmodule.NewRegistry()
	if err := registry.Register(socialmodule.New(socialServer)); err != nil {
		log.Fatalf("register social module failed: %v", err)
	}

	mux := http.NewServeMux()
	registry.MountAll(mux)
	mux.HandleFunc("/api/apps", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"apps": registry.IDs()})
	})
	mux.Handle("/", http.FileServer(http.Dir(".")))

	log.Printf("Assembler-Apps listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}
