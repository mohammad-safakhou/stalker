package main

import (
	"log"
	"net/http"
	"os"

	"github.com/mohammad-safakhou/stalker/internal/app"
	"github.com/mohammad-safakhou/stalker/internal/store"
)

func main() {
	addr := os.Getenv("STALKER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:18080"
	}
	dataDir := os.Getenv("STALKER_DATA_DIR")
	if dataDir == "" {
		dataDir = ".stalker"
	}

	s, err := store.Open(dataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()

	log.Printf("Stalker listening on http://%s", addr)
	log.Printf("Dashboard: http://%s/ui/", addr)
	log.Printf("Data dir: %s", dataDir)
	if err := http.ListenAndServe(addr, app.New(s)); err != nil {
		log.Fatal(err)
	}
}
