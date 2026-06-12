//go:build tinygo

package eh

// goRootFromToolchain has no process model to query under TinyGo (os/exec is
// unavailable on wasm), so GOROOT detection falls back to the env override
// alone (see detectedGoRoot). Returning "" just means stack-trace paths under
// GOROOT are not shortened. See ADR-0078.
func goRootFromToolchain() string {
	return ""
}
