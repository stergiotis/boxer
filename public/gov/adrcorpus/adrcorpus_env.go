package adrcorpus

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Corpus location, resolved once here rather than by each consumer. The names
// are quoted back at the operator when the corpus cannot be found.
const (
	EnvAdrDirName  = "BOXER_ADR_DIR"
	EnvAdrRootName = "BOXER_ADR_ROOT"
)

// The corpus reads its location through the boxer-wide typed env registry
// (ADR-0009 / config/env) rather than raw os.Getenv, so the variables appear in
// `env list` and on the CLI flag surface.
var (
	// envAdrDir names the corpus. Empty walks up from the working directory
	// looking for a doc/adr, so a process finds it when started anywhere
	// inside a checkout — and can be pointed at a different one (a sibling
	// checkout, a review worktree) when not.
	envAdrDir = env.NewPath(env.Spec{
		Name:        EnvAdrDirName,
		Description: "ADR markdown directory to read as the corpus; empty finds the nearest doc/adr at or above the working directory",
		Category:    env.CategoryDev,
	})
	// envAdrRoot names the tree scanned for §-pinned citations, which is what
	// distinguishes a sub-item nothing references from one code is visibly
	// building. Empty derives the checkout holding the corpus. The scan is
	// skippable rather than fatal: a corpus without it still carries every
	// decision and every ✓, only with no code evidence.
	envAdrRoot = env.NewPath(env.Spec{
		Name:        EnvAdrRootName,
		Description: "source tree to scan for ADR code citations; empty derives the checkout containing the ADR directory, and an unresolvable root just omits the code evidence",
		Category:    env.CategoryDev,
	})
)

// ResolveCorpus yields the directory to parse and the tree to scan for code
// evidence. It errors rather than guessing about the corpus — a reader silently
// fed an empty directory would see a corpus with no ADRs in it — but returns an
// empty root instead of failing, since evidence is an enrichment.
func ResolveCorpus() (adrDir, root string, err error) {
	if adrDir = envAdrDir.Get(); adrDir != "" {
		if !isDir(adrDir) {
			return "", "", eh.Errorf("%s is set to %q, which is not a directory", EnvAdrDirName, adrDir)
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
	return "", "", eh.Errorf("no doc/adr directory found at or above %q; set %s to point at one", wd, EnvAdrDirName)
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

// LoadWindow is how long a Load is reused for the same corpus location.
//
// It is a consistency device before it is a cache. A reader that exposes the
// corpus as several tables reads them one at a time — a query joining `adr` and
// `subtask` calls Load twice — and a read takes long enough (see below) that an
// edit landing in between would produce a torn join: two tables describing
// different repositories, with no error to show for it. Sharing one read across
// the window makes them one snapshot.
//
// Halving the cost is the side effect, and worth stating because the cost is
// not where you would guess: parsing the corpus dominates (hundreds of ms for
// ~120 files), while the whole-tree citation scan is a fraction of it. That
// also rules out the obvious alternative — an mtime key over the scanned tree
// costs about as much to compute as the work it would skip.
//
// The window is short enough that a human cannot edit an ADR and re-query
// inside it, so the tables stay honestly Live: they never serve an answer that
// outlives a change by longer than this.
const LoadWindow = 2 * time.Second

var (
	loadMu   sync.Mutex
	loadedAt time.Time
	// loadedKey is the resolved (adrDir, root). Keying on it means pointing a
	// process at a different corpus takes effect at once rather than after the
	// window — and keeps the memo from leaking between tests.
	loadedKey  string
	loadedAdrs []Adr
	loadedSubs []Subtask
	loadedRefs []CodeRef
)

// Load resolves the corpus and reads it whole: the decisions, their sub-items,
// and the code citations folded in. Reads within [LoadWindow] of one another
// for the same corpus share one snapshot.
//
// Every failure degrades to fewer rows rather than an error — an unresolvable
// corpus yields nothing, and an unresolvable scan root yields decisions with no
// evidence — because the callers are introspection surfaces, where "nothing
// here" is a legible answer and a hard failure is not.
//
// The returned slices are shared with other callers in the window and must not
// be mutated.
func Load() (adrs []Adr, subs []Subtask, refs []CodeRef) {
	adrDir, root, err := ResolveCorpus()
	if err != nil {
		return
	}
	key := adrDir + "\x00" + root
	loadMu.Lock()
	defer loadMu.Unlock()
	if key == loadedKey && !loadedAt.IsZero() && time.Since(loadedAt) < LoadWindow {
		return loadedAdrs, loadedSubs, loadedRefs
	}
	if adrs, err = ParseDir(adrDir); err != nil {
		return nil, nil, nil
	}
	refs, _ = ScanCodeRefs(root, adrDir, "")
	// Aggregate even for nil refs: it zeroes the evidence counts rather than
	// leaving a previous read's behind.
	adrs = Aggregate(adrs, refs)
	subs = AllSubtasks(adrs)
	loadedKey, loadedAt, loadedAdrs, loadedSubs, loadedRefs = key, time.Now(), adrs, subs, refs
	return adrs, subs, refs
}
