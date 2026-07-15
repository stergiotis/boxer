package codeview

import (
	"strconv"
	"strings"
	"testing"
)

// The measurements behind ADR-0125, kept as a regression guard: a Prepare* hit
// must stay orders below the Build* it skips. If a change makes Prepare*
// approach Build*, the memo has stopped working — which is invisible to the
// unit tests, since a broken memo still returns correct output, just slowly.
//
// Absolute figures are machine-dependent; the ratio is the assertion. Run:
//
//	go test -tags="$(cat ./tags)" -run XXX -bench . ./public/.../codeview/

const benchSQLOneLiner = `SELECT count() FROM anchor.facts`

const benchSQLCte = `WITH recent AS (SELECT * FROM anchor.facts LIMIT 50), by_kind AS (
  SELECT event_type, count() AS n FROM recent GROUP BY event_type)
SELECT event_type, n FROM by_kind ORDER BY n DESC`

// benchJSON builds a JSON document of roughly n entries.
func benchJSON(n int) string {
	var b strings.Builder
	b.WriteString("{\n")
	for i := range n {
		b.WriteString(`  "key`)
		b.WriteString(strings.Repeat("x", 8))
		b.WriteString(`": {"a": 1234, "b": "value string here", "c": true, "d": null},`)
		b.WriteByte('\n')
		_ = i
	}
	b.WriteString(`  "last": 1` + "\n}")
	return b.String()
}

// benchMD builds a markdown document of roughly n sections.
func benchMD(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString("## Heading\n\nSome *body* text with `code` and a [link](https://example.com).\n\n- item one\n- item two\n\n")
		_ = i
	}
	return b.String()
}

// SQL is the expensive case: highlight.Highlight runs a full nanopass.Parse and
// a CST walk, not a lex. The CTE is the one that motivated ADR-0125 — a Graph
// tab drew one of these per node, per frame.
func BenchmarkBuildSql(b *testing.B) {
	for _, tc := range []struct{ name, src string }{
		{"oneliner", benchSQLOneLiner},
		{"cte", benchSQLCte},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = BuildSql(tc.src)
			}
		})
	}
}

// The same sources through the memo. Every iteration past the first is a hit.
func BenchmarkPrepareSql(b *testing.B) {
	for _, tc := range []struct{ name, src string }{
		{"oneliner", benchSQLOneLiner},
		{"cte", benchSQLCte},
	} {
		b.Run(tc.name, func(b *testing.B) {
			memo.reset()
			b.ReportAllocs()
			for b.Loop() {
				_ = PrepareSql(tc.src)
			}
		})
	}
}

func BenchmarkBuildMarkdown(b *testing.B) {
	for _, n := range []int{5, 100} {
		src := benchMD(n)
		b.Run(sizeLabel(len(src)), func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for b.Loop() {
				_ = BuildMarkdown(src)
			}
		})
	}
}

// helphost's shape: a whole document re-prepared every frame.
func BenchmarkPrepareMarkdown(b *testing.B) {
	for _, n := range []int{5, 100} {
		src := benchMD(n)
		b.Run(sizeLabel(len(src)), func(b *testing.B) {
			memo.reset()
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for b.Loop() {
				_ = PrepareMarkdown(src)
			}
		})
	}
}

func BenchmarkBuildJson(b *testing.B) {
	for _, n := range []int{10, 200} {
		src := benchJSON(n)
		b.Run(sizeLabel(len(src)), func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for b.Loop() {
				_ = BuildJson(src)
			}
		})
	}
}

func BenchmarkPrepareJson(b *testing.B) {
	for _, n := range []int{10, 200} {
		src := benchJSON(n)
		b.Run(sizeLabel(len(src)), func(b *testing.B) {
			memo.reset()
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for b.Loop() {
				_ = PrepareJson(src)
			}
		})
	}
}

// The memo's cost when it cannot help: compare against BenchmarkBuildJson/tiny,
// the same work without the bookkeeping.
//
// Forcing a real miss needs care. A source set that fits the cache stops missing
// after the first pass — cycling 512 sources through a 4096-entry cache measures
// hits, not misses, and reports the hit cost under a name that claims otherwise.
// This cycles MORE distinct sources than memoMaxEntries, in order, which is the
// LRU's worst case: each access is exactly the entry just evicted.
func BenchmarkPrepareJsonAllMisses(b *testing.B) {
	memo.reset()
	srcs := make([]string, memoMaxEntries*2)
	for i := range srcs {
		srcs[i] = `{"i":` + strconv.Itoa(i) + `}`
	}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		_ = PrepareJson(srcs[i%len(srcs)])
		i++
	}
	_, misses, _, _ := memo.stats()
	if misses < uint64(b.N)/2 {
		b.Fatalf("meant to measure misses, but only %d of %d calls missed", misses, b.N)
	}
}

func sizeLabel(n int) string {
	switch {
	case n < 1024:
		return "tiny"
	case n < 64*1024:
		return "medium"
	}
	return "large"
}
