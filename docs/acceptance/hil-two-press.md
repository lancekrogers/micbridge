# HIL: Hollyland two-press acceptance

**Optional.** Requires physical USB `3547:0007`. Not part of default CI.

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
