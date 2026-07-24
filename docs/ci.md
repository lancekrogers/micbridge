# Continuous integration

## Workflow

File: `.github/workflows/ci.yml`

Runs on every pull request and on pushes to `main`. The job is **hardware-free**
and uses the same `just` recipes as local development:

1. Install Linux native build prerequisites (`build-essential`, `pkg-config`,
   `libasound2-dev`) for CGO/miniaudio readiness.
2. `just bootstrap`
3. `just check` — gofmt, vet, unit tests
4. `just test-race`
5. `just build`
6. `just demo`
7. `just review` — lint + race
8. Assert HIL smoke remains opt-in (must not pass as a required gate without
   hardware).

## Pinning and cache

- Actions are version-pinned (e.g. `actions/checkout@v4.2.2`,
  `actions/setup-go@v5.4.0`).
- Go module cache uses `actions/setup-go` cache keyed from `go.mod` /
  `go.sum`.
- No secrets, audio fixtures, or hardware runners are required.

## Reproducing failures locally

```bash
# Full gate
just bootstrap && just check && just test-race && just build && just demo && just review

# Single slices
just check
just test-race
just review
```

If `golangci-lint` is missing locally, `just review` falls back to `go vet`;
install the same major version CI uses for parity:

```bash
# example — match host tooling when possible
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

## Native / CGO notes

Capture will use `github.com/gen2brain/malgo` (miniaudio) with cgo. CI installs
ALSA development headers so future packages that enable cgo compile on Ubuntu
runners without a USB receiver. Physical Hollyland acceptance remains a later,
opt-in hardware job.
