package main

import (
	"context"
	"fmt"
	"log"

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

	fmt.Println("jacktasks — Phase 1 skeleton")
	fmt.Printf("  db:        %s\n", dbPath)
	fmt.Printf("  device_id: %s\n", deviceID)
}