package godepview

import (
	"os"
	"path/filepath"
	"strings"

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

	if raw := envTags.Get(); raw != "" {
		cfg.Tags = splitTags(raw)
	} else if root != "" {
		cfg.Tags = readTagsFile(filepath.Join(root, "tags"))
	}
	return
}

// splitTags parses a comma-separated tag list, trimming blanks.
func splitTags(csv string) (tags []string) {
	for t := range strings.SplitSeq(csv, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	return
}

// readTagsFile reads a build-tag file (newline- or comma-separated),
// returning nil when it is absent or empty.
func readTagsFile(path string) (tags []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return splitTags(strings.ReplaceAll(string(data), "\n", ","))
}
