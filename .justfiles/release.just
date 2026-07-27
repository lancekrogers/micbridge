# Release module — packaging scaffolding (no publish by default)

set working-directory := ".."

bin_dir := "bin"
binary_name := "micbridge"
gobin := env_var_or_default("GOBIN", `go env GOPATH` + "/bin")
commit := `git rev-parse --short=12 HEAD`
version := env_var_or_default("VERSION", "0.0.0-dev+" + commit)

# Install binary into GOBIN (delegates to root recipe).
install-bin:
    just install

# Print release metadata that would be stamped (does not tag or push).
info:
    @echo "binary={{ binary_name }}"
    @echo "version={{ version }}"
    @echo "module=$(go list -m)"
    @echo "go=$(go env GOVERSION)"
    @echo "NOTE: do not publish a release/tag unless a festival task authorizes it"

# Build a deterministic Linux/amd64 candidate archive and checksum. This never
# creates a tag, GitHub release, or upload.
[doc('Build a deterministic local Linux/amd64 candidate and SHA-256 checksum')]
package-candidate:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "$(go env GOHOSTOS)/$(go env GOHOSTARCH)" != "linux/amd64" ]]; then
      echo "package-candidate: Linux/amd64 host required for the native CGO candidate"
      exit 1
    fi
    if [[ ! "{{ version }}" =~ ^[0-9A-Za-z._-]+$ ]]; then
      echo "package-candidate: invalid VERSION={{ version }}"
      exit 1
    fi
    if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then
      echo "package-candidate: commit or stash source changes before stamping provenance"
      exit 1
    fi
    root="$(pwd)"
    dist="$root/dist"
    stage="$(mktemp -d "${TMPDIR:-/tmp}/micbridge-package.XXXXXX")"
    cleanup() { rm -rf "$stage"; }
    trap cleanup EXIT
    package="micbridge_{{ version }}_linux_amd64"
    mkdir -p "$dist" "$stage/$package"
    commit="$(git rev-parse --verify HEAD)"
    CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
      -trimpath -buildvcs=false \
      -ldflags="-s -w -X github.com/lancekrogers/micbridge/internal/core.Version={{ version }}" \
      -o "$stage/$package/micbridge" ./cmd/micbridge
    {
      echo "name=micbridge"
      echo "version={{ version }}"
      echo "target=linux/amd64"
      echo "commit=$commit"
      echo "go=$(go env GOVERSION)"
      echo "cgo=enabled"
    } > "$stage/$package/BUILDINFO"
    chmod 0755 "$stage/$package/micbridge"
    chmod 0644 "$stage/$package/BUILDINFO"
    archive="$dist/$package.tar.gz"
    tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner \
      -C "$stage" -cf - "$package" | gzip -n -9 > "$archive"
    sha256sum "$archive" > "$archive.sha256"
    echo "package-candidate: $archive"
    cat "$archive.sha256"
