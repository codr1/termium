#!/bin/bash
# Compatibility entry point. The portable Go harness replaces the old smoke checks.
set -euo pipefail
cd "$(dirname "$0")/.."
go test -tags=integration -count=1 -timeout=1m -v ./tests/integration -run '^TestBinaryCLI$'
