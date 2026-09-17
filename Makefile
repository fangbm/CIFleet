.PHONY: test fmt build

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal

build:
	mkdir -p bin
	go build -o bin/cifleet-controller ./cmd/controller
	go build -o bin/cifleet-agent ./cmd/agent
