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

## install — install the TUI client to /usr/local/bin (macOS)
install:
	go build -o /usr/local/bin/jacktasks ./cmd/jacktasks

## build-sync-linux — cross-compile the sync server for linux/amd64 (ThinkCentre)
## Output: jacktasks-sync-linux in the repo root
build-sync-linux:
	GOOS=linux GOARCH=amd64 go build -o jacktasks-sync-linux ./cmd/jacktasks-sync
	@echo "Built: jacktasks-sync-linux"
	@echo "Next: scp jacktasks-sync-linux <thinkcentre>:/tmp/ and follow deploy/DEPLOY.md"
