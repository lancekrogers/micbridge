# HIL: LARK M2 two-double-press acceptance

**Optional.** Requires physical USB `3547:0007`. Not part of default CI.

## Evidence status (updated 2026-07-26)

- PASS: ordinary-user exact USB discovery and one grouped identity
- PASS: exact semantic Consumer Control evdev surface was readable
- PASS: the vendor-documented LARK M2 transmitter double press reproduced five
  HID `06 01 00` / `06 00 00` press/release pairs in one bounded exact-device
  Habitat window; single transmitter/receiver presses correctly remained
  host-silent noise-cancellation controls
- PASS: exact Hollyland malgo source selected; native 48 kHz/stereo/24-bit
  evidence remained separate from requested 16-bit WAV format
- PASS: bounded API arm/start/stop captured and validated a 67,244-byte WAV
- PASS: the shipped daemon was armed through its public CLI, the first physical
  LARK M2 transmitter double press produced `control.pressed` and transitioned
  the session from `armed` to `recording`, and the second produced another
  `control.pressed` and transitioned it to `completed`
- PASS: the finalized 67,559,084-byte artifact was independently identified by
  `file` and `ffprobe` as 351.870 seconds of RIFF/WAVE PCM signed 16-bit little
  endian, stereo, 48 kHz audio; its journal recorded 67,559,040 data bytes and
  a final CRC32
- PASS: a third press after completion was observed as press/release but did
  not mutate the completed session
- PASS: the private HIL audio was deleted after validation and was not added to
  the repository
- Tracking: campaign intent
  `micbridge-execute-and-record-the-20260724-155720` is now physically satisfied.

## Procedure

1. `micbridge serve` as ordinary user
2. `micbridge doctor` → all checks green
3. `micbridge devices` → exactly one grouped device
4. Enable LARK M2 TX double press → `recording.toggle`
5. Arm recording
6. Double-press either microphone → session recording
7. Double-press either microphone again → finalized WAV
8. Disconnect mid-record → partial + journal; reconnect same identity only

Steps 1–7 passed on 2026-07-26. Step 8 remains separately covered by the
deterministic disconnect/recovery suite; it was not repeated during this
privacy-sensitive live recording.

## Pass criteria

- One identity for audio+control
- TX double press documented in the UI; single press identified as noise cancellation
- Passive Volume-Up side effect declared
- WAV validates (`recording.ValidateWAV`)
- No silent fallback to another microphone
