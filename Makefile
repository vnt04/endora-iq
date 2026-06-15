# ApiLens Makefile
#
# Go 1.25+ is required (see README: the charmbracelet/bubbles dependency needs it).
# In this repository's dev environment Go lives in WSL. Each recipe that calls Go
# first sources scripts/go-env.sh, which puts the WSL toolchain on PATH and pins
# caches; on a normal Linux/macOS box where `go` is already on PATH it is a no-op.
# `test` and `vet` wrap the scripts/ dev harness directly.
#
# Usage: run `make <target>` from a POSIX shell (Git Bash, WSL, Linux, macOS).
# On Windows, run these via WSL (e.g. `wsl.exe -d Ubuntu -- make build`).

BINARY   := apilens
PKG      := ./...
COVERAGE := coverage.out
GO       ?= go
GOFLAGS  ?=
ENV      := . ./scripts/go-env.sh;

.PHONY: all build test cover vet fmt install clean run help

all: build

## build: compile the apilens binary to ./apilens
build:
	$(ENV) $(GO) build $(GOFLAGS) -o $(BINARY) .

## test: run the full test suite with coverage (wraps scripts/test.sh)
test:
	./scripts/test.sh

## cover: run tests, write coverage.out, print the total coverage
cover:
	$(ENV) $(GO) test $(PKG) -count=1 -covermode=atomic -coverprofile=$(COVERAGE)
	$(ENV) $(GO) tool cover -func=$(COVERAGE) | tail -n 1

## vet: build + go vet the whole module (wraps scripts/check.sh)
vet:
	./scripts/check.sh

## fmt: gofmt all Go sources in place
fmt:
	$(ENV) $(GO) fmt $(PKG)

## install: install the apilens binary into $(GOBIN) / $(GOPATH)/bin
install:
	$(ENV) $(GO) install .

## run: build then run apilens (pass flags via ARGS, e.g. make run ARGS="version")
run: build
	./$(BINARY) $(ARGS)

## clean: remove the binary and coverage artifacts
clean:
	rm -f $(BINARY) $(BINARY).exe $(COVERAGE)
	$(ENV) $(GO) clean

## help: list available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed -e 's/## //'
