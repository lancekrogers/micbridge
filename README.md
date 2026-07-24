# micbridge

Programmable wireless microphone controls and local recording API.

Micbridge is a local-first Go service: it discovers supported USB receivers,
exposes a versioned device-independent API, and records recoverable WAV
sessions. The first evidence-backed driver targets Hollyland USB **`3547:0007`**
on Linux. The product contract stays generic—no Habitat runtime dependency and
no Samantha-specific schema.

**Status:** repository bootstrap (v0.0.0-dev). Fake/status paths work without
hardware; native capture and the Hollyland driver land in later phases.

## Supported host (v0.1 focus)

| Surface | v0.1 |
|---|---|
| OS | **Linux** (primary; CI on Ubuntu 24.04) |
| Go | version in `go.mod` (**1.26+**) |
| Task runner | [just](https://github.com/casey/just) |
| Audio (upcoming) | miniaudio via `gen2brain/malgo` (cgo) |
| Control (upcoming) | Linux evdev under USB parent grouping |
| Transport (upcoming) | per-user Unix socket under `$XDG_RUNTIME_DIR` |

macOS/Windows schemas may appear early; support is claimed only after
platform-specific evidence.

## Quick start (no hardware)

```bash
git clone https://github.com/lancekrogers/micbridge.git
cd micbridge

# Optional Linux packages (required once malgo/cgo capture is linked; CI installs them)
# Debian/Ubuntu:
#   sudo apt-get install --yes build-essential pkg-config libasound2-dev
# Arch:
#   sudo pacman -S --needed base-devel alsa-lib

just bootstrap   # go version + go mod tidy
just check       # gofmt check, go vet, unit tests
just build       # writes bin/micbridge
just demo        # version + status against a temporary state directory
```

Expected smoke output (version string may change):

```text
micbridge 0.0.0-dev
{
  "healthy": true,
  "journals_dir": "/tmp/micbridge-demo..../journals",
  "name": "micbridge",
  "recordings_dir": "/tmp/micbridge-demo..../recordings",
  "state_dir": "/tmp/micbridge-demo....",
  "version": "0.0.0-dev"
}
```

CLI without just:

```bash
go test ./...
go build -o bin/micbridge ./cmd/micbridge
MICBRIDGE_STATE_DIR=$(mktemp -d) ./bin/micbridge version
MICBRIDGE_STATE_DIR=$(mktemp -d) ./bin/micbridge status
```

### Common dependency failures

| Symptom | Fix |
|---|---|
| `just: command not found` | Install just; or use the `go` commands above |
| Wrong Go version | Install Go ≥ `go.mod`; `go version` should report 1.26+ |
| `cgo: C compiler "gcc" not found` (later capture builds) | Install `build-essential` / `base-devel` |
| ALSA / miniaudio link errors (later) | Install `libasound2-dev` or `alsa-lib` + pkg-config |
| `gofmt needed on:` from `just check` | Run `gofmt -s -w .` |
| golangci-lint missing | `just review` falls back to vet; install lint for full parity |

## Developer commands

| Recipe | Purpose |
|---|---|
| `just bootstrap` | Toolchain check + module tidy |
| `just build` | Build `bin/micbridge` |
| `just check` | Format gate + vet + unit tests |
| `just test` / `just test-race` | Tests (hardware-free) |
| `just demo` | Fake status demo in a temp state dir |
| `just review` | Lint + race (CI review gate) |
| `just install` | Install to `GOBIN` |

CI runs the same recipes. Details: [docs/ci.md](docs/ci.md).

## What is real vs fake today

| Path | Needs hardware? | Notes |
|---|---|---|
| `micbridge version` / `status` | No | Bootstrap shells; temp or `MICBRIDGE_STATE_DIR` |
| Unit / race tests | No | Deterministic clocks, IDs, synthetic PCM |
| Native audio capture tests | No device, later cgo | Phase 003; uses malgo + synthetic sinks |
| Physical Hollyland two-press | **Yes** | Phase 004+; opt-in `just xdev hil-*` only |

Never wire HIL into `just check`. Physical validation is bounded and optional.

## Repository layout

```text
cmd/micbridge/          CLI entry (version, status, …)
internal/core/          shared product constants/contracts
internal/driver/        compiled-in driver registry boundary
internal/event/         ordered event bus boundary
internal/binding/       control → action mapping
internal/recording/     session / recovery boundary
internal/audio/         capture boundary (malgo later)
internal/storage/       state root / artifact paths
internal/api/           local control API handlers
internal/clock|idgen|queue/   injectable seams
internal/testsupport/   deterministic test helpers (tests only)
docs/                   baseline, CI notes
.justfiles/             modular just recipes
.github/workflows/      hardware-free CI
```

Package boundaries follow design WI-66237e. Hollyland-specific code must not
enter the generic core; API and recording packages do not import a concrete
driver. See [docs/implementation-baseline.md](docs/implementation-baseline.md).

## Generated and runtime files (do not commit)

| Path / pattern | Purpose |
|---|---|
| `bin/`, `dist/`, `micbridge` | build outputs |
| `.micbridge/`, `state/` | local state roots |
| `recordings/`, `journals/`, `*.wav`, `*.journal` | capture artifacts |
| `coverage.out` | test coverage |
| `*.sock` | local sockets |

`.gitignore` already covers these. **Never** commit user recordings, serials,
or live audio—only synthetic/licensed fixtures.

## Evidence and privacy

- Match Hollyland only as USB **`3547:0007`** plus measured descriptors.
- Exact retail model remains unresolved; do not claim family-wide support.
- Transmitter / noise-cancellation controls stay unavailable until Habitat
  produces repeatable host evidence.
- Receiver Volume-Up is a declared passive side effect, not silent exclusive
  grab.
- See [SECURITY.md](SECURITY.md) for reporting and data-handling rules.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for PR expectations, dependency updates,
and how to reproduce CI failures locally.

## Design references (campaign)

When working from the lance-arch campaign tree:

- Design: `workflow/design/programmable-wireless-mic-api/` (WI-66237e)
- Evidence: `workflow/explore/hollyland-wireless-mic-usb-investigation/`
- Festival: `festivals/active/micbridge-hollyland-recording-v1-MH0001/`
