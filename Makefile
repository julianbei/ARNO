.PHONY: build test fmt

build:
	go build ./...

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal
