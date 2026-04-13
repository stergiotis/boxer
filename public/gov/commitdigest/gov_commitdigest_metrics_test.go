//go:build llm_generated_opus46

package commitdigest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeMetrics_TotalAndUniqueAuthors(t *testing.T) {
	commits := []CommitEntry{
		{Hash: "aaaa1234567890abcdef1234567890abcdef1234", Author: "Alice <alice@example.com>", Date: "2026-04-10"},
		{Hash: "bbbb1234567890abcdef1234567890abcdef1234", Author: "Bob <bob@example.com>", Date: "2026-04-11"},
		{Hash: "cccc1234567890abcdef1234567890abcdef1234", Author: "Alice <alice@example.com>", Date: "2026-04-12"},
	}
	metrics := ComputeMetrics(commits, MetricsConfig{})
	assert.Equal(t, int32(3), metrics.TotalCommits)
	assert.Equal(t, int32(2), metrics.UniqueAuthors)
}

func TestComputeMetrics_ForeignAuthors(t *testing.T) {
	commits := []CommitEntry{
		{Hash: "aaaa1234567890abcdef1234567890abcdef1234", Author: "Alice <alice@example.com>", Date: "2026-04-10"},
		{Hash: "bbbb1234567890abcdef1234567890abcdef1234", Author: "Bob <bob@external.com>", Date: "2026-04-11"},
		{Hash: "cccc1234567890abcdef1234567890abcdef1234", Author: "Charlie <charlie@example.com>", Date: "2026-04-12"},
	}
	config := MetricsConfig{
		KnownAuthors: []string{"alice@example.com", "charlie@example.com"},
	}
	metrics := ComputeMetrics(commits, config)
	assert.Equal(t, 1, len(metrics.ForeignCommits))
	assert.Equal(t, "Bob <bob@external.com>", metrics.ForeignCommits[0].Author)
}

func TestComputeMetrics_NoKnownAuthors(t *testing.T) {
	commits := []CommitEntry{
		{Hash: "aaaa1234567890abcdef1234567890abcdef1234", Author: "Alice <alice@example.com>", Date: "2026-04-10"},
	}
	metrics := ComputeMetrics(commits, MetricsConfig{})
	assert.Equal(t, 0, len(metrics.ForeignCommits))
}

func TestComputeMetrics_IterationHotspots(t *testing.T) {
	commits := []CommitEntry{
		{
			Hash: "aaaa1234567890abcdef1234567890abcdef1234", Author: "Alice", Date: "2026-04-10",
			Stat: " src/foo.go | 10 +++++++---\n src/bar.go | 5 ++---",
		},
		{
			Hash: "bbbb1234567890abcdef1234567890abcdef1234", Author: "Bob", Date: "2026-04-11",
			Stat: " src/foo.go | 3 ++-\n src/baz.go | 1 +",
		},
		{
			Hash: "cccc1234567890abcdef1234567890abcdef1234", Author: "Alice", Date: "2026-04-12",
			Stat: " src/foo.go | 7 +++----\n src/bar.go | 2 +-",
		},
	}
	metrics := ComputeMetrics(commits, MetricsConfig{HotspotTopN: 5})
	assert.GreaterOrEqual(t, len(metrics.IterationHotspots), 1)
	assert.Equal(t, "src/foo.go", metrics.IterationHotspots[0].Path)
	assert.Equal(t, int32(3), metrics.IterationHotspots[0].CommitCount)
	if len(metrics.IterationHotspots) >= 2 {
		assert.Equal(t, "src/bar.go", metrics.IterationHotspots[1].Path)
		assert.Equal(t, int32(2), metrics.IterationHotspots[1].CommitCount)
	}
}

func TestComputeMetrics_EmptyStats(t *testing.T) {
	commits := []CommitEntry{
		{Hash: "aaaa1234567890abcdef1234567890abcdef1234", Author: "Alice", Date: "2026-04-10"},
	}
	metrics := ComputeMetrics(commits, MetricsConfig{})
	assert.Equal(t, 0, len(metrics.IterationHotspots))
}

func TestComputeMetrics_HotspotsExcludeSingleTouch(t *testing.T) {
	commits := []CommitEntry{
		{
			Hash: "aaaa1234567890abcdef1234567890abcdef1234", Author: "Alice", Date: "2026-04-10",
			Stat: " src/one.go | 1 +",
		},
		{
			Hash: "bbbb1234567890abcdef1234567890abcdef1234", Author: "Bob", Date: "2026-04-11",
			Stat: " src/two.go | 1 +",
		},
	}
	metrics := ComputeMetrics(commits, MetricsConfig{})
	// each file touched only once — not a hotspot
	assert.Equal(t, 0, len(metrics.IterationHotspots))
}

func TestExtractCommitEmail(t *testing.T) {
	assert.Equal(t, "alice@example.com", extractCommitEmail("Alice <alice@example.com>"))
	assert.Equal(t, "alice", extractCommitEmail("Alice"))
}

func TestIsKnownAuthor_ByEmail(t *testing.T) {
	known := map[string]struct{}{"alice@example.com": {}}
	assert.True(t, isKnownAuthor("alice@example.com", "Alice <alice@example.com>", known))
	assert.False(t, isKnownAuthor("bob@example.com", "Bob <bob@example.com>", known))
}

func TestIsKnownAuthor_BySubstring(t *testing.T) {
	known := map[string]struct{}{"alice": {}}
	assert.True(t, isKnownAuthor("alice@example.com", "Alice <alice@example.com>", known))
}
