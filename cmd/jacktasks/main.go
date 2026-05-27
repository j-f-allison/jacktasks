package main

import (
	"context"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/j-f-allison/jacktasks/internal/config"
	"github.com/j-f-allison/jacktasks/internal/paths"
	"github.com/j-f-allison/jacktasks/internal/reminders"
	"github.com/j-f-allison/jacktasks/internal/store"
	"github.com/j-f-allison/jacktasks/internal/syncclient"
)

func main() {
	ctx := context.Background()

	// Subcommand dispatch: "jacktasks sync" runs a one-shot sync and exits.
	// Everything else (including no args) launches the TUI.
	if len(os.Args) > 1 && os.Args[1] == "sync" {
		runSync(ctx)
		return
	}

	runTUI(ctx)
}

// runSync performs one push-pull cycle against the sync server and exits.
// Reads JACKTASKS_SYNC_URL and JACKTASKS_SYNC_TOKEN from the environment.
func runSync(ctx context.Context) {
	url := os.Getenv("JACKTASKS_SYNC_URL")
	token := os.Getenv("JACKTASKS_SYNC_TOKEN")
	if url == "" || token == "" {
		fmt.Fprintln(os.Stderr, "error: JACKTASKS_SYNC_URL and JACKTASKS_SYNC_TOKEN must be set")
		os.Exit(1)
	}

	dataDir, err := paths.DataDir()
	if err != nil {
		log.Fatalf("paths: %v", err)
	}
	s, err := store.Open(paths.DBPathFromDir(dataDir))
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer s.Close()

	cfg := syncclient.Config{URL: url, Token: token}
	if err := syncclient.Sync(ctx, s, cfg, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "sync error: %v\n", err)
		os.Exit(1)
	}
}

// runTUI opens the store and launches the Bubble Tea TUI.
func runTUI(ctx context.Context) {
	dataDir, err := paths.DataDir()
	if err != nil {
		log.Fatalf("paths: %v", err)
	}

	s, err := store.Open(paths.DBPathFromDir(dataDir))
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer s.Close()

	deviceID, err := s.DeviceID(ctx)
	if err != nil {
		log.Fatalf("device id: %v", err)
	}

	// Reminders is best-effort: access denied or non-darwin is non-fatal.
	var remClient reminders.Client
	if rc, err := reminders.NewEventKit(); err != nil {
		fmt.Fprintf(os.Stderr, "reminders unavailable: %v\n", err)
	} else {
		remClient = rc
	}

	syncCfg := syncclient.Config{
		URL:   os.Getenv("JACKTASKS_SYNC_URL"),
		Token: os.Getenv("JACKTASKS_SYNC_TOKEN"),
	}

	cfgPath, err := paths.ConfigPath()
	if err != nil {
		log.Fatalf("config path: %v", err)
	}
	appCfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	m := newModel(s, deviceID, dataDir, ctx, remClient, syncCfg, appCfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
