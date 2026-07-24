# Bounded Hollyland native-capture evidence — 2026-07-24

Scope: ordinary-user discovery, source correlation, native callback ingress,
and WAV finalization. This is not the still-pending human two-press result.

## Sanitized environment

- Linux amd64
- USB profile: `3547:0007`
- One exact receiver grouped as one Micbridge device
- Control surface: one readable evdev node (raw host path omitted)
- Capture source: `Wireless microphone Analog Stereo`
- Native source evidence: 48 kHz, stereo, 24-bit
- Requested/public WAV format: 48 kHz, stereo, 16-bit PCM

## Executed workflow

```text
micbridge serve --json
micbridge devices --json
micbridge sources --json
micbridge record arm --json
micbridge record --json
# bounded 350 ms capture
micbridge stop --json
file <temporary-state>/recordings/rec_1.wav
```

## Result

- Device grouping: PASS
- Explicit source selection (no default fallback): PASS
- Arm → recording → completed state: PASS
- Final artifact size: 67,244 bytes
- Independent file identification: RIFF/WAVE, Microsoft PCM, 16-bit, stereo,
  48 kHz
- `recording.ValidateWAV`: covered by the same production finalization path and
  deterministic integration regression
- Audio retention: the temporary HIL WAV was not added to the repository

The first attempted live capture exposed a cgo device-ID ownership panic. The
review fix now allocates the selected malgo device ID in C memory for
`InitDevice`, frees it immediately after initialization, and the repeated
bounded run passed. This failure was not suppressed.
