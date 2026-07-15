package godepview

import (
	"os"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
	"github.com/stergiotis/boxer/public/config/env"
)

// godepview reads its launch configuration through the boxer-wide typed env
// registry (ADR-0009 / config/env) rather than raw os.Getenv, so the
// variables appear in `env list` and on the CLI flag surface.
var (
	// envRoot overrides the module directory the collector walks. Empty
	// resolves the nearest go.mod above the process working directory, so
	// the app collects the right module regardless of how it was launched.
	envRoot = env.NewPath(env.Spec{
		Name:        "GODEPVIEW_ROOT",
		Description: "module directory to collect the Go dependency graph from; empty resolves the nearest go.mod above the working directory",
		Category:    env.CategoryDev,
	})
	// envTags overrides the build tags the collector's `go list` runs
	// under (comma-separated). Empty falls back to the contents of
	// <root>/tags if present, otherwise whatever GOFLAGS carries.
	envTags = env.NewString(env.Spec{
		Name:        "GODEPVIEW_TAGS",
		Description: "comma-separated build tags for collection; empty falls back to <root>/tags then inherited GOFLAGS",
		Category:    env.CategoryDev,
	})
)

// resolveCollectorConfig builds the collector Config from the environment.
// Per ADR-0064 SD3 the collector itself stays env-free and takes an explicit
// Config; this composition-root helper does the resolution. Root comes from
// GODEPVIEW_ROOT or the module root above the working dir; tags from
// GODEPVIEW_TAGS or the root's `tags` file (else nil — inherit GOFLAGS).
func resolveCollectorConfig() (cfg godepcollect.Config) {
	root := envRoot.Get()
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			if r, ok := godepcollect.ModuleRoot(wd); ok {
				root = r
			}
		}
	}
	cfg.Dir = root // "" → collector falls back to the process working dir
	cfg.Tags = godepcollect.ResolveTags(envTags.Get(), root)
	return
}
