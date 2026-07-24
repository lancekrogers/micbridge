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
rm -rf "${MICBRIDGE_STATE_DIR:-$HOME/.local/state/micbridge}"
```
