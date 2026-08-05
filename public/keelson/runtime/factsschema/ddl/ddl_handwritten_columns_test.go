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

// physicalColRe matches a leeway physical column name as it appears in a Go
// string literal: the `tv:` tagged-value prefix through the streaming-group
// suffix. Anchored on both ends so a partial name (a prefix used to build one
// at runtime) is not mistaken for a whole one.
var physicalColRe = regexp.MustCompile("`(tv:[A-Za-z0-9]+:[A-Za-z0-9]+:[a-z]+:[^`]*::[A-Za-z0-9]+)`")

// TestHandwrittenColumnsMatchGeneratedSchema pins every physical column name
// hardcoded in hand-written SQL under keelson/runtime against the generated
// column block.
//
// It exists because that coupling has no other guard. The leeway physical
// column name encodes the column's aspect bitmask, so changing a section's
// value semantics or its codec renames the column — and the read-back queries
// in chstore and queryrunfacts spell those names out as string constants
// rather than deriving them. The generators do not touch those constants.
//
// Nothing else catches the drift in the default lane: the chstore round-trip
// tests that would fail call t.Skipf when ClickHouse is unreachable, so on a
// machine without it a rename ships green and fails at runtime against a
// re-created table. This test needs no ClickHouse.
//
// A failure means the constants are stale, not that the schema is wrong. Fix
// the named file, do not edit the generated block.
func TestHandwrittenColumnsMatchGeneratedSchema(t *testing.T) {
	root, err := filepath.Abs("../..") // public/keelson/runtime
	if err != nil {
		t.Fatalf("resolve scan root: %v", err)
	}
	type site struct{ file, col string }
	var stale []site
	var checked int
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Generated files are re-emitted from the schema and so cannot drift
		// from it; scanning them would only re-assert the generator against
		// itself.
		if strings.HasSuffix(path, ".out.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(root, path)
		for _, m := range physicalColRe.FindAllStringSubmatch(string(src), -1) {
			col := m[1]
			checked++
			if !strings.Contains(ddl.ColumnsSQL, `"`+col+`"`) {
				stale = append(stale, site{file: rel, col: col})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	if checked == 0 {
		t.Fatal("found no hardcoded physical column names to check — the regexp or the scan root is wrong, " +
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
		t.Fatal(b.String())
	}
	t.Logf("checked %d hardcoded physical column names against the generated block", checked)
}
