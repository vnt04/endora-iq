#!/usr/bin/env bash
# Run the full test suite with coverage summary.
set -o pipefail
DIR="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=/dev/null
source "$DIR/scripts/go-env.sh"
cd "$DIR" || exit 1
go test ./... -count=1 -cover 2>&1
exit $?
