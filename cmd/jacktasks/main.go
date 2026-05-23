package main

import (
	"context"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/j-f-allison/jacktasks/internal/paths"
	"github.com/j-f-allison/jacktasks/internal/reminders"
	"github.com/j-f-allison/jacktasks/internal/store"
)

func main() {
	ctx := context.Background()

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

	m := newModel(s, deviceID, dataDir, ctx, remClient)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
