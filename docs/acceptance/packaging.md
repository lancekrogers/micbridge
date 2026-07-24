# Packaging and permissions (v0.1)

## Install

```bash
just build
just install   # copies bin/micbridge to GOBIN
```

Or:

```bash
go install github.com/lancekrogers/micbridge/cmd/micbridge@latest
```

(cgo + ALSA headers required for malgo enumeration).

## Deterministic package candidate

On a Linux/amd64 host with the native build dependencies installed:

```bash
VERSION=0.1.0-rc1 just xrelease package-candidate
sha256sum --check dist/micbridge_0.1.0-rc1_linux_amd64.tar.gz.sha256
```

The archive contains `micbridge` plus a deterministic `BUILDINFO` manifest
with version, target, source commit, Go toolchain, and CGO mode. Tar ownership
and timestamps and the gzip header are normalized. Running the recipe twice
from the same source commit and toolchain must produce the same SHA-256.

This recipe is local-only: it never creates a Git tag, uploads an artifact, or
publishes a GitHub release. v0.1 supports only Linux/amd64 packaging because
the native audio path is CGO-backed and other targets have not been accepted.

## Runtime layout

| Path | Mode | Purpose |
|---|---|---|
| `$XDG_RUNTIME_DIR/micbridge/` | 0700 | Socket directory |
| `micbridge.sock` | 0600 | Local API |
| `$MICBRIDGE_STATE_DIR/recordings/` | 0700 | Create-only WAVs |
| `$MICBRIDGE_STATE_DIR/journals/` | 0700 | Session journals |

## Permissions principles

- Ordinary user process only
- No root daemon
- No world-writable device nodes
- Prefer seat-scoped `uaccess` udev tags limited to VID:PID 3547:0007
- See `hollyland35470007.UdevHint()` / doctor output

## Uninstall

```bash
rm "$(go env GOPATH)/bin/micbridge"
```

Uninstalling the binary intentionally preserves the state directory and every
recording. To purge state, first copy any wanted WAV files elsewhere, stop the
daemon, inspect the resolved path, and remove it as a separate explicit action:

```bash
state="${MICBRIDGE_STATE_DIR:-$HOME/.local/state/micbridge}"
printf 'State to purge (includes recordings): %s\n' "$state"
# After review, run manually:
# rm -rf -- "$state"
```
