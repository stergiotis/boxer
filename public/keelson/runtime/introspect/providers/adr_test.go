package providers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/app/commands/adr"
	"github.com/stergiotis/boxer/public/gov/adrcorpus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ADR-0122 §SD4: the keelson tables carry the same names and schemas `boxer adr`
// binds over its Arrow dump, so a query written against one runs verbatim
// against the other. These pin the two together — the symmetry is a claim the
// ADR makes, and nothing but a test keeps it true.

func TestAdrTableSchemasMatchTheArrowDump(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  *arrow.Schema
		want *arrow.Schema
	}{
		{"adr", adrProvider{}.Schema(), adr.AdrArrowSchema()},
		{"subtask", subtaskProvider{}.Schema(), adr.SubtaskArrowSchema()},
		{"coderef", coderefProvider{}.Schema(), adr.CoderefArrowSchema()},
	} {
		require.Equal(t, len(tc.want.Fields()), len(tc.got.Fields()),
			"table %q: field count", tc.name)
		for i, w := range tc.want.Fields() {
			g := tc.got.Field(i)
			assert.Equal(t, w.Name, g.Name, "table %q: field %d name", tc.name, i)
			assert.Truef(t, arrow.TypeEqual(w.Type, g.Type),
				"table %q: field %q type: dump has %s, keelson has %s", tc.name, w.Name, w.Type, g.Type)
		}
	}
}

// Schema parity is not enough: the table's 25 columns are hand-mapped onto Adr's
// fields, and swapping two of the same type (code_files for code_pkgs) would
// pass every schema check and report the wrong repository forever. So compare
// the two producers' *cells* on identical input — the Arrow dump is the oracle
// because `boxer adr` has been read against real corpora.
func TestAdrTableCellsMatchTheArrowDump(t *testing.T) {
	adrs := []adrcorpus.Adr{{
		Num: 42, Slug: "the-slug", Title: "The Title", Status: "accepted",
		Date: "2026-01-02", ReviewedBy: "@someone", ReviewedDate: "2026-01-03",
		SupersededBy: "ADR-0043", WithdrawnDate: "2026-01-04",
		BodyBytes: 1234, HasUpdate: true, UpdateCount: 2, LastDate: "2026-01-05",
		PlanMarkers: []string{"M1", "M2"}, PlanMaxPhase: 7,
		// Distinct values so a swap between same-typed columns cannot hide.
		CodeRefs: 11, CodeFiles: 22, CodePkgs: 33,
		CodeLangs: []string{"go", "rust"}, CodeQualifiers: []string{"SD1", "SD2"},
		ImplEvidence:  "broad",
		SubtasksTotal: 44, SubtasksDone: 55, SubtasksCited: 66,
		Path: "doc/adr/0042-the-slug.md",
	}}
	subs := []adrcorpus.Subtask{{
		Num: 42, Marker: "SD3", Kind: "SD", Ordinal: 3, Title: "A sub-item",
		Done: true, Shape: "list", Line: 77, CodeRefs: 88,
	}}
	refs := []adrcorpus.CodeRef{{
		Num: 42, Path: "public/x/y.go", Line: 99, Lang: "go",
		Pkg: "y", Qualifier: "SD3", Snippet: "// ADR-0042 §SD3",
	}}

	dir := t.TempDir()
	for _, tc := range []struct {
		name  string
		write func(string) error
		got   arrow.RecordBatch
	}{
		{"adr", func(p string) error { return adr.WriteAdrArrow(p, adrs) },
			adrTable(adrs).Build(introspect.AllColumns(), len(adrs))},
		{"subtask", func(p string) error { return adr.WriteSubtaskArrow(p, subs) },
			subtaskTable(subs).Build(introspect.AllColumns(), len(subs))},
		{"coderef", func(p string) error { return adr.WriteCoderefArrow(p, refs) },
			coderefTable(refs).Build(introspect.AllColumns(), len(refs))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.got.Release()
			path := filepath.Join(dir, tc.name+".arrow")
			require.NoError(t, tc.write(path))
			want := readOneArrowRecord(t, path)
			defer want.Release()

			require.Equal(t, want.NumCols(), tc.got.NumCols())
			require.Equal(t, want.NumRows(), tc.got.NumRows())
			for i := range int(want.NumCols()) {
				assert.Equal(t, want.ColumnName(i), tc.got.ColumnName(i), "column %d name", i)
				assert.Equal(t, want.Column(i).String(), tc.got.Column(i).String(),
					"column %q: the dump and the keelson table must carry the same cells",
					want.ColumnName(i))
			}
		})
	}
}

func readOneArrowRecord(t *testing.T, path string) arrow.RecordBatch {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	r, err := ipc.NewFileReader(f, ipc.WithAllocator(memory.DefaultAllocator))
	require.NoError(t, err)
	rec, err := r.Record(0)
	require.NoError(t, err)
	rec.Retain()
	require.NoError(t, r.Close())
	return rec
}

// The three tables are Live: the corpus is files on disk that change under a
// running process, so a Static table would go stale the first time an ADR is
// edited — silently, which is the failure mode a governance table can least
// afford.
func TestAdrTablesAreLive(t *testing.T) {
	for _, p := range []introspect.Provider{adrProvider{}, subtaskProvider{}, coderefProvider{}} {
		assert.Equal(t, introspect.FreshnessLive, p.Freshness(), "table %q", p.Name())
	}
}

// Off-repo the tables are empty, not absent and not an error: a shipped binary
// with no checkout around it has no corpus, which is a fact about the process
// rather than a failure. The schema stays available so a query still parses.
func TestAdrTablesAreEmptyWithNoCorpus(t *testing.T) {
	t.Setenv(adrcorpus.EnvAdrDirName, t.TempDir()) // a real dir, no ADRs in it
	for _, p := range []introspect.Provider{adrProvider{}, subtaskProvider{}, coderefProvider{}} {
		rec, err := p.Snapshot(introspect.AllColumns())
		require.NoError(t, err, "table %q must not error off-repo", p.Name())
		require.NotNil(t, rec, "table %q keeps its schema", p.Name())
		assert.Zero(t, rec.NumRows(), "table %q", p.Name())
		rec.Release()
	}
}

// An unresolvable BOXER_ADR_DIR is the one case that reports rather than
// guesses — but the provider still degrades to empty rather than propagating,
// since an introspection table with no rows is legible and a failed query is not.
func TestAdrTablesEmptyOnUnresolvableDir(t *testing.T) {
	t.Setenv(adrcorpus.EnvAdrDirName, "/nonexistent-corpus-path-for-test")
	_, _, err := adrcorpus.ResolveCorpus()
	require.Error(t, err, "resolution reports the bad path")
	assert.Contains(t, err.Error(), adrcorpus.EnvAdrDirName, "the message names the variable to fix")

	rec, snapErr := adrProvider{}.Snapshot(introspect.AllColumns())
	require.NoError(t, snapErr)
	assert.Zero(t, rec.NumRows())
	rec.Release()
}

// The registry carries all three under the names `boxer adr` binds.
func TestAdrTablesRegister(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, RegisterStatic(r))
	names := r.Names()
	for _, want := range []string{"adr", "subtask", "coderef"} {
		assert.Contains(t, names, want, "keelson(%q) must be registered", want)
	}
}
