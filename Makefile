BIN ?= $(HOME)/go/bin/kripke

.PHONY: build install test lint tidy bench

build:
	go build ./...

install:
	go build -o $(BIN) ./cmd/kripke

test:
	go test ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"
	go run ./cmd/kripke purity ./examples/settle/core

tidy:
	go mod tidy

bench:
	go test ./fv -run '^$$' -bench . -benchtime 2x
