//go:build llm_generated_opus47

package widgets

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// buildSourceURL converts an absolute file path + line into a GitHub blob URL
// pinned to the build's commit SHA. Returns "" if the build lacks VCS info,
// the module is not a github.com/<org>/<repo>... path, or the absolute path
// can't be made repo-relative (e.g. binary moved out of the worktree).
//
// vcs.ModuleInfo() returns the full main-package import path (e.g.
// "github.com/stergiotis/boxer/public/thestack/cmd/imzero2"), not the
// repo root — we keep only the first three segments to recover
// "github.com/<org>/<repo>".
//
// Build is not run with -trimpath (verified scripts/ci/*.sh and src/rust/hmi.sh),
// so file paths arrive absolute. We strip everything up to and including the
// last "/<repoName>/" segment, which works for both worktree layouts
// (/home/.../pebble2impl/...) and CI checkouts.
func buildSourceURL(absFile string, line int) (url string) {
	var revision string
	// Under IMZERO2_SCREENSHOT_DIR substitute "main" for the build's
	// commit SHA. The SHA is baked into every <a href> in the SVG export,
	// so a screenshot tour rerun at a different commit produces N SVG
	// byte diffs that don't reflect any visual change. "main" resolves
	// against the branch tip at click-time — the link still works and
	// captures stop drifting per commit.
	if imzero2env.ScreenshotDir.Get() != "" {
		revision = "main"
	} else {
		var err error
		revision, _, err = vcs.GetVcsRevision()
		if err != nil || revision == "" {
			return
		}
	}
	modulePath := vcs.ModuleInfo()
	parts := strings.SplitN(modulePath, "/", 4)
	if len(parts) < 3 || parts[0] != "github.com" {
		return
	}
	ownerRepo := strings.Join(parts[:3], "/")
	repoName := parts[2]
	marker := "/" + repoName + "/"
	idx := strings.LastIndex(absFile, marker)
	if idx < 0 {
		return
	}
	relPath := absFile[idx+len(marker):]
	url = fmt.Sprintf("https://%s/blob/%s/%s#L%d", ownerRepo, revision, relPath, line)
	return
}
