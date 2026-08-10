#!/usr/bin/env bash
# Containerized Go race regression runner (standard environment).
#
# Rationale: `make test` / `scripts/test-go.sh --race` needs cgo (-race) and a
# working gcc. Hosts without a C toolchain fail immediately with
#   go: -race requires cgo; enable cgo by setting CGO_ENABLED=1
# This script wraps the identical suite in the official golang:1.25 image
# (bundled gcc), sharing the host module cache and DB, and disables the module
# proxy (GOPROXY=off) so the run is hermetic and offline.
#
# Usage (from repo root):
#   bash scripts/test-go-docker.sh --race
#   bash scripts/test-go-docker.sh                 # non-race, same env
#
# Env overrides:
#   GOLANG_IMAGE   image to run (default golang:1.25)
#   DATABASE_URL   test DB DSN (default localhost:5432/multica)
#   GO_MOD_CACHE   host module cache dir (default $HOME/go/pkg/mod)
set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)

GOLANG_IMAGE="${GOLANG_IMAGE:-golang:1.25}"
DATABASE_URL="${DATABASE_URL:-postgres://multica:multica@localhost:5432/multica?sslmode=disable}"
GO_MOD_CACHE="${GO_MOD_CACHE:-$HOME/go/pkg/mod}"

if [ ! -d "$GO_MOD_CACHE" ]; then
  echo "host module cache $GO_MOD_CACHE not found; prime it first (e.g. go mod download)" >&2
  exit 1
fi

# Run from the server module dir so go resolves server/go.mod.
exec docker run --rm --network host \
  -v "$REPO_ROOT:/workspace" \
  -v "$GO_MOD_CACHE:/go/pkg/mod:ro" \
  -e GOPROXY=off \
  -e GOMODCACHE=/go/pkg/mod \
  -e GOFLAGS=-mod=mod \
  -e DATABASE_URL="$DATABASE_URL" \
  -w /workspace \
  "$GOLANG_IMAGE" \
  bash scripts/test-go.sh "$@"
