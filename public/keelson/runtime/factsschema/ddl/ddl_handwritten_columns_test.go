package ddl_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ddl"
)

// physicalColRe matches a leeway physical column name as it appears in source:
// the `tv:` tagged-value prefix through the streaming-group suffix. Anchored on
// both ends by a quoting delimiter so a partial name (a prefix used to build one
// at runtime) is not mistaken for a whole one. The delimiter class covers every
// form the scanned trees use — Go raw strings and markdown code spans (backtick),
// ClickHouse identifier quoting (double quote), and shell literals (single
// quote).
var physicalColRe = regexp.MustCompile("[`\"'](tv:[A-Za-z0-9]+:[A-Za-z0-9]+:[a-z]+:[^`\"']*::[A-Za-z0-9]+)[`\"']")

// sectionPrefixRe pulls the `tv:<section>:<column>:` head off a physical name.
// That head is what decides whether a name belongs to this table at all — see
// inScope.
var sectionPrefixRe = regexp.MustCompile("^(tv:[A-Za-z0-9]+:[A-Za-z0-9]+:)")

// inScope reports whether col names a section+column boxer.facts actually has.
//
// The scanned trees are not single-table: apps/play/help demonstrates queries
// against both boxer.facts and the anchor demo table, and the two share a file.
// Comparing every name found there against this table's block would report
// anchor's `tv:timeRange:…` and `tv:geoPoint:…` columns as stale when they are
// simply not ours. Matching on the section+column head instead keeps the check
// where it bites — a section this table *does* have, carrying an aspect segment
// this table no longer emits.
//
// Known limit: renaming a section outright (rather than re-aspecting it) takes
// its old head out of the block too, so stale occurrences of the old name go
// unseen here. Section renames are schema edits with their own review; aspect
// shifts are the silent ones this guard is for.
func inScope(col string) bool {
	m := sectionPrefixRe.FindStringSubmatch(col)
	if m == nil {
		return false
	}
	return strings.Contains(ddl.ColumnsSQL, `"`+m[1])
}

// scanRoots are the places that spell boxer.facts physical column names out by
// hand. Each is a repo-root-relative directory (walked) or a single file. The
// list is explicit rather than repo-wide because whole trees of other leeway
// tables — recordstore, the ecsdemo and example schemas — would otherwise be
// walked for nothing, and because being explicit records *which* code queries
// this table.
//
// Adding a boxer.facts query outside these paths means adding the path here.
var scanRoots = []string{
	// The facts store itself, its read-back queries and the codec layer.
	"public/keelson/runtime",
	// Reads capability-map facts back out of boxer.facts.
	"public/gov/capmapfacts",
	// Reads fact kinds off the symbol section to build load studies.
	"public/analytics/timeseries/loadstudy",
	// Help SQL users copy verbatim into the playground — wrong names here are
	// wrong answers in the user's hands, not just a broken build.
	"apps/play/help",
	// Applet-store durability check and the jsonbench facts arm.
	"apps/sqlapplet",
	"apps/jsonbench",
	// The naming explainer's worked example.
	"doc/explanation/leeway-column-names.md",
	// Trial protocol scripts are meant to be re-run against a current table.
	// Their runs/ outputs and logbook are frozen records of what past builds
	// actually emitted and are deliberately not scanned.
	"doc/trials/jsonbench-on-facts/arm-c.sh",
	"doc/trials/jsonbench-on-facts/arm-d.sh",
	"doc/trials/jsonbench-on-facts/measure.sh",
	// A name-resolution fixture that asserts a physical name passes through
	// handle resolution untouched; it uses a real one, so keep it real.
	// The nanopass resolver fixture next door is deliberately absent: it fakes
	// several tables at once, so most of its names are not ours to check.
	"public/semistructured/leeway/lwsql/lwsql_test.go",
}

