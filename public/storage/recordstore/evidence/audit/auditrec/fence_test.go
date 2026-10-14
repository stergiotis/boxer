package auditrec_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The recorder links no keelson, thestack, imzero2 or egui package: a
// process that links no runtime records through it.
func TestLinksNoRuntime(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain")
	}
	tags, _ := os.ReadFile("../../../../../../tags")
	out, err := exec.Command("go", "list", "-deps", "-tags", strings.TrimSpace(string(tags)),
		"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec").CombinedOutput()
	require.NoError(t, err, "%s", out)
	for _, dep := range strings.Fields(string(out)) {
		for _, fenced := range []string{"/public/keelson/", "/public/thestack/", "imzero2", "egui"} {
			require.NotContains(t, dep, fenced, "auditrec links %s", dep)
		}
	}
}
