# Contributing to Micbridge

Micbridge is a local-first wireless microphone control and recording service.
Treat device evidence, logs, and recordings as sensitive even among
collaborators. Never commit live audio.

Please follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Prerequisites

| Tool | Notes |
|---|---|
| Go | Version from `go.mod` (`1.26`+) |
| just | Task runner (`just --list`); optional if you invoke `go` directly |
| C toolchain | Required for malgo/miniaudio (`build-essential`) |
| pkg-config + ALSA headers | Linux: `libasound2-dev` (Debian/Ubuntu) |

Hardware (Hollyland USB `3547:0007`) is **not** required for ordinary
development or CI. Physical tests are opt-in and clearly named.

### Linux native libraries

```bash
# Debian/Ubuntu (matches CI)
sudo apt-get install --yes build-essential pkg-config libasound2-dev

# Arch Linux
sudo pacman -S --needed base-devel alsa-lib
```

CGO must be enabled for capture builds (`CGO_ENABLED=1`, the Go default on
this platform). CI installs the same packages so pull requests never depend on
a developer workstation having a receiver attached.

## Setup

```bash
git clone https://github.com/lancekrogers/micbridge.git
cd micbridge
just bootstrap
just check
just build
just demo
```

Without just:

```bash
go test ./...
go build -o bin/micbridge ./cmd/micbridge
```

## Everyday commands

These are the same recipes CI runs:

| Recipe | Purpose |
|---|---|
| `just bootstrap` | Toolchain check + `go mod tidy` |
| `just build` | Build `bin/micbridge` |
| `just check` | gofmt check, `go vet`, unit tests |
| `just test` | `go test ./...` |
| `just test-race` | `go test -race ./...` |
| `just review` | lint + race suite |
| `just demo` | version/status against a temp state dir |
| `just install` | install binary to `GOBIN` |

Reproduce a failed CI check locally by running the same recipe from the
repository root:

```bash
just check
just test-race
just review
```

Details: [docs/ci.md](docs/ci.md).

### Hardware-only recipes

Anything under `just xdev hil-*` requires an attached receiver and must
**not** be required for merge. Do not wire HIL into the default `check` path.

## Pull requests

- Keep changes focused; prefer small vertical slices.
- Add or update tests with behavior changes.
- Run `just check` and `just test-race` before requesting review.
- Do not commit recordings, journals, serials, private paths, or live audio.
- Generated fixtures must be synthetic or licensed — never user captures.
- Do not add vendor USB writes, firmware actions, or unproven transmitter
  capabilities.

Match Hollyland only as exact USB `3547:0007` plus the measured LARK M2 USB-C
receiver descriptor shape. Do not broaden matching to brand, model text, or
other receiver variants.

## Repository layout

```text
cmd/micbridge/          CLI entry
internal/core/          shared product constants/contracts
internal/driver/        compiled-in driver registry (fake + Hollyland)
internal/event/         ordered event bus
internal/binding/       control → action mapping
internal/recording/     session / recovery
internal/audio/         capture (malgo)
internal/storage/       state root / artifact paths
internal/api/           local control API
pkg/client/             supported Go SDK
docs/                   user guide, acceptance, CI
.justfiles/             modular just recipes
.github/workflows/      hardware-free CI
```

Hollyland-specific code stays in `internal/driver/hollyland35470007`. The API
and recording packages must not import a concrete driver.

## Dependency updates

Dependabot opens weekly PRs for Go modules and GitHub Actions. Review diffs
for cgo-native and audio-related upgrades carefully; run `just check` and
`just test-race` on each. Prefer minimal version bumps over drive-by refactors.

## License of contributions

By contributing, you confirm you have the right to submit the work and agree
it is licensed under the [Apache License 2.0](LICENSE).
