#!/usr/bin/env bash
# Build + vet the whole module. Returns non-zero on failure.
set -o pipefail
DIR="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=/dev/null
source "$DIR/scripts/go-env.sh"
cd "$DIR" || exit 1
echo "=== go version ==="; go version
echo "=== go build ./... ==="
go build ./... 2>&1
BUILD=$?
echo "=== go vet ./... ==="
go vet ./... 2>&1
VET=$?
echo "BUILD_EXIT=$BUILD VET_EXIT=$VET"
exit $(( BUILD != 0 ? BUILD : VET ))
