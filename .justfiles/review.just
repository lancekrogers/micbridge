# Review module — static analysis for sequence gates

# Full review gate: format, vet, lint, race.
all:
    just review

# golangci-lint when available.
lint:
    #!/usr/bin/env bash
    set -euo pipefail
    if command -v golangci-lint >/dev/null 2>&1; then
      golangci-lint run ./...
    else
      echo "xreview::lint: golangci-lint missing; go vet fallback"
      go vet ./...
    fi

# go vet only.
vet:
    go vet ./...

# Fail if gofmt -s would change files.
fmt-check:
    #!/usr/bin/env bash
    set -euo pipefail
    out="$(gofmt -s -l .)"
    if [[ -n "$out" ]]; then
      echo "gofmt needed on:"
      echo "$out"
      exit 1
    fi
