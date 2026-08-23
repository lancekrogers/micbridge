# Security and privacy policy

## Reporting a vulnerability

Do **not** open a public issue for security problems.

Use [GitHub private vulnerability reporting](https://github.com/lancekrogers/micbridge/security/advisories/new).
Include steps to reproduce. Redact serial numbers, private filesystem paths,
API tokens, and any audio content.

## Product security boundaries

Micbridge is designed as an ordinary-user local service:

- Default control plane is a per-user Unix-domain socket (mode `0600` in a
  `0700` directory). Loopback TCP is off unless explicitly configured.
- There is no root daemon and no requirement for the `input` group as a broad
  grant; device access must stay scoped and documented.
- Device connection is **not** consent to record. Capture requires an armed or
  confirmed policy and visible state.
- Clients must not open Micbridge-owned audio or input handles behind the
  daemon’s back.

## Sensitive data

Never commit or paste into issues:

- user or lab recordings (WAV/PCM) and partial artifacts;
- session journals that may embed paths or titles;
- device serials, topology dumps with personal paths;
- secrets, install tokens, or socket credentials;
- raw HID/USB captures that include non-public environments.

Diagnostics and fixtures must redact secrets, serials, private paths, and
audio. CI fixtures use synthetic PCM only (`internal/testsupport`).

## Scope notes

In-scope examples:

- local socket authorization and filesystem permissions;
- path traversal or artifact overwrite outside the state root;
- callback/path bugs that could leak audio to logs or subscribers as JSON;
- dependency issues in capture, HTTP, or crypto stacks.

Out of scope for product claims until separately evidenced:

- vendor USB write/firmware surfaces;
- host volume-key exclusive interception;
- remote/cloud control planes (none ship by default).

## Coordinated disclosure

Please allow reasonable time for a fix before public discussion. We will
acknowledge private reports and prefer patched releases over silent ignores.
