SHELL := /usr/bin/env bash

.DEFAULT_GOAL := build

.PHONY: gen wire build test test-race lint vuln fmt

gen:
	go generate ./...

wire:
	go tool wire gen ./internal

build:
	CGO_ENABLED=0 go build ./...

test:
	go test ./...

test-race:
	go test -race ./...

lint:
	go tool golangci-lint run

vuln:
	go tool govulncheck ./...

fmt:
	go fmt ./...
