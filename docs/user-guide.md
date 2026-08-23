# Micbridge user guide

Install notes live in the [README](../README.md). This page is the recording
workflow.

## What you need

- Linux host, ordinary-user permissions (no root daemon)
- Optional: Hollyland LARK M2 USB-C receiver **3547:0007**
- `micbridge` on your `PATH` (`just install` or `go build`)

Hardware is not required to explore the fake driver path.

## Record with a LARK M2

1. Plug in the receiver.
2. Run `micbridge doctor` and fix any path/permission hints.
3. Confirm the device appears once under a stable ID (not ALSA card numbers):

   ```bash
   micbridge serve          # terminal 1
   micbridge devices --json # terminal 2
   ```

4. Run `micbridge setup` if you want the guided binding plan. The default
   primary-press binding is complete only when audio and control are callable.
5. Run `micbridge` (or `micbridge ui`) and arm once with `a`, then `y`.
6. **Double-press the button on either LARK M2 microphone** to start, then
   double-press either microphone again to stop.
7. The completed screen shows the finalized WAV. The next double-press starts
   another recording without re-arming.

One press only toggles LARK M2 noise cancellation and does not send a host
control event.

**Side effect:** the transmitter double-press is also Consumer Control Volume
Up on the desktop. Micbridge declares this passively and does not exclusive-grab
the device by default.

### Device access (udev)

If `doctor` cannot open the audio or control nodes, install a **device-scoped**
`uaccess` rule for this VID:PID only, then replug the receiver:

```udev
# Example only — review before installing. Scope to 3547:0007.
SUBSYSTEM=="sound", ATTRS{idVendor}=="3547", ATTRS{idProduct}=="0007", TAG+="uaccess"
SUBSYSTEM=="input", ATTRS{idVendor}=="3547", ATTRS{idProduct}=="0007", TAG+="uaccess"
```

Do not add a root daemon, do not grant the whole `input` group, and do not
leave device nodes world-writable. `micbridge doctor` prints this hint when
access fails.

## Five-minute path (no hardware)

```bash
# terminal 1
export MICBRIDGE_STATE_DIR=$PWD/.micbridge-demo
export MICBRIDGE_SOCK=$MICBRIDGE_STATE_DIR/micbridge.sock
MICBRIDGE_FAKE=1 micbridge serve

# terminal 2
export MICBRIDGE_STATE_DIR=$PWD/.micbridge-demo
export MICBRIDGE_SOCK=$MICBRIDGE_STATE_DIR/micbridge.sock
micbridge doctor --json
micbridge devices --json
micbridge setup
micbridge ui
# or script it:
micbridge record arm
micbridge record
micbridge stop
```

## Recording location

The default state root does not depend on the directory where the daemon was
started:

```text
$XDG_STATE_HOME/micbridge
# or, when XDG_STATE_HOME is unset:
$HOME/.local/state/micbridge
```

Final WAV files are under `recordings/`; recovery journals are under
`journals/`. Journals identify whether start and stop came from the microphone
binding, CLI, API, or system. Both the status API and TUI show the resolved
absolute path.

Override the root with an absolute `MICBRIDGE_STATE_DIR` when needed. Older
development runs that explicitly used `.micbridge` remain in that directory
and are not moved or deleted.

The live Hollyland path records its measured native 48 kHz, stereo, 24-bit PCM
shape. Capture buffering favors continuity over monitoring latency, and any
dropped callback frames fail the session visibly instead of publishing a
damaged WAV as completed.

## Recovery

Interrupted sessions leave `*.wav.partial` under the recordings directory plus
a `.journal`. Run `micbridge recover --json` to classify them. Do not delete
partials until you have reviewed them.

## TUI keys

- `Enter` / `Space`: choose the safe action for the current state
- `a`: arm once after daemon startup or failure recovery
- `y`: confirm a mutation
- `n` / `Esc`: cancel a pending mutation
- `r`: refresh
- `q`: quit

## Privacy

- Local Unix socket only by default (`0600` in a `0700` directory)
- No cloud upload
- Do not share recordings, serials, or private paths in bug reports

See [SECURITY.md](../SECURITY.md) for reporting rules.
