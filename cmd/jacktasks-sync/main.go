// jacktasks-sync is the self-hosted sync server for jacktasks.
// It exposes a small REST API over HTTP, intended to run on a homelab machine
// reachable via Tailscale.
//
// Configuration (all required):
//
//	JACKTASKS_SYNC_TOKEN  shared bearer token (same value on all clients)
//	JACKTASKS_SYNC_DB     path to the master SQLite database
//	JACKTASKS_SYNC_ADDR   listen address, e.g. "100.64.0.1:8484"
//
// Endpoints:
//
//	GET  /healthz
//	POST /push?table=<name>
//	GET  /pull?table=<name>&since=<unix_sec>
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/j-f-allison/jacktasks/internal/store"
	"github.com/j-f-allison/jacktasks/internal/syncserver"
)

func main() {
	token := requireEnv("JACKTASKS_SYNC_TOKEN")
	dbPath := requireEnv("JACKTASKS_SYNC_DB")
	addr := requireEnv("JACKTASKS_SYNC_ADDR")

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open db %q: %v", dbPath, err)
	}
	defer st.Close()
	log.Printf("database: %s", dbPath)

	mux := syncserver.NewMux(st, token)

	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "error: %s is required\n", key)
		os.Exit(1)
	}
	return v
}