// scannedExts are the file kinds that carry hand-written column names.
var scannedExts = map[string]bool{".go": true, ".md": true, ".sh": true, ".sql": true}

// repoRoot walks up from the test's working directory until it finds go.mod, so
// the scan roots stay valid if this package moves.
func repoRoot(t *testing.T) (root string) {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve working directory: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			root = dir
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found walking up from the test directory — cannot locate the repo root")
		}
		dir = parent
	}
}

// TestHandwrittenColumnsMatchGeneratedSchema pins every physical column name
// hardcoded against boxer.facts to the generated column block.
//
// It exists because that coupling has no other guard. The leeway physical
// column name encodes the column's aspect bitmask, so changing a section's
// value semantics or its codec renames the column — and the read-back queries
// in chstore and queryrunfacts, plus the help SQL and the trial scripts, spell
// those names out as constants rather than deriving them. The generators do not
// touch those constants.
//
// Nothing else catches the drift in the default lane: the chstore round-trip
// tests that would fail call t.Skipf when ClickHouse is unreachable, so on a
// machine without it a rename ships green and fails at runtime against a
// re-created table. This test needs no ClickHouse.
//
// A failure means the constants are stale, not that the schema is wrong. Fix
// the named file, do not edit the generated block.
func TestHandwrittenColumnsMatchGeneratedSchema(t *testing.T) {
	root := repoRoot(t)
	type site struct{ file, col string }
	var stale []site
	var checked int

	scanRootedFiles(t, root, &checked, func(rel, col string) {
		stale = append(stale, site{file: rel, col: col})
	})

	if checked == 0 {
		t.Fatal("found no hardcoded physical column names to check — the regexp or the scan roots are wrong, " +
			"and a vacuously passing guard is worse than none")
	}
	if len(stale) > 0 {
		sort.Slice(stale, func(i, j int) bool { return stale[i].file < stale[j].file })
		var b strings.Builder
		b.WriteString("hardcoded physical column names are absent from the generated schema — ")
		b.WriteString("the section's aspects changed and these constants were not updated:\n")
		for _, s := range stale {
			b.WriteString("\t" + s.file + ": " + s.col + "\n")
		}
		b.WriteString("\nregenerate with `./boxer.sh runtimecodegen all`, then update the constants above " +
			"to the names in the regenerated block.")
		t.Fatal(b.String())
	}
	t.Logf("checked %d hardcoded physical column names against the generated block", checked)
}

// scanRootedFiles walks every scan root and reports each physical name absent
// from the generated block through onStale. checked counts every name examined,
// so a scan that silently matches nothing can be told from a clean one.
func scanRootedFiles(t *testing.T, root string, checked *int, onStale func(rel, col string)) {
	t.Helper()
	check := func(path string) {
		if !scannedExts[filepath.Ext(path)] {
			return
		}
		// Generated files are re-emitted from the schema and so cannot drift
		// from it; scanning them would only re-assert the generator against
		// itself. That holds within a regeneration lane. It does not hold
		// across two, which is what TestStoreDDLMatchesGeneratedSchema covers
		// — the names there are not hardcoded, so this scan is the wrong shape
		// for them either way.
		base := filepath.Base(path)
		if strings.Contains(base, ".out.") {
			return
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		rel, _ := filepath.Rel(root, path)
		for _, m := range physicalColRe.FindAllStringSubmatch(string(src), -1) {
			col := m[1]
			if !inScope(col) {
				continue
			}
			*checked++
			if !strings.Contains(ddl.ColumnsSQL, `"`+col+`"`) {
				onStale(rel, col)
			}
		}
	}
	for _, r := range scanRoots {
		abs := filepath.Join(root, r)
		info, err := os.Stat(abs)
		if err != nil {
			t.Fatalf("scan root %q does not exist — the list is stale: %v", r, err)
		}
		if !info.IsDir() {
			check(abs)
			continue
		}
		err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			check(path)
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", r, err)
		}
	}
}
