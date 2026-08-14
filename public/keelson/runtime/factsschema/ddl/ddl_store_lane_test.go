package ddl_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ddl"
)

// storeDDLFiles are the checked-in DDL files emitted by facts-bound record
// stores — repo-root-relative. Each states the physical schema its store
// decodes positionally, and each is generated in a *different lane* from this
// package's own artifacts. That is the whole reason this list exists; see
// TestStoreDDLMatchesGeneratedSchema.
//
// Adding a facts-bound store means adding its DDL file here. A store that
// binds its own table (recordstore/example, persiststore) does not belong —
// its columns are not this table's and comparing them would fail correctly
// for the wrong reason.
var storeDDLFiles = []string{
	// ADR-0184 (proposed) M1: the sysmetrics tee.
	"public/keelson/runtime/sysmfacts/facts_ddl_clickhouse.out.sql",
}

// TestStoreDDLMatchesGeneratedSchema pins every facts-bound store's emitted
// column block to this package's generated one.
//
// Both blocks are generated from one source — factsschema's TableDesc — so
// they cannot disagree about the schema. They can disagree about *when*,
// because they regenerate in two lanes that no single command couples:
//
//   - this package's four artifacts move only under `boxer runtimecodegen
//     all`, which no //go:generate directive sweeps;
//   - a store's move from a gen-test, which plain `go test ./...` and
//     `go generate ./...` both run.
//
// So a leeway aspect-vocabulary change — which renames physical columns —
// plus a bare `go test ./...` leaves the store on the new names and this
// package on the old ones. Both still compile, both still pass their own
// tests, and the two now describe one live table differently. That is the
// state this test refuses.
//
// It is the reason the sibling handwritten-columns guard cannot cover this:
// that one skips `.out.` files as re-emitted from the schema and therefore
// unable to drift from it, which holds within a lane and not across two.
//
// Out of scope: whether either block matches the *live* table. `chstore` is
// the sole DDL author (ADR-0184 (proposed) §SD2) and the generated
// VerifySchema is what checks a running server. This test needs no
// ClickHouse.
//
// A failure means one lane was not regenerated, not that either schema is
// wrong. Run scripts/dev/generate.sh, which invokes both in order.
func TestStoreDDLMatchesGeneratedSchema(t *testing.T) {
	if len(storeDDLFiles) == 0 {
		t.Fatal("no facts-bound store DDL files listed — a vacuously passing guard is worse than none")
	}
	root := repoRoot(t)
	want := strings.TrimSpace(ddl.ColumnsSQL)

	for _, rel := range storeDDLFiles {
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s — the list is stale: %v", rel, err)
		}
		got := columnBlock(t, rel, string(src))
		if got == want {
			continue
		}
		t.Errorf("%s\n%s", rel, describeDrift(got, want))
	}
}

// columnBlock cuts the column list out of a composed CREATE TABLE: everything
// between the opening paren's line and the closing paren, which starts a line
// of its own (a column line's only parens are inside a type or a CODEC, never
// at column zero).
func columnBlock(t *testing.T, rel, sql string) (block string) {
	t.Helper()
	open := "CREATE TABLE IF NOT EXISTS " + ddl.DatabaseName + "." + ddl.TableName + " (\n"
	i := strings.Index(sql, open)
	if i < 0 {
		t.Fatalf("%s: no %q — the store's table reference changed, or it no longer binds this table", rel, strings.TrimSuffix(open, " (\n"))
	}
	rest := sql[i+len(open):]
	j := strings.Index(rest, "\n)")
	if j < 0 {
		t.Fatalf("%s: column block is not closed by a line-leading ')'", rel)
	}
	return strings.TrimSpace(rest[:j])
}

// describeDrift names the first differing line and the count, rather than
// dumping two ~180-line blocks a reader has to diff by eye. A regeneration
// skew renames a handful of columns, so the first difference is normally the
// whole story.
func describeDrift(got, want string) (msg string) {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	var b strings.Builder
	b.WriteString("store DDL column block differs from ddl.ColumnsSQL — one lane was not regenerated.\n")
	if len(g) != len(w) {
		fmt.Fprintf(&b, "\tcolumn count: store has %d, this package has %d\n", len(g), len(w))
	}
	for i := 0; i < len(g) && i < len(w); i++ {
		if g[i] == w[i] {
			continue
		}
		fmt.Fprintf(&b, "\tfirst difference at column line %d:\n\t\tstore: %s\n\t\tthis:  %s\n",
			i+1, strings.TrimSpace(g[i]), strings.TrimSpace(w[i]))
		break
	}
	b.WriteString("\nrun scripts/dev/generate.sh — `boxer runtimecodegen all` and `go generate ./...` " +
		"move the two lanes and only the script runs both.")
	return b.String()
}
