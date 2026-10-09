package passes_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// valuesStatement is a statement of about 20 tokens per row: a literal table,
// the shape an applet that carries its data inline sends.
func valuesStatement(rows int) string {
	var b strings.Builder
	b.WriteString("SELECT * FROM values('id String, parent String, value UInt64, color Float64'")
	for i := range rows {
		fmt.Fprintf(&b, ",\n('dir/%d', 'dir', %d, %d.5)", i, i*7, i%50)
	}
	b.WriteString(")")
	return b.String()
}

// allocatedBytes is what f allocates, in bytes, over one call.
func allocatedBytes(f func()) (n uint64) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// growthOf reports how much more f allocates on a statement four times the
// size: about 4 for an emission linear in the statement, about 16 for one that
// copies the text emitted so far once per token.
func growthOf(t *testing.T, f func(pr *nanopass.ParseResult)) (ratio float64) {
	t.Helper()
	const small = 200
	prSmall, err := nanopass.Parse(valuesStatement(small))
	require.NoError(t, err)
	prLarge, err := nanopass.Parse(valuesStatement(4 * small))
	require.NoError(t, err)
	f(prSmall) // warm whatever is lazily built
	f(prLarge)
	a := allocatedBytes(func() { f(prSmall) })
	b := allocatedBytes(func() { f(prLarge) })
	require.NotZero(t, a)
	ratio = float64(b) / float64(a)
	t.Logf("allocated %d B at %d rows, %d B at %d rows: ×%.1f", a, small, b, 4*small, ratio)
	return
}

// TestTextEmissionIsLinearInStatementSize pins that turning a parsed statement
// back into text costs memory in proportion to its length. A pass that
// changes nothing still emits its result through an unedited rewriter, and
// the antlr token stream builds that text by appending one token at a time to
// an immutable string, which copies everything emitted so far for every token:
// the bytes grow with the square of the statement. On a 112 KB statement of
// literal rows that ran a wasm module out of memory inside ResolveColumnNames,
// which play's diagnostics run on every buffer, an applet's included.
func TestTextEmissionIsLinearInStatementSize(t *testing.T) {
	const ceiling = 8.0 // ×4 is linear, ×16 quadratic
	t.Run("GetText of an unedited rewriter", func(t *testing.T) {
		ratio := growthOf(t, func(pr *nanopass.ParseResult) {
			_ = nanopass.GetText(nanopass.NewRewriter(pr))
		})
		require.Less(t, ratio, ceiling)
	})
	t.Run("NodeText of the whole statement", func(t *testing.T) {
		ratio := growthOf(t, func(pr *nanopass.ParseResult) {
			_ = nanopass.NodeText(pr, pr.Tree)
		})
		require.Less(t, ratio, ceiling)
	})
}
