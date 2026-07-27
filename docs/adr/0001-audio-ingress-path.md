# ADR 0001: Audio ingress path (direct Go vs C SPSC ring)

Status: **Accepted** (2026-07-24)

Amended: **2026-07-26** after the first user playback exposed audible crackle.

## Context

Micbridge captures PCM via malgo/miniaudio. The design requires starting with a
direct miniaudio→Go callback that copies fixed frames into a preallocated
bounded queue, escalating to a C-owned SPSC ring only if measured failure remains
after minimizing the Go handoff.

## Decision

**Retain the direct Go SPSC ingress** implemented in `internal/audio.Ingress`
(`PushFromCallback` + atomic ring). Do **not** introduce a C ring for v0.1.

For recording, request the measured native 48 kHz/stereo/24-bit shape instead
of real-time 16-bit conversion, use miniaudio's conservative profile with four
20 ms periods, and accept bounded variable callback chunks. Track accepted and
dropped frames; any drop makes finalization fail visibly and preserves the
partial artifact.

## Predeclared thresholds

| Metric | Pass criteria | Unit |
|---|---|---|
| Steady-state drops (synthetic producer, capacity≥8, no artificial stall) | 0 | frames |
| Overflow under intentionally slow drain | counted, non-blocking, no hang | drops > 0 allowed |
| Queue memory | fixed at construction | frames × frameBytes |
| Race detector on producer+consumer | clean | go test -race |
| Shutdown | drain worker exits after Close+cancel | ≤2s |
| Callback work | no alloc/log/IO/fan-out in push path | code review |

## Evidence (this workspace)

Commands:

```bash
go test ./internal/audio/ -count=1 -race
go test ./internal/audio/ -run 'Ingress|Health' -count=3
```

Host: Linux amd64, Go 1.26.x, cgo available.

Results:

- `TestIngressBoundedOverflow` — full queue drops newest; counter increments.
- `TestIngressSlowWriterStress` — slow drain produces overflow without hang;
  queue length stays ≤ capacity; `-race` clean after SPSC rewrite.
- `TestIngressCleanShutdown` — worker exits after close/cancel.
- Health tests inject stalls/gaps/disconnect and assert level transitions.
- `OpenNativeCapture` copies bounded callback chunks into the same ingress;
  all WAV/file work runs on the drain worker.
- A bounded ordinary-user run against the attached `3547:0007` source
  negotiated native 48 kHz/stereo/24-bit input to requested 16-bit PCM and
  produced a validated 67,244-byte WAV.

No reproducible deadline miss or callback drop was observed in synthetic loads
or the bounded real callback run. Therefore a C ring is not justified.

The initial 16-bit HIL remains valid historical transition evidence, but it is
not sufficient audio-quality evidence. The current native 24-bit path requires
a fresh bounded listening check before release.

## Consequences

- Go owns the ingress ring, overflow policy, and health counters.
- A future C ring may only own fixed PCM slots + counters if a **named**
  threshold fails on a target platform after profiling; it must not own
  devices, sessions, files, API, or policy.
- Decision reopens if: race-free stress still drops under real malgo callbacks
  at 48 kHz stereo with a real writer, or sanitizer builds show ordering bugs.

## Alternatives considered

1. **C SPSC ring now** — premature; adds cgo surface without measured failure.
2. **Go channels from callback** — rejected (can block/allocate under load).
