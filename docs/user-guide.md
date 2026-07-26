# Micbridge user guide (v0.1)

## What you need

- Linux host with ordinary-user permissions
- Optional: Hollyland LARK M2 USB-C receiver **3547:0007**
- `micbridge` installed (`just install` or `go install`)

Hardware is not required to explore the fake driver path.

## Five-minute path (fake / no hardware)

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

## Physical LARK M2 receiver (3547:0007)

1. Plug in the receiver.
2. Run `micbridge doctor` and fix any path/permission hints (udev `uaccess` for this VID:PID only).
3. Confirm the device appears once under a stable ID (not ALSA card numbers).
4. Run `micbridge setup`; the default primary-press binding is complete only
   when audio/control capabilities are callable.
5. Run `micbridge ui` (or `micbridge record arm`). **Double-press the button on
   either LARK M2 microphone** to start, then double-press either microphone
   again to stop.

One press only toggles LARK M2 noise cancellation and does not send a host
control event.

**Side effect:** the transmitter double press is also Consumer Control Volume
Up on the host. Micbridge declares this passively and does not exclusive-grab
by default.

## Recovery

Interrupted sessions leave `*.wav.partial` under the state recordings directory
plus a `.journal`. Run `micbridge recover --json` to classify them; never
delete partials until you have reviewed them.

## TUI keys

- `Enter` / `Space`: choose the safe action for the current state
- `a`: arm
- `y`: confirm a mutation
- `n` / `Esc`: cancel a pending mutation
- `r`: refresh
- `q`: quit

## Privacy

- Local Unix socket only by default
- No cloud upload
- Do not share recordings, serials, or private paths in bug reports
