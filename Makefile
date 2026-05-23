.PHONY: build test vet check install build-sync-linux

## build — compile all binaries for the current platform (macOS)
build:
	go build ./...

## test — run all tests
test:
	go test ./...

## vet — run go vet
vet:
	go vet ./...

## check — build + vet + test (run before committing)
check: build vet test

## install — install the TUI client (macOS). Defaults to /usr/local/bin (needs sudo).
## Override with: make install PREFIX=$HOME/.local/bin   (no sudo needed)
PREFIX ?= /usr/local/bin
install:
	go build -o $(PREFIX)/jacktasks ./cmd/jacktasks
	@echo "Installed: $(PREFIX)/jacktasks"

## build-sync-linux — cross-compile the sync server for linux/amd64 (ThinkCentre)
## Output: jacktasks-sync-linux in the repo root
build-sync-linux:
	GOOS=linux GOARCH=amd64 go build -o jacktasks-sync-linux ./cmd/jacktasks-sync
	@echo "Built: jacktasks-sync-linux"
	@echo "Next: scp jacktasks-sync-linux <thinkcentre>:/tmp/ and follow deploy/DEPLOY.md"
