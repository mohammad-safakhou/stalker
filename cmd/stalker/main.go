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
	"github.com/mohammad-safakhou/stalker/internal/discovery"
	"github.com/mohammad-safakhou/stalker/internal/setup"
	"github.com/mohammad-safakhou/stalker/internal/store"
	"github.com/mohammad-safakhou/stalker/internal/syncapi"
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
			runUpgrade(os.Args[2:])
			return
		case "runner":
			runRunner(os.Args[2:])
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
	syncAddr := config.SyncAddr()
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
	if syncAddr != "" {
		syncServer := &http.Server{Addr: syncAddr, Handler: syncapi.New(s)}
		go func() {
			log.Printf("Sync API listening on http://%s", syncAddr)
			if err := syncServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("sync server error: %v", err)
			}
		}()
		defer syncServer.Shutdown(context.Background())

		advertiser, err := discovery.Advertise(syncAddr, s)
		if err != nil {
			log.Printf("Bonjour discovery disabled: %v", err)
		} else if advertiser != nil {
			defer advertiser.Shutdown()
			log.Printf("Bonjour discovery: %s", discovery.ServiceType)
		}
	}
	if err := http.ListenAndServe(addr, app.New(s)); err != nil {
		log.Fatal(err)
	}
}

func runInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	service := fs.String("service", "", "service to configure: codex or none")
	runner := fs.String("runner", "", "background runner to install: launchd or none")
	migrate := fs.Bool("migrate", false, "move legacy .stalker data into the app data directory, backing up any existing destination")
	noStartRunner := fs.Bool("no-start-runner", false, "install the background runner without starting it")
	yes := fs.Bool("yes", false, "accept non-interactive defaults")
	_ = fs.Parse(args)

	if err := setup.Install(setup.InstallOptions{
		In:            os.Stdin,
		Out:           os.Stdout,
		Runner:        *runner,
		Service:       *service,
		Migrate:       *migrate,
		NoStartRunner: *noStartRunner,
		Yes:           *yes,
	}); err != nil {
		log.Fatal(err)
	}
}

func runUpgrade(args []string) {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	restartRunner := fs.Bool("restart-runner", false, "restart the launchd runner after upgrading")
	_ = fs.Parse(args)
	if err := setup.Upgrade(setup.UpgradeOptions{Out: os.Stdout, RestartRunner: *restartRunner}); err != nil {
		log.Fatal(err)
	}
}

func runRunner(args []string) {
	if len(args) == 0 {
		runnerUsage()
		os.Exit(2)
	}
	var err error
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("runner install", flag.ExitOnError)
		start := fs.Bool("start", false, "start the launchd runner after installing it")
		_ = fs.Parse(args[1:])
		err = setup.InstallLaunchAgent(os.Stdout, *start)
	case "start":
		err = setup.StartLaunchAgent(os.Stdout)
	case "stop":
		err = setup.StopLaunchAgent(os.Stdout)
	case "restart":
		err = setup.RestartLaunchAgent(os.Stdout)
	case "status":
		err = setup.LaunchAgentStatus(os.Stdout)
	case "uninstall":
		err = setup.UninstallLaunchAgent(os.Stdout)
	case "path":
		var path string
		path, err = setup.LaunchAgentPath()
		if err == nil {
			fmt.Println(path)
		}
	default:
		runnerUsage()
		os.Exit(2)
	}
	if err != nil {
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
  stalker runner status   Show macOS LaunchAgent status
  stalker compact --yes   Remove retained raw payload data and shrink storage
  stalker paths           Print resolved data paths

Install options:
  --runner launchd|none   Install a background runner
  --no-start-runner       Write the runner but do not start it
  --service codex|none    Configure a service non-interactively
  --migrate               Move existing .stalker data into the app data directory
  --yes                   Use non-interactive defaults

Upgrade options:
  --restart-runner        Restart the launchd runner after upgrading
`)
}

func runnerUsage() {
	fmt.Print(`Usage:
  stalker runner install [--start]  Install the macOS LaunchAgent
  stalker runner start              Start the LaunchAgent
  stalker runner stop               Stop the LaunchAgent
  stalker runner restart            Restart the LaunchAgent
  stalker runner status             Print launchctl status
  stalker runner uninstall          Stop and remove the LaunchAgent
  stalker runner path               Print the LaunchAgent plist path
`)
}
