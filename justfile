#!/usr/bin/env just --justfile
# micbridge — local wireless-mic recording service

set shell := ["bash", "-euo", "pipefail", "-c"]

# Configuration (override with env or `just VAR=value recipe`)
binary_name := "micbridge"
bin_dir := "bin"
cmd_pkg := "./cmd/micbridge"
gobin := env_var_or_default("GOBIN", `go env GOPATH` + "/bin")
state_home := env_var_or_default("XDG_STATE_HOME", env_var("HOME") + "/.local/state")
state_dir := env_var_or_default("MICBRIDGE_STATE_DIR", state_home + "/micbridge")
commit := `git rev-parse --short=12 HEAD`
version := env_var_or_default("VERSION", "0.0.0-dev+" + commit)

# Focused modules (names avoid clashing with top-level recipes)
[doc('Build and clean artifacts')]
mod xbuild '.justfiles/build.just'

[doc('Unit, race, and package tests')]
mod xtest '.justfiles/test.just'

[doc('Local development helpers')]
mod xdev '.justfiles/dev.just'

[doc('Review and static analysis')]
mod xreview '.justfiles/review.just'

[doc('Install and release scaffolding')]
mod xrelease '.justfiles/release.just'

[private]
default:
    @echo "micbridge — programmable wireless microphone controls and local recording API"
    @echo ""
    @echo "Common recipes:"
    @echo "  just bootstrap   # verify toolchain; tidy modules"
    @echo "  just build       # build ./bin/micbridge"
    @echo "  just check       # fmt-check + vet + unit tests"
    @echo "  just test        # go test ./..."
    @echo "  just test-race   # go test -race ./..."
    @echo "  just install     # install to GOBIN"
    @echo "  just serve       # run the physical-device daemon"
    @echo "  just ui          # open the installed-style TUI client"
    @echo "  just demo        # version + status against a temp state dir"
    @echo "  just review      # static analysis + race suite"
    @echo ""
    @just --list --unsorted

# Verify Go toolchain and tidy modules (no package installs, no sudo).
bootstrap:
    @echo "bootstrap: go $(go env GOVERSION) module $(go list -m)"
    go version
    go mod tidy
    @test -f go.mod
    @echo "bootstrap: ok (hardware and privileged installers are opt-in later recipes)"

# Build the micbridge binary into bin/.
build:
    mkdir -p {{ bin_dir }}
    go build -ldflags="-X github.com/lancekrogers/micbridge/internal/core.Version={{ version }}" -o {{ bin_dir }}/{{ binary_name }} {{ cmd_pkg }}
    @echo "built {{ bin_dir }}/{{ binary_name }}"

# Format-check, vet, and unit tests (CI-friendly gate).
check: _fmt-check _vet test
    @echo "check: ok"

# Unit tests (no hardware).
test:
    go test ./...

# Race detector suite (no hardware).
test-race:
    go test -race ./...

# Install the binary into GOBIN (or GOPATH/bin).
install: build
    install -d "{{ gobin }}"
    install -m 755 "{{ bin_dir }}/{{ binary_name }}" "{{ gobin }}/{{ binary_name }}"
    cmp "{{ bin_dir }}/{{ binary_name }}" "{{ gobin }}/{{ binary_name }}"
    @echo "installed {{ gobin }}/{{ binary_name }} ($({{ gobin }}/{{ binary_name }} version))"

# Run the physical-device daemon using the freshly built binary.
serve: build
    "./{{ bin_dir }}/{{ binary_name }}" serve

# Open the TUI using the freshly built binary. Start `just serve` separately.
ui: build
    "./{{ bin_dir }}/{{ binary_name }}" ui

# Smoke demo: build, print version, status against a temporary state directory.
demo: build
    #!/usr/bin/env bash
    set -euo pipefail
    tmp="$(mktemp -d "${TMPDIR:-/tmp}/micbridge-demo.XXXXXX")"
    sock="$tmp/micbridge.sock"
    server_pid=""
    cleanup() {
      if [[ -n "$server_pid" ]]; then
        kill "$server_pid" 2>/dev/null || true
        wait "$server_pid" 2>/dev/null || true
      fi
      rm -rf "$tmp"
    }
    trap cleanup EXIT
    echo "demo: state_dir=$tmp"
    MICBRIDGE_STATE_DIR="$tmp" "./{{ bin_dir }}/{{ binary_name }}" version
    MICBRIDGE_FAKE=1 MICBRIDGE_STATE_DIR="$tmp" MICBRIDGE_SOCK="$sock" \
      "./{{ bin_dir }}/{{ binary_name }}" serve --json >"$tmp/server.log" 2>&1 &
    server_pid=$!
    for _ in $(seq 1 100); do
      [[ -S "$sock" ]] && break
      if ! kill -0 "$server_pid" 2>/dev/null; then
        cat "$tmp/server.log"
        exit 1
      fi
      sleep 0.02
    done
    [[ -S "$sock" ]] || { cat "$tmp/server.log"; echo "demo: socket was not ready"; exit 1; }
    MICBRIDGE_STATE_DIR="$tmp" MICBRIDGE_SOCK="$sock" \
      "./{{ bin_dir }}/{{ binary_name }}" status --json

# Static analysis + race tests for review gates.
review: _fmt-check _vet _lint test-race
    @echo "review: ok"

# --- shared primitives (private; used by root and modules via just) ---

[private]
_fmt:
    gofmt -s -w .

[private]
_fmt-check:
    #!/usr/bin/env bash
    set -euo pipefail
    out="$(gofmt -s -l .)"
    if [[ -n "$out" ]]; then
      echo "gofmt needed on:"
      echo "$out"
      exit 1
    fi

[private]
_vet:
    go vet ./...

[private]
_lint:
    #!/usr/bin/env bash
    set -euo pipefail
    if command -v golangci-lint >/dev/null 2>&1; then
      golangci-lint run ./...
    else
      if [[ "${MICBRIDGE_REQUIRE_LINT:-0}" == "1" ]]; then
        echo "lint: MICBRIDGE_REQUIRE_LINT=1 but golangci-lint is missing"
        exit 1
      fi
      echo "lint: golangci-lint not on PATH; running go vet only"
      go vet ./...
    fi
