package env_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Library packages that every binary links stay free of the test harness,
// the flag parsers and process spawning: the registry's CLI and test
// helpers live in envcli and envtest, so reading a variable links neither
// (ADR-0394).
func TestFoundationPackagesLinkNoHarness(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain")
	}
	tags, _ := os.ReadFile("../../../tags")
	for _, pkg := range []string{
		"github.com/stergiotis/boxer/public/config/env",
		"github.com/stergiotis/boxer/public/observability/eh",
		"github.com/stergiotis/boxer/public/containers",
		"github.com/stergiotis/boxer/public/functional/option",
	} {
		out, err := exec.Command("go", "list", "-deps", "-tags", strings.TrimSpace(string(tags)), pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
		}
		for _, dep := range strings.Fields(string(out)) {
			switch dep {
			case "testing", "flag", "os/exec", "github.com/urfave/cli/v3":
				t.Errorf("%s links %s", pkg, dep)
			}
		}
	}
}
