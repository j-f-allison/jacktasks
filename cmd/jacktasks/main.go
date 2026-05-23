package main

import (
	"context"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/j-f-allison/jacktasks/internal/paths"
	"github.com/j-f-allison/jacktasks/internal/store"
)

func main() {
	ctx := context.Background()

	dbPath, err := paths.DBPath()
	if err != nil {
		log.Fatalf("paths: %v", err)
	}

	s, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer s.Close()

	deviceID, err := s.DeviceID(ctx)
	if err != nil {
		log.Fatalf("device id: %v", err)
	}

	m := newModel(s, deviceID, ctx)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
