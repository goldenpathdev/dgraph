#!/usr/bin/env bash
# OWLGraph — local build & test script
# Usage:
#   ./scripts/build.sh              # full build + owl tests
#   ./scripts/build.sh build        # build binary only
#   ./scripts/build.sh test         # owl package tests only
#   ./scripts/build.sh test-all     # all owl/* tests
#   ./scripts/build.sh verify       # compile check + tests (no binary)
#   ./scripts/build.sh binary-check # just verify binary runs
#   ./scripts/build.sh image        # build local Docker image
#   ./scripts/build.sh cluster-up   # start dev cluster (1 Zero, 1 Alpha)
#   ./scripts/build.sh cluster-down # stop dev cluster
#   ./scripts/build.sh cluster-health # check cluster health

set -euo pipefail

export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH
export GOPATH=${GOPATH:-$HOME/go}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS${NC} $1"; }
fail() { echo -e "${RED}FAIL${NC} $1"; exit 1; }
info() { echo -e "${YELLOW}==>${NC} $1"; }

cmd_build() {
    info "Building dgraph binary..."
    make dgraph 2>&1
    if [[ -f dgraph/dgraph ]]; then
        pass "Binary built: dgraph/dgraph ($(du -h dgraph/dgraph | cut -f1))"
    else
        fail "Binary not found after build"
    fi
}

cmd_binary_check() {
    info "Checking dgraph binary..."
    if [[ ! -f dgraph/dgraph ]]; then
        fail "No binary found at dgraph/dgraph — run './scripts/build.sh build' first"
    fi
    VERSION=$(./dgraph/dgraph version 2>&1 | grep "Dgraph version" | sed 's/^[[:space:]]*//')
    pass "Binary OK: $VERSION"
}

cmd_test() {
    info "Running owl/ tests..."
    go test -v ./owl/ 2>&1
    pass "owl/ tests"
}

cmd_test_all() {
    info "Running all owl/* tests..."
    go test -v ./owl/... 2>&1
    pass "All owl/* tests"
}

cmd_verify() {
    info "Compile check: all owl packages..."
    go build ./owl/... 2>&1
    pass "owl packages compile"

    info "Running owl tests..."
    go test -v -count=1 ./owl/... 2>&1
    pass "All owl tests"

    info "Compile check: main dgraph binary (no install)..."
    go build -o /dev/null ./dgraph/ 2>&1
    pass "Dgraph binary compiles"
}

cmd_image() {
    info "Building local Docker image..."
    make local-image 2>&1
    pass "Docker image: dgraph/dgraph:local"
}

cmd_cluster_up() {
    if ! docker image inspect dgraph/dgraph:local >/dev/null 2>&1; then
        info "No local image found, building first..."
        cmd_image
    fi
    info "Starting dev cluster..."
    docker compose -f "$PROJECT_DIR/dev/docker-compose.yml" up -d 2>&1
    info "Waiting for Alpha to be healthy..."
    for i in $(seq 1 30); do
        if curl -sf http://localhost:8080/health >/dev/null 2>&1; then
            pass "Dev cluster running (Alpha: localhost:8080, gRPC: localhost:9080)"
            return 0
        fi
        sleep 2
    done
    fail "Cluster did not become healthy within 60s"
}

cmd_cluster_down() {
    info "Stopping dev cluster..."
    docker compose -f "$PROJECT_DIR/dev/docker-compose.yml" down 2>&1
    pass "Dev cluster stopped"
}

cmd_cluster_health() {
    if curl -sf http://localhost:8080/health >/dev/null 2>&1; then
        VERSION=$(curl -s http://localhost:8080/health | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['version'])" 2>/dev/null || echo "unknown")
        pass "Cluster healthy (Alpha v$VERSION)"
    else
        fail "Cluster not reachable at localhost:8080"
    fi
}

cmd_full() {
    cmd_build
    echo ""
    cmd_test_all
    echo ""
    cmd_binary_check
    echo ""
    info "All checks passed."
}

case "${1:-full}" in
    build)        cmd_build ;;
    test)         cmd_test ;;
    test-all)     cmd_test_all ;;
    verify)       cmd_verify ;;
    binary-check) cmd_binary_check ;;
    image)          cmd_image ;;
    cluster-up)     cmd_cluster_up ;;
    cluster-down)   cmd_cluster_down ;;
    cluster-health) cmd_cluster_health ;;
    full)           cmd_full ;;
    *)
        echo "Usage: $0 {build|test|test-all|verify|binary-check|image|cluster-up|cluster-down|cluster-health|full}"
        exit 1
        ;;
esac
