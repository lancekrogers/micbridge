# Dev module — local helpers (no host package installs)

set working-directory := ".."

state_dir := env_var_or_default("MICBRIDGE_STATE_DIR", ".micbridge")
bin_dir := "bin"
binary_name := "micbridge"

# Run version against the built binary.
version: ensure-bin
    "./{{bin_dir}}/{{binary_name}}" version

# Run status; override with MICBRIDGE_STATE_DIR.
status: ensure-bin
    MICBRIDGE_STATE_DIR="{{state_dir}}" "./{{bin_dir}}/{{binary_name}}" status

# Print the configured state directory layout (does not create recordings).
print-state:
    @echo "MICBRIDGE_STATE_DIR={{state_dir}}"
    @echo "recordings: {{state_dir}}/recordings"
    @echo "journals:   {{state_dir}}/journals"

# Hardware lab helper is opt-in and never part of the ordinary test target.
[doc('Requires one attached Hollyland 3547:0007 — not for normal CI')]
hil-smoke:
    MICBRIDGE_HIL=1 go test ./internal/driver/hollyland35470007 \
      -run '^TestOptInLiveDiscoveryMatchesProfile$' -count=1 -v

[private]
ensure-bin:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ ! -x "{{bin_dir}}/{{binary_name}}" ]]; then
      just build
    fi
