package highlight_test

// Benchmark for the editor-side highlighter — runs per keystroke in the
// play app, so latency and the token→span pairing complexity matter.

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
)

func BenchmarkHighlight(b *testing.B) {
	small := "SELECT a, sum(b) FROM t WHERE x > 1 GROUP BY a"
	large := "WITH c AS (SELECT id FROM src) " + strings.Repeat("SELECT a.x, b.y, 'lit' FROM t1 AS a JOIN t2 b ON a.id = b.id WHERE a.v IN (1, 2, 3) UNION ALL ", 24) + "SELECT 1, 2, '3' FROM t3"
	for _, c := range []struct {
		name string
		sql  string
	}{{"small", small}, {"large", large}} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(c.sql)))
			for b.Loop() {
				if spans := highlight.Highlight(c.sql); len(spans) == 0 {
					b.Fatal("no spans")
				}
			}
		})
	}
}
