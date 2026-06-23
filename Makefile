BIN ?= stalker

.PHONY: build install run test

build:
	mkdir -p bin
	go build -o bin/$(BIN) ./cmd/stalker

install:
	go install ./cmd/stalker
	stalker install

run:
	go run ./cmd/stalker

test:
	go test ./...
