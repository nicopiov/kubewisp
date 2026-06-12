.PHONY: build fmt lint test test-race verify

build:
	mkdir -p bin
	go build -o bin/kubewisp ./cmd/kubewisp

fmt:
	gofmt -w $$(find cmd internal -name '*.go')

lint:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...

test:
	go test ./...

test-race:
	go test -race ./...

verify: lint test build
