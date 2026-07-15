package adrboard

import (
	"os"
	"path/filepath"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Variable names, quoted back at the operator when the corpus cannot be found.
const (
	envAdrDirName  = "ADRBOARD_DIR"
	envAdrRootName = "ADRBOARD_ROOT"
)

// adrboard reads its launch configuration through the boxer-wide typed env
// registry (ADR-0009 / config/env) rather than raw os.Getenv, so the variables
// appear in `env list` and on the CLI flag surface.
var (
	// envAdrDir names the corpus. Empty walks up from the working directory
	// looking for a doc/adr, so the app finds it when launched from anywhere
	// inside a checkout — and can be pointed at a different one (a sibling
	// checkout, a review worktree) when not.
	envAdrDir = env.NewPath(env.Spec{
		Name:        envAdrDirName,
		Description: "ADR markdown directory to render as a board; empty finds the nearest doc/adr at or above the working directory",
		Category:    env.CategoryDev,
	})
	// envAdrRoot names the tree scanned for §-pinned citations, which is what
	// distinguishes a sub-item nothing references from one code is visibly
	// building. Empty derives the checkout holding the corpus. The scan is
	// skippable rather than fatal: a board without it still shows every
	// decision and every ✓, only with no amber dots.
	envAdrRoot = env.NewPath(env.Spec{
		Name:        envAdrRootName,
		Description: "source tree to scan for ADR code citations; empty derives the checkout containing the ADR directory, and an unresolvable root just omits the code-evidence dots",
		Category:    env.CategoryDev,
	})
)

// resolveCorpus yields the directory to parse and the tree to scan for code
// evidence. It errors rather than guessing about the corpus — a board silently
// built from an empty directory would look like a corpus with no ADRs in it —
// but returns an empty root instead of failing, since evidence is an
// enrichment.
func resolveCorpus() (adrDir, root string, err error) {
	if adrDir = envAdrDir.Get(); adrDir != "" {
		if !isDir(adrDir) {
			return "", "", eh.Errorf("%s is set to %q, which is not a directory", envAdrDirName, adrDir)
		}
		return adrDir, resolveRoot(adrDir), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", "", eh.Errorf("unable to determine the working directory: %w", err)
	}
	if adrDir, found := findAdrDirAbove(wd); found {
		return adrDir, resolveRoot(adrDir), nil
	}
	return "", "", eh.Errorf("no doc/adr directory found at or above %q; set %s to point at one", wd, envAdrDirName)
}

// resolveRoot picks the tree to scan: the override when set, else the checkout
// holding adrDir — i.e. the grandparent of a conventional <root>/doc/adr. An
// unconventional layout yields "" (no scan) rather than a guess, because
// scanning the wrong tree would report evidence that isn't there.
func resolveRoot(adrDir string) (root string) {
	if root = envAdrRoot.Get(); root != "" {
		if !isDir(root) {
			return ""
		}
		return root
	}
	abs, err := filepath.Abs(adrDir)
	if err != nil {
		return ""
	}
	if filepath.Base(abs) != "adr" || filepath.Base(filepath.Dir(abs)) != "doc" {
		return ""
	}
	return filepath.Dir(filepath.Dir(abs))
}

// findAdrDirAbove walks from start to the filesystem root looking for doc/adr.
func findAdrDirAbove(start string) (dir string, found bool) {
	for at := start; ; {
		if cand := filepath.Join(at, "doc", "adr"); isDir(cand) {
			return cand, true
		}
		parent := filepath.Dir(at)
		if parent == at {
			return "", false
		}
		at = parent
	}
}

func isDir(path string) (ok bool) {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
