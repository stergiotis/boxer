package play

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/hmi/gloss"
)

// analyzeSample is EXPLAIN ANALYZE output as ClickHouse 26.9 writes it.
const analyzeSample = `Query summary:
  Time:        3.77 ms (planning 1.52 ms · execution 2.25 ms)
  Read:        1.00 million rows, 8.00 MB (444.53 million rows/s., 3.56 GB/s.)
  Peak memory: 154.34 KiB

Output: number MOD 10, count()

Expression (Project names)
│  I/O: rows 3 → 3 · 27 B → 27 B
│    time 4.59 us (0.2%) · parallelism 0.99/1
└──Limit (preliminary LIMIT)
   │  Limit 3
   │  Offset 0
   │  I/O: rows 3 → 3 · 27 B → 27 B
   └──Sorting (Sorting for ORDER BY)
      │  Sort description: number MOD 10 ASC
      │  Limit 3
      │  I/O: rows 6 → 3 (50.00%) · 54 B → 27 B
      │    Stage (sorting): time 59.93 us (2.7%) · parallelism 2.03/32
      │    Stage (merge sorted streams): time 23.33 us (1.0%) · parallelism 0.98/1
      └──Expression ((Before ORDER BY + Projection))
         │  I/O: rows 6 → 6 · 54 B → 54 B
         │    time 902.00 ns (0.0%) · parallelism 0.95/1
         └──Aggregating
            │  Keys: number MOD 10
            │  Aggregates: count()
            │  Top-K: limit=3, columns=1, directions=[1]
            │  Skip merging: 0
            │  I/O: rows 333.33 thousand → 6 (0.00%) · 333.33 KB → 54 B
            │    Stage (partial aggregation): time 281.46 us (12.5%) · parallelism 1.00/1
            │    Stage (final aggregation): time 11.25 us (0.5%) · parallelism 0.99/1
            └──Expression (Before GROUP BY)
               │  I/O: rows 333.33 thousand → 333.33 thousand · 2.67 MB → 333.33 KB
               │    time 126.16 us (5.6%) · parallelism 0.99/1
               └──Filter ((WHERE + Change column names to column identifiers))
                  │  Filter column: number MOD 3 = 0
                  │  I/O: rows 1.00 million → 333.33 thousand (33.33%) · 8.00 MB → 2.67 MB
                  │    time 578.14 us (25.7%) · parallelism 1.00/1
                  └──ReadFromSystemNumbers
                        Output: number
                        I/O: rows 0 → 1.00 million · 0 B → 8.00 MB
                          time 132.53 us (5.9%) · parallelism 0.99/1`

func TestParseAnalyzeSplitsSummaryAndTree(t *testing.T) {
	p := parseAnalyze(strings.Split(analyzeSample, "\n"))
	require.Len(t, p.Summary, 3)
	assert.True(t, strings.HasPrefix(p.Summary[0], "Time:"))
	require.NotEmpty(t, p.Tree)
	assert.True(t, strings.HasPrefix(p.Tree[0], "Output:"), "the tree starts after the summary's blank line")

	steps := map[string]bool{}
	for _, s := range p.Timed {
		steps[s.Step] = true
		assert.Contains(t, p.Tree[s.Line], s.Time, "a timed step points at the line it was read from")
	}
	for _, want := range []string{"Expression (Project names)", "Aggregating", "ReadFromSystemNumbers"} {
		assert.True(t, steps[want], "%s is timed", want)
	}
	for _, s := range p.Timed {
		if s.Step == "Aggregating" {
			assert.NotEmpty(t, s.Stage, "aggregation reports its stages")
		}
	}
}

func TestParseAnalyzeRanksByShare(t *testing.T) {
	p := parseAnalyze(strings.Split(analyzeSample, "\n"))
	hot := p.hottest(3)
	require.Len(t, hot, 3)
	assert.GreaterOrEqual(t, hot[0].Share, hot[1].Share)
	assert.GreaterOrEqual(t, hot[1].Share, hot[2].Share)
}

func TestParseAnalyzeKeepsWhatItCannotRead(t *testing.T) {
	lines := []string{"Something else entirely", "│  time soon"}
	p := parseAnalyze(lines)
	assert.Empty(t, p.Summary)
	assert.Equal(t, lines, p.Tree, "unrecognised output is shown, only not ranked")
	assert.Empty(t, p.Timed)
}

func TestAnalyzeTiming(t *testing.T) {
	tm, share, ok := analyzeTiming("│    Stage (partial aggregation): time 2.32 ms (21.5%) · parallelism 1.00/1")
	require.True(t, ok)
	assert.Equal(t, "2.32 ms", tm)
	assert.InDelta(t, 21.5, share, 1e-9)
	_, _, ok = analyzeTiming("│  I/O: rows 3 → 3 · 27 B → 27 B")
	assert.False(t, ok)
	assert.Equal(t, gloss.ToneError, analyzeTone(51.5))
	assert.Equal(t, gloss.ToneWarning, analyzeTone(12.5))
	assert.Equal(t, gloss.ToneNeutral, analyzeTone(0.7))
}

func TestExplainAnalyzeWrapSurvivesATrailingComment(t *testing.T) {
	assert.Equal(t, "SELECT * FROM (EXPLAIN ANALYZE SELECT 1 -- note\n)", explainAnalyzeWrap("SELECT 1 -- note"))
}
