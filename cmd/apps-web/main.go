package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

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

	mux := http.NewServeMux()
	socialServer.Register(mux)
	mux.Handle("/", http.FileServer(http.Dir(".")))

	log.Printf("Assembler-Apps listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}
