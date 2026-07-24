# Build module — artifacts and cleanup

set working-directory := ".."

bin_dir := "bin"
binary_name := "micbridge"
cmd_pkg := "./cmd/micbridge"
version := env_var_or_default("VERSION", "0.0.0-dev")

# Build ./bin/micbridge (same as root `just build`).
default-build:
    mkdir -p {{bin_dir}}
    go build -ldflags="-X github.com/lancekrogers/micbridge/internal/core.Version={{version}}" -o {{bin_dir}}/{{binary_name}} {{cmd_pkg}}

# Remove local build outputs (never touches recordings outside repo ignores).
clean:
    rm -rf {{bin_dir}}/ dist/ coverage.out coverage.html

# Show build-related environment overrides.
env:
    @echo "VERSION={{version}}"
    @echo "bin_dir={{bin_dir}}"
    @echo "cmd_pkg={{cmd_pkg}}"
    @go env GOVERSION GOOS GOARCH CGO_ENABLED
