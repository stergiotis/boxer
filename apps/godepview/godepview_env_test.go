package godepview

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveCollectorConfig_FindsModuleRootAndTags verifies the
// self-sufficiency fix (ADR-0064): with neither GODEPVIEW_ROOT nor
// GODEPVIEW_TAGS set, resolveCollectorConfig must still locate the module
// root and its build tags. `go test` runs with the working directory set to
// this package (apps/godepview) — a subdirectory of the module — so a pass
// proves resolution no longer depends on the launcher's CWD or GOFLAGS.
func TestResolveCollectorConfig_FindsModuleRootAndTags(t *testing.T) {
	if _, set := os.LookupEnv("GODEPVIEW_ROOT"); set {
		t.Skip("GODEPVIEW_ROOT is set in the environment; skipping the walk-up assertion")
	}
	cfg := resolveCollectorConfig()
	if cfg.Dir == "" {
		t.Fatal("expected a resolved module root from the go.mod walk, got empty Dir")
	}
	if _, err := os.Stat(filepath.Join(cfg.Dir, "go.mod")); err != nil {
		t.Fatalf("resolved Dir %q does not contain go.mod: %v", cfg.Dir, err)
	}
	if len(cfg.Tags) == 0 {
		t.Errorf("expected build tags resolved from %s/tags, got none", cfg.Dir)
	}
	t.Logf("resolved root=%s, %d tags", cfg.Dir, len(cfg.Tags))
}
