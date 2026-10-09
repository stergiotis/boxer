package sccapplet

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/trivialsql"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/scctree"
)

func scan() []scctree.SccGroup {
	return []scctree.SccGroup{
		{Name: "Go", Files: []scctree.SccFile{
			{Filename: "main.go", Location: "./main.go", Code: 10, Complexity: 1},
			{Filename: "a.go", Location: "./public/a/a.go", Code: 100, Complexity: 20},
			{Filename: "b.go", Location: "./public/a/b/c/d/b.go", Code: 300, Complexity: 30},
			{Filename: "x.out.go", Location: "./public/a/x.out.go", Code: 5000, Complexity: 900},
			{Filename: "a_test.go", Location: "./public/a/a_test.go", Code: 700, Complexity: 70},
			{Filename: "q.go", Location: "./it's/q.go", Code: 50, Complexity: 5},
		}},
		{Name: "Markdown", Files: []scctree.SccFile{
			{Filename: "README.md", Location: "./doc/README.md", Code: 40},
		}},
	}
}

type row struct {
	parent, label string
	value         uint64
	color         float64
}

// rowsOf answers the document's statement as a tab does — the trivial
// evaluator, no server — and returns its rows by id.
func rowsOf(t *testing.T, doc []byte) (rows map[string]row) {
	t.Helper()
	s := string(doc)
	start := strings.Index(s, "```sql\n")
	require.GreaterOrEqual(t, start, 0)
	end := strings.Index(s[start+7:], "```")
	require.GreaterOrEqual(t, end, 0)
	batch, _, err := trivialsql.Read(introspect.NewRegistry(), s[start+7:start+7+end]+"FORMAT ArrowStream", nil)
	require.NoError(t, err)
	defer batch.Release()
	col := func(name string) arrow.Array {
		idx := batch.Schema().FieldIndices(name)
		require.Len(t, idx, 1, name)
		return batch.Column(idx[0])
	}
	str := func(a arrow.Array, i int) string {
		switch c := a.(type) {
		case *array.String:
			return c.Value(i)
		case *array.LargeString:
			return c.Value(i)
		case *array.Binary:
			return string(c.Value(i))
		case *array.LargeBinary:
			return string(c.Value(i))
		}
		t.Fatalf("unexpected string column type %T", a)
		return ""
	}
	ids, parents, labels := col("id"), col("parent"), col("label")
	values, colors := col("value").(*array.Uint64), col("color").(*array.Float64)
	rows = map[string]row{}
	for i := range int(batch.NumRows()) {
		rows[str(ids, i)] = row{parent: str(parents, i), label: str(labels, i), value: values.Value(i), color: colors.Value(i)}
	}
	require.Len(t, rows, int(batch.NumRows()), "ids are unique")
	for id, r := range rows {
		if r.parent == "" {
			require.Equal(t, rootID, id)
			continue
		}
		_, ok := rows[r.parent]
		require.True(t, ok, "%s has a parent row %s", id, r.parent)
	}
	return
}

func TestComposeAnswersInATab(t *testing.T) {
	doc, st, err := Compose(scan(), Options{Revision: "0123abc"})
	require.NoError(t, err)
	require.Equal(t, Stats{Dirs: 8, Files: 5, Generated: 1, Tests: 1, Code: 500, Complexity: 56}, st)
	require.Contains(t, string(doc), "A snapshot of the repository at `0123abc`:")

	rows := rowsOf(t, doc)
	require.Len(t, rows, st.Dirs)
	var sum uint64
	for _, r := range rows {
		sum += r.value
	}
	require.Equal(t, uint64(st.Code), sum, "every line of code is counted once")
	require.Equal(t, row{parent: "public", label: "a", value: 100, color: 12.5}, rows["public/a"], "colour reads the subtree: (20+30)/(100+300)")
	require.Equal(t, row{parent: rootID, label: "public", value: 0, color: 12.5}, rows["public"])
	require.Equal(t, row{parent: rootID, label: "it's", value: 50, color: 10}, rows["it's"])
	require.Equal(t, row{parent: rootID, label: "doc", value: 40, color: 0}, rows["doc"])
	require.Equal(t, uint64(10), rows[rootID].value)
}

func TestComposeFoldsDeepDirectories(t *testing.T) {
	doc, st, err := Compose(scan(), Options{Depth: 2})
	require.NoError(t, err)
	rows := rowsOf(t, doc)
	require.Len(t, rows, st.Dirs)
	_, deep := rows["public/a/b"]
	require.False(t, deep)
	require.Equal(t, uint64(400), rows["public/a"].value, "public/a/b/c/d folds into public/a")
	require.Contains(t, string(doc), "A snapshot of a repository:", "no revision, no stamp")
}

func TestComposeIsDeterministic(t *testing.T) {
	a, _, err := Compose(scan(), Options{Depth: 3, Revision: "r"})
	require.NoError(t, err)
	g := scan()
	g[0], g[1] = g[1], g[0]
	for i, j := 0, len(g[1].Files)-1; i < j; i, j = i+1, j-1 {
		g[1].Files[i], g[1].Files[j] = g[1].Files[j], g[1].Files[i]
	}
	b, _, err := Compose(g, Options{Depth: 3, Revision: "r"})
	require.NoError(t, err)
	require.Equal(t, string(a), string(b))
}

func TestComposeRefusesNegativeDepth(t *testing.T) {
	_, _, err := Compose(scan(), Options{Depth: -1})
	require.Error(t, err)
}
