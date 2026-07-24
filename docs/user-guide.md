# Micbridge user guide (v0.1)

## What you need

- Linux host with ordinary-user permissions
- Optional: Hollyland USB receiver **3547:0007**
- `micbridge` installed (`just install` or `go install`)

Hardware is not required to explore the fake driver path.

## Five-minute path (fake / no hardware)

```bash
# terminal 1
export MICBRIDGE_STATE_DIR=$PWD/.micbridge-demo
export MICBRIDGE_SOCK=$MICBRIDGE_STATE_DIR/micbridge.sock
micbridge serve

# terminal 2
export MICBRIDGE_STATE_DIR=$PWD/.micbridge-demo
export MICBRIDGE_SOCK=$MICBRIDGE_STATE_DIR/micbridge.sock
micbridge doctor --json
micbridge devices --json
micbridge bindings set bind1 dev_fake_1 primary recording.toggle
micbridge record
micbridge stop
```

## Physical receiver (3547:0007)

1. Plug in the receiver.
2. Run `micbridge doctor` and fix any path/permission hints (udev `uaccess` for this VID:PID only).
3. Confirm the device appears once under a stable ID (not ALSA card numbers).
4. Enable the primary-press → recording.toggle binding.
5. Arm/start via UI/CLI/API, then use the **receiver button** to toggle.

**Side effect:** the receiver button is also Consumer Control Volume Up on the
host. Micbridge declares this passively and does not exclusive-grab by default.

## Recovery

Interrupted sessions leave `*.wav.partial` under the state recordings directory
plus a `.journal`. Run doctor/status and use recover guidance; never delete
partials until you have reviewed them.

## Privacy

- Local Unix socket only by default
- No cloud upload
- Do not share recordings, serials, or private paths in bug reports
