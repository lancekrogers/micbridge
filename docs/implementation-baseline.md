# Micbridge implementation baseline

Work item: **WI-66237e** (Programmable wireless microphone API)  
Festival: **MH0001** (`micbridge-hollyland-recording-v1`)  
Design: `workflow/design/programmable-wireless-mic-api/`  
Evidence: `workflow/explore/hollyland-wireless-mic-usb-investigation/`

This document is the decision index for repository bootstrap. It does not
re-derive Hollyland measurements or implement product behavior.

## Repository inventory (2026-07-24)

| Item | Value |
|---|---|
| Path | `projects/micbridge` (git submodule of lance-arch) |
| Default branch | `main` @ `0c97481` (“Initial commit”) |
| Remote | `https://github.com/lancekrogers/micbridge.git` |
| Working tree | Clean; only `README.md` present before bootstrap |
| Current files | `README.md` (one-line project blurb) |
| Host | Linux `archdtop` / `x86_64` (Linux is the first supported host) |

### Tool versions (host)

| Tool | Version / path |
|---|---|
| Go | `go1.26.5-X:nodwarf5` (`/usr/bin/go`) |
| just | `1.56.0` |
| golangci-lint | `v1.64.8` |
| git | `2.55.0` |
| ffmpeg | present (`linuxbrew`) |
| arecord | present |
| pactl | present |
| sox | not installed |
| pkg-config / ALSA | present; `alsa` package available |
| malgo (cached) | `v0.11.24`, `v0.11.25` under module cache |

No product source, module, CI, or package layout exists yet. Next tasks own
bootstrap; this task only freezes design inputs.

## Selected design decisions

### Language and module

| Decision | Selection | Source |
|---|---|---|
| Implementation language | **Go** (service/API/daemon) | `language-decision.md` (Accepted) |
| Audio capture | **miniaudio via `github.com/gen2brain/malgo`** | `language-decision.md`, `architecture.md` |
| Module path | **`github.com/lancekrogers/micbridge`** | origin remote; peer pattern (`samantha`) |
| Go version (module) | **`1.26`** (toolchain matches host ≥1.26.1 peers) | host + `samantha` `go 1.26.1` |
| cgo | Required for malgo/miniaudio; per-target builders, not pure `GOOS/GOARCH` | architecture platform plan |

### Process and product boundaries

- One ordinary-user daemon + CLI/reference client; **no root daemon**.
- Drivers are **compiled in** for v1 (no dynamic plugins / shared-object load).
- **Habitat** is evidence-only; not a runtime dependency.
- **Samantha** is a future public-API client only; no Samantha types in schemas.
- Clients never open Micbridge-owned audio or input handles.

### Hollyland evidence boundary (do not reinterpret)

Measured and in-scope for v0.1 first driver:

- Exact USB identity **`3547:0007`** plus measured descriptor shape
- Two-channel **48 kHz** USB Audio input
- Receiver HID press/release → Linux `EV_KEY` / `KEY_VOLUMEUP` (115)
- Vendor bulk endpoints present with **unknown** semantics (no product writes)

Not proven / must stay unavailable until Habitat evidence:

- Transmitter noise-cancellation buttons as host-visible controls
- Exact retail model name as a support claim (model remains unresolved)
- Vendor USB writes, firmware, pairing, or private-radio decoding
- Automatic Volume-Up suppression (declare passive side effect only)

Identity rule: never persist ALSA indexes, `/dev/input/eventN`, USB bus/device
numbers, or hidraw paths as stable identity.

## Package map (contract → intended package)

From `architecture.md`. Product packages must not import a concrete driver.

| Contract / concern | Intended package |
|---|---|
| CLI + daemon entry | `cmd/micbridge/` |
| Lifecycle / DI wiring | `internal/service/` |
| Stable IDs, capabilities, registry | `internal/device/` |
| Compiled driver registry | `internal/driver/` |
| Exact first device profile | `internal/driver/hollyland35470007/` |
| miniaudio / malgo capture | `internal/platform/audio/` |
| Linux evdev (+ later OS adapters) | `internal/platform/input/` |
| Hotplug / reacquisition | `internal/platform/hotplug/` |
| Control → action bindings | `internal/binding/` |
| Session SM, WAV writer, journal, recovery | `internal/recording/` |
| Bounded event fan-out + replay cursor | `internal/eventbus/` |
| Transport-independent HTTP handlers | `internal/api/` |
| Supported Go SDK | `pkg/client/` |
| Schemas / OpenAPI artifacts | `api/` |
| Narrow C/ObjC/Win32 shims only when measured | under `internal/platform/` (ownership-explicit) |

