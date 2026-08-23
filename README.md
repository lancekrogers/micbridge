# Micbridge

[![CI](https://github.com/lancekrogers/micbridge/actions/workflows/ci.yml/badge.svg)](https://github.com/lancekrogers/micbridge/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](go.mod)

Record from a wireless microphone onto your Linux machine. Start and stop with the transmitter button, a terminal UI, or a local API. Audio never leaves the box.

The first supported receiver is the Hollyland **LARK M2** USB-C dongle at USB **`3547:0007`**.

![Micbridge recorder TUI](docs/vhs/tui.gif)

## Status

Early public **v0.1**. Linux only. One proven device. The binary still reports `0.0.0-dev`; there is no tagged GitHub release yet.

| Works today | Not in this release |
|---|---|
| Discover the exact LARK M2 USB-C receiver | macOS / Windows |
| Double-press either transmitter to start/stop a WAV | Other Hollyland receivers or brands |
| Recoverable recordings, doctor, CLI, TUI | Host control of noise cancellation |
| Hardware-free fake driver (`MICBRIDGE_FAKE=1`) | Distro packages / Homebrew |
| Local Unix-socket API and Go client | Cloud, accounts, or remote control |

## Install

Linux x86_64, **Go 1.26+**, a C compiler, and ALSA headers. Capture uses miniaudio via cgo.

```bash
# Debian / Ubuntu
sudo apt-get install --yes build-essential pkg-config libasound2-dev

# Arch
sudo pacman -S --needed base-devel alsa-lib
```

From source ([just](https://github.com/casey/just) optional):

```bash
git clone https://github.com/lancekrogers/micbridge.git
cd micbridge
just install    # builds, copies to GOBIN, prints the path
```

Without just:

```bash
go build -o bin/micbridge ./cmd/micbridge
install -m 755 bin/micbridge "$(go env GOPATH)/bin/micbridge"
```

`go install github.com/lancekrogers/micbridge/cmd/micbridge@latest` works the same way once you have the C toolchain and ALSA headers.

## Record with a LARK M2

1. Plug in the USB-C receiver.
2. Run `micbridge doctor` and apply the device-scoped udev hint if access fails. Do not add a root daemon or join the `input` group.
3. Start the daemon, then the UI:

```bash
# terminal 1
micbridge serve

# terminal 2
micbridge          # same as: micbridge ui
```

4. Arm once (`a`, then `y`).
5. **Double-press either transmitter** to start. Double-press again to stop.
6. After a take finishes, the next double-press starts another recording. No re-arm.

Finished files are 48 kHz / stereo / 24-bit WAV under:

```text
$XDG_STATE_HOME/micbridge/recordings
# or, if XDG_STATE_HOME is unset:
$HOME/.local/state/micbridge/recordings
```

A **single** press on a LARK M2 only toggles onboard noise cancellation and never reaches the host. The double-press also sends **Volume-Up** to the desktop; Micbridge reports that side effect and does not swallow the key.

Full walkthrough, TUI keys, and recovery: [docs/user-guide.md](docs/user-guide.md).

## Try it without hardware

```bash
# terminal 1
MICBRIDGE_FAKE=1 micbridge serve

# terminal 2
micbridge ui
```

Or `just demo` for a one-shot version/status smoke in a temp directory.

## Commands

| Command | What it does |
|---|---|
| `micbridge serve` | Local daemon (physical devices by default) |
| `micbridge` / `ui` | Interactive recorder |
| `micbridge doctor` | Permissions and device hints |
| `micbridge devices` | List discovered devices |
| `micbridge setup` | Guided binding/setup plan |
| `micbridge record arm` / `record` / `stop` | Scripted recording |
| `micbridge recover` | Classify interrupted sessions |
| `micbridge events` | Stream daemon events |
| `micbridge status` / `version` | Health and version |

Add `--json` (or `MICBRIDGE_JSON=1`) for machine-readable output.

## Go client

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/lancekrogers/micbridge/pkg/client"
)

func main() {
	sock := os.Getenv("MICBRIDGE_SOCK")
	if sock == "" {
		sock = os.Getenv("XDG_RUNTIME_DIR") + "/micbridge/micbridge.sock"
	}
	c := client.New(sock)
	status, err := c.Status(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(status["version"], status["recordings_dir"])
}
```

The daemon listens on a per-user Unix socket (`0600` in a `0700` directory). There is no default TCP listener.

## Contributing

Development setup, CI recipes, and PR rules: [CONTRIBUTING.md](CONTRIBUTING.md).
Please follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Docs

| Doc | For |
|---|---|
| [User guide](docs/user-guide.md) | Recording, recovery, TUI keys, udev |
| [Packaging](docs/acceptance/packaging.md) | Install layout and local package candidate |
| [Contributing](CONTRIBUTING.md) | Dev setup, CI recipes, PR rules |
| [Security](SECURITY.md) | Vulnerability reporting and privacy |
| [CI](docs/ci.md) | What GitHub Actions runs |

## License

[Apache License 2.0](LICENSE).
