#!/usr/bin/env bash
# Canonical Go environment for ApiLens dev tasks (WSL).
# Source this, then run go commands. Keeps caches WSL-native and pins the
# local toolchain so go.mod's "go 1.24" line does not trigger a toolchain download.
export PATH="$HOME/.local/go/bin:$PATH"
export GOTOOLCHAIN=local
export GOFLAGS=-mod=mod
export CGO_ENABLED=0
export GOCACHE="$HOME/.cache/go-build"
export GOMODCACHE="$HOME/go/pkg/mod"
export GOPROXY="https://proxy.golang.org,direct"
export GOSUMDB="sum.golang.org"
# Note: this script only sets env vars. Callers must cd into the repo root.