### Capability surface (versioned)

`audio.capture`, `control.press`, `control.release`, `control.hold`,
`recording.trigger`, `status.battery`, `status.link`, `setting.noise_cancellation`

Each carries `available`, `mode`, `side_effects`, `evidence`, and driver version.
Unavailable capabilities remain queryable with an evidence-linked reason.

### Local API (v1 transport)

- Unix socket: `$XDG_RUNTIME_DIR/micbridge/micbridge.sock` (dir `0700`, sock `0600`)
- Resources: `/v1/status`, `/v1/devices`, bindings, recordings, `/v1/events` (SSE)
- No PCM or audio content in JSON
- Mutations accept `Idempotency-Key`

### Audio ingress policy

Default: miniaudio C callback → minimal malgo/Go handoff → preallocated bounded
Go queue → workers.

**Never** allocate, block, log, write files, encode JSON, or fan out subscribers
from the native audio callback.

C-owned SPSC ring is **fallback only** after reproducing direct-callback failure
and proving the fix with the identical test suite (`test-plan.md` / language ADR).

### Determinism and tests (bootstrap expectations)

Next sequences must introduce:

- Fake clock / ID / filesystem / synthetic audio seams
- Fake driver exercising the same interfaces as production drivers
- Hardware-free CI for the full workflow; HIL opt-in and bounded

## Native and host dependencies

| Dependency | Role | Notes |
|---|---|---|
| `github.com/gen2brain/malgo` | Capture via miniaudio | cgo; start with direct callback |
| C toolchain | Build malgo/miniaudio | Required on release builders |
| ALSA / PipeWire / Pulse | Linux audio backend via miniaudio | Host stack present on this machine |
| Linux evdev | Receiver control events | Exact library TBD (see open decisions) |
| udev/netlink (or bounded poll) | Hotplug | Mechanism TBD |
| just + modular `.justfiles/` | Dev workflows | Match campaign peers (`obey`) |
| golangci-lint, race detector | Quality gates | Host already has golangci-lint |

## Festival / campaign alignment

| Layer | Location |
|---|---|
| Festival | `festivals/active/micbridge-hollyland-recording-v1-MH0001/` |
| Linked project | `projects/micbridge` |
| Phase order | BOOTSTRAP → CORE CONTRACTS → AUDIO → HOLLYLAND → UX → ACCEPTANCE |
| Physical hardware | Not required before phase `004_HOLLYLAND_DRIVER` |

## Decisions recorded for bootstrap

1. **Module path:** `github.com/lancekrogers/micbridge`
2. **Go directive:** `1.26` (align with current toolchain / peer projects)
3. **Language:** Go + malgo; C only behind ownership-explicit shims
4. **Package layout:** adopt architecture map above without renaming contracts
5. **First driver identity:** exact `3547:0007` + descriptor match only
6. **Privacy defaults:** local socket, ordinary user, no audio in logs/fixtures
7. **No product code in this task:** baseline only

## Unresolved decisions (phase-gated; do not block receiver slice)

From `implementation-plan.md` — still require implementation evidence:

| Topic | Status |
|---|---|
| SSE alone vs SSE + binary PCM stream | Open |
| Exact Linux evdev library and hotplug mechanism | Open |
| Narrow exclusive Volume-Up interception reliability | Deferred; passive side effect is default |
| Default recording sample format / optional mono downmix | Open (evidence implies 48 kHz stereo capture) |
| Installation format and service manager support | Open |
| Second device for generalization gate | Open |
| TUI vs smaller status/recorder interface | Open |

## Assumptions and follow-ups

| Item | Classification |
|---|---|
| Module path matches GitHub remote org/user | **Decision** (above) |
| `go 1.26` module line (not pin to patch) | **Decision**; toolchain may float with host |
| Peer `just` modular layout under `.justfiles/` | **Assumption** for tooling task; confirm with bootstrap sequence |
| No existing user changes to preserve beyond clean `README.md` | **Measured** inventory |
| Exact retail Hollyland model string | **Unresolved**; do not invent |
| Transmitter NC host visibility | **Unsupported** until Habitat evidence |
| Direct malgo callback adequacy | **Measured later** in AUDIO phase; C-ring only if failed |

## Next task can proceed without rediscovery

Bootstrap may:

1. `go mod init github.com/lancekrogers/micbridge` with `go 1.26`
2. Scaffold the package map directories and `cmd/micbridge`
3. Add modular just workflows, CI, and deterministic test seams
4. Keep Hollyland and native capture behind the interfaces above

Do not invent capabilities beyond measured evidence. Do not couple API/recording
packages to `hollyland35470007`.
