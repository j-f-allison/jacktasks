package main

// Version is set at build time via -ldflags "-X main.Version=X.Y.Z".
// The Makefile is the single source of truth; this default matches it.
var Version = "1.8.0"
