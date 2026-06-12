//go:build !tinygo

package eh

import (
	"os/exec"
	"strings"
)

// goRootFromToolchain asks the installed Go toolchain for GOROOT. This is the
// native implementation; the TinyGo build uses the goroot_tinygo.go stub
// instead, because wasm has no process model to exec `go` with. A failure
// returns "" — the caller (detectedGoRoot) treats that as "GOROOT unknown" and
// simply leaves stack-trace paths unshortened.
func goRootFromToolchain() string {
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
