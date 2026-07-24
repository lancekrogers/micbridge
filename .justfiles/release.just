# Release module — packaging scaffolding (no publish by default)

bin_dir := "bin"
binary_name := "micbridge"
gobin := env_var_or_default("GOBIN", `go env GOPATH` + "/bin")
version := env_var_or_default("VERSION", "0.0.0-dev")

# Install binary into GOBIN (delegates to root recipe).
install-bin:
    just install

# Print release metadata that would be stamped (does not tag or push).
info:
    @echo "binary={{binary_name}}"
    @echo "version={{version}}"
    @echo "module=$(go list -m)"
    @echo "go=$(go env GOVERSION)"
    @echo "NOTE: do not publish a release/tag unless a festival task authorizes it"

# Explicit opt-in name: would package artifacts later; blocked until authorized.
[doc('Blocked until festival release task authorizes packaging')]
package-candidate:
    @echo "xrelease::package-candidate is not authorized in bootstrap"
    @exit 1
