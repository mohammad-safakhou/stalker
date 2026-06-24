package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/mohammad-safakhou/stalker/internal/app"
	"github.com/mohammad-safakhou/stalker/internal/config"
	"github.com/mohammad-safakhou/stalker/internal/setup"
	"github.com/mohammad-safakhou/stalker/internal/store"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			runServer()
			return
		case "install":
			runInstall(os.Args[2:])
			return
		case "upgrade":
			runUpgrade()
			return
		case "compact":
			runCompact(os.Args[2:])
			return
		case "paths":
			runPaths()
			return
		case "help", "-h", "--help":
			usage()
			return
		}
	}
	runServer()
}

func runServer() {
	addr := config.Addr()
	dataDir, err := config.DataDir()
	if err != nil {
		log.Fatal(err)
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

func runInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	service := fs.String("service", "", "service to configure: codex or none")
	migrate := fs.Bool("migrate", false, "move legacy .stalker data into the app data directory, backing up any existing destination")
	yes := fs.Bool("yes", false, "accept non-interactive defaults")
	_ = fs.Parse(args)

	if err := setup.Install(setup.InstallOptions{
		In:      os.Stdin,
		Out:     os.Stdout,
		Service: *service,
		Migrate: *migrate,
		Yes:     *yes,
	}); err != nil {
		log.Fatal(err)
	}
}

func runUpgrade() {
	if err := setup.Upgrade(os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func runCompact(args []string) {
	fs := flag.NewFlagSet("compact", flag.ExitOnError)
	yes := fs.Bool("yes", false, "confirm raw payload removal")
	_ = fs.Parse(args)
	if !*yes {
		fmt.Fprintln(os.Stderr, "compact removes retained request/response payloads and per-exchange token rows. Re-run with --yes to confirm.")
		os.Exit(2)
	}
	dataDir, err := config.DataDir()
	if err != nil {
		log.Fatal(err)
	}
	s, err := store.Open(dataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	if err := s.CompactRawData(context.Background()); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Compacted raw data in %s\n", dataDir)
}

func runPaths() {
	dataDir, err := config.DataDir()
	if err != nil {
		log.Fatal(err)
	}
	defaultDataDir, err := config.DefaultDataDir()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Data dir: %s\n", dataDir)
	fmt.Printf("Default data dir: %s\n", defaultDataDir)
}

func usage() {
	fmt.Print(`Usage:
  stalker                 Run the proxy and dashboard
  stalker serve           Run the proxy and dashboard
  stalker install         Set up data storage and optional service config
  stalker upgrade         Upgrade to the latest version with go install
  stalker compact --yes   Remove retained raw payload data and shrink storage
  stalker paths           Print resolved data paths

Install options:
  --service codex|none    Configure a service non-interactively
  --migrate               Move existing .stalker data into the app data directory
  --yes                   Use non-interactive defaults
`)
}
