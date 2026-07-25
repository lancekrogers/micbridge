# HIL: Hollyland two-press acceptance

**Optional.** Requires physical USB `3547:0007`. Not part of default CI.

## Evidence status (2026-07-24 review)

- PASS: ordinary-user exact USB discovery and one grouped identity
- PASS: correlated evdev control surface was readable
- PASS: exact Hollyland malgo source selected; native 48 kHz/stereo/24-bit
  evidence remained separate from requested 16-bit WAV format
- PASS: bounded API arm/start/stop captured and validated a 67,244-byte WAV
- PENDING: a human must perform the final two physical receiver presses through
  the shipped daemon. The procedure below is not recorded as executed evidence.
- Tracking: ready campaign intent
  `micbridge-execute-and-record-the-20260724-155720`.

## Procedure

1. `micbridge serve` as ordinary user
2. `micbridge doctor` → all checks green
3. `micbridge devices` → exactly one grouped device
4. Enable primary press → `recording.toggle`
5. Arm recording
6. Press receiver button → session recording
7. Press again → finalized WAV
8. Disconnect mid-record → partial + journal; reconnect same identity only

## Pass criteria

- One identity for audio+control
- Passive Volume-Up side effect declared
- WAV validates (`recording.ValidateWAV`)
- No silent fallback to another microphone
