# Test module — unit and race (hardware-free)

set working-directory := ".."

# Default package tests.
unit:
    go test ./...

# Race detector.
race:
    go test -race ./...

# Verbose package tests.
verbose:
    go test -v ./...

# Coverage profile written to coverage.out (gitignored).
cover:
    go test -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out | tail -n 1

# Deterministic parser/contract fuzz gate used by CI. A fixed execution budget
# avoids wall-clock deadline flakes while still exercising generated inputs.
fuzz-smoke:
    go test ./internal/core -run '^$' -fuzz '^FuzzDeviceJSONRoundTrip$' -fuzztime=100000x
    go test ./internal/driver/hollyland35470007 -run '^$' -fuzz '^FuzzEvaluateMatchClassification$' -fuzztime=100000x
