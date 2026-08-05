package capmapcorpus

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// EnvVaultDirName names the corpus location. Quoted back at the operator when
// the vault cannot be found, so the message says what to set.
const EnvVaultDirName = "BOXER_CAPMAP_VAULT"

// conventionalVaultDir is where a checkout keeps its capability vault,
// mirroring how the decision corpus sits at doc/adr.
var conventionalVaultDir = filepath.Join("doc", "capabilities")

// The corpus reads its location through the boxer-wide typed env registry
// (ADR-0009 / config/env) rather than raw os.Getenv, so the variable shows up
// in `env list` and on the CLI flag surface.
//
// Empty walks up from the working directory looking for doc/capabilities, so a
// process started anywhere inside a checkout finds it — and can be pointed at
// a different one (a vault kept outside the tree, a review copy) when not.
var envVaultDir = env.NewPath(env.Spec{
	Name:        EnvVaultDirName,
	Description: "business-capability vault directory to read as the corpus; empty finds the nearest doc/capabilities at or above the working directory",
	Category:    env.CategoryDev,
})

// ResolveVault yields the directory to parse.
//
// It errors rather than guessing: a reader silently handed an empty directory
// would see a corpus with no capabilities in it and no way to tell that apart
// from a vault that is genuinely empty.
func ResolveVault() (vaultDir string, err error) {
	if vaultDir = envVaultDir.Get(); vaultDir != "" {
		if !isDir(vaultDir) {
			return "", eh.Errorf("%s is set to %q, which is not a directory", EnvVaultDirName, vaultDir)
		}
		return vaultDir, nil
	}
	wd, wdErr := os.Getwd()
	if wdErr != nil {
		return "", eh.Errorf("unable to determine the working directory: %w", wdErr)
	}
	if found, ok := findVaultAbove(wd); ok {
		return found, nil
	}
	return "", eh.Errorf("no %s directory found at or above %q; set %s to point at one",
		conventionalVaultDir, wd, EnvVaultDirName)
}

// findVaultAbove walks from start to the filesystem root looking for the
// conventional vault directory.
func findVaultAbove(start string) (dir string, found bool) {
	for at := start; ; {
		if cand := filepath.Join(at, conventionalVaultDir); isDir(cand) {
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

// LoadWindow is how long one read of a vault is reused for the same location.
//
// It is a consistency device before it is a cache. A reader that exposes the
// corpus as several tables reads them one at a time — a query joining
// capabilities to relations calls Load twice — and an edit landing in between
// would produce a torn join: two tables describing different vaults, with no
// error to show for it. Sharing one read across the window makes them one
// snapshot.
//
// The window is short enough that a human cannot edit a capability and
// re-query inside it, so readers stay honestly live: they never serve an
// answer that outlives a change by longer than this.
const LoadWindow = 2 * time.Second

var (
	loadMu   sync.Mutex
	loadedAt time.Time
	// loadedKey is the resolved vault directory. Keying on it means pointing a
	// process at a different vault takes effect at once rather than after the
	// window, and keeps the memo from leaking between tests.
	loadedKey    string
	loadedCorpus Corpus
)

// Load resolves the vault and reads it whole. Reads within [LoadWindow] of one
// another for the same vault share a snapshot.
//
// Every failure degrades to no rows rather than an error, because the callers
// are introspection surfaces where "nothing here" is a legible answer and a
// hard failure is not — a binary shipped without a checkout around it has no
// corpus, which is a fact about the process rather than a fault. Callers that
// need the reason (an ingest, a CLI) should call [ResolveVault] and [ParseDir]
// directly.
//
// The returned slices are shared with other callers in the window and must not
// be mutated.
func Load() (corpus Corpus) {
	vaultDir, err := ResolveVault()
	if err != nil {
		return Corpus{}
	}
	loadMu.Lock()
	defer loadMu.Unlock()
	if vaultDir == loadedKey && !loadedAt.IsZero() && time.Since(loadedAt) < LoadWindow {
		return loadedCorpus
	}
	if corpus, err = ParseDir(vaultDir); err != nil {
		return Corpus{}
	}
	loadedKey, loadedAt, loadedCorpus = vaultDir, time.Now(), corpus
	return corpus
}

// resetLoadMemo drops the shared snapshot so the next Load performs a real
// read. Waiting out [LoadWindow] would do the same and cost every caller the
// window.
func resetLoadMemo() {
	loadMu.Lock()
	defer loadMu.Unlock()
	loadedKey, loadedAt, loadedCorpus = "", time.Time{}, Corpus{}
}

// SetVaultForTest points the corpus at dir for the duration of t.
//
// It exists because neither half of the state a test needs to control is
// reachable from outside: the env handle caches its value on the first read
// for the life of the process, so a plain t.Setenv is invisible once anything
// has resolved the vault, and the load memo would serve the previous vault's
// snapshot even after the variable changed. This resets both, and restores
// them when the test ends.
//
// Following the same convention as [env.PathVar.SetForTest], which is why the
// production file imports testing.
func SetVaultForTest(t testing.TB, dir string) {
	t.Helper()
	envVaultDir.SetForTest(t, dir)
	resetLoadMemo()
	t.Cleanup(resetLoadMemo)
}
