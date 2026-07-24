# Contract fixtures (v1)

Golden JSON examples for Micbridge public contracts. These are API artifacts:
native capture and drivers must remain compatible with them.

## Update process

1. Change the Go types / encoders under `internal/` or `pkg/`.
2. Run `go test ./api/... -update` **only** when intentionally refreshing goldens
   (requires the `-update` flag in the fixture test package).
3. Review the diff of every changed file under `api/fixtures/`.
4. Document the rationale in the commit message (additive vs breaking).
5. Breaking changes require a schema version bump (`micbridge.*/vN`) — do not
   silently rewrite v1 goldens to hide breaks.

## Layout

| File | Content |
|---|---|
| `device.json` | Stable identity + surfaces + mixed capabilities |
| `binding.json` | Control-to-action binding with side effects |
| `event_control.json` | Ordered control event envelope |
| `event_gap.json` | `stream.gap` with cursor range |
| `recording_state.json` | Recording snapshot |
| `error.json` | Typed API error body |
| `status.json` | Daemon status envelope |
