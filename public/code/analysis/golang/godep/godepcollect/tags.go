package godepcollect

import (
	"os"
	"path/filepath"
	"strings"
)

// This file is the one place build tags are resolved for the package-loading
// tools. It lives beside ModuleRoot because every caller that needs a module
// root also needs the tags that root's packages are built with, and because the
// resolution is only useful if every tool agrees on it: two collectors resolving
// tags differently would see different files and disagree about the same tree
// for reasons no one could see.

// ResolveTags resolves the build-tag list for a module, in precedence order:
// an explicit value (a --tags flag), else the module root's `tags` file, else
// the -tags= carried in GOFLAGS. It returns nil when none apply.
//
// The tags are load-bearing in this repo: a load that omits them selects a
// different set of files than a real build, so tools that skip this resolution
// silently analyse a tree that does not exist.
func ResolveTags(explicit string, root string) (tags []string) {
	if explicit != "" {
		return SplitTags(explicit)
	}
	if root != "" {
		if t := ReadTagsFile(filepath.Join(root, "tags")); len(t) > 0 {
			return t
		}
	}
	if gf := os.Getenv("GOFLAGS"); gf != "" { //boxer:lint disable=CS011 reason="GOFLAGS is a Go-toolchain variable owned by the toolchain, not a boxer config var; read here only to mirror the toolchain's own -tags resolution as the last-resort fallback"
		for f := range strings.SplitSeq(gf, " ") {
			if after, ok := strings.CutPrefix(strings.TrimSpace(f), "-tags="); ok {
				return SplitTags(after)
			}
		}
	}
	return nil
}

// SplitTags parses a comma-separated tag list, trimming blanks.
func SplitTags(csv string) (tags []string) {
	for t := range strings.SplitSeq(csv, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	return
}

// ReadTagsFile reads a build-tag file (newline- or comma-separated), returning
// nil when it is absent or empty.
func ReadTagsFile(path string) (tags []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return SplitTags(strings.ReplaceAll(string(data), "\n", ","))
}
