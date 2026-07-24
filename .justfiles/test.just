# Test module — unit and race (hardware-free)

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
