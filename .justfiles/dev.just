# Dev module — local helpers (no host package installs)

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

# Hardware lab helpers are opt-in and named explicitly (not implemented yet).
[doc('Placeholder: requires attached Hollyland 3547:0007 — not for CI')]
hil-smoke:
    @echo "xdev::hil-smoke is intentionally unimplemented in bootstrap"
    @echo "Attach hardware and use phase 004 recipes when available"
    @exit 1

[private]
ensure-bin:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ ! -x "{{bin_dir}}/{{binary_name}}" ]]; then
      just build
    fi
