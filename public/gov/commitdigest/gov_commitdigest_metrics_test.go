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

func TestComputeMetrics_NoBoundaryCrossingsWithoutDetection(t *testing.T) {
	commits := []CommitEntry{
		{Hash: "aaaa1234567890abcdef1234567890abcdef1234", Author: "Alice <alice@example.com>", Date: "2026-04-10"},
	}
	metrics := ComputeMetrics(commits, MetricsConfig{})
	assert.Nil(t, metrics.BoundaryCrossings)
}

func TestExtractCommitEmail(t *testing.T) {
	assert.Equal(t, "alice@example.com", extractCommitEmail("Alice <alice@example.com>"))
	assert.Equal(t, "alice", extractCommitEmail("Alice"))
}

func TestComputeMetrics_DetectsReverts(t *testing.T) {
	commits := []CommitEntry{
		{Hash: "aaaa1234567890abcdef1234567890abcdef1234", Author: "Alice", Date: "2026-04-10", Subject: "feat: add widget"},
		{Hash: "bbbb1234567890abcdef1234567890abcdef1234", Author: "Alice", Date: "2026-04-11", Subject: "Revert: add widget"},
		{Hash: "cccc1234567890abcdef1234567890abcdef1234", Author: "Alice", Date: "2026-04-12", Subject: `Revert "feat: add widget"`},
	}
	metrics := ComputeMetrics(commits, MetricsConfig{})
	assert.Len(t, metrics.Reverts, 2)
	assert.Equal(t, "bbbb1234567890abcdef1234567890abcdef1234", metrics.Reverts[0].Hash)
	assert.Equal(t, "cccc1234567890abcdef1234567890abcdef1234", metrics.Reverts[1].Hash)
}

func TestComputeMetrics_DetectsFollowUpSignals(t *testing.T) {
	commits := []CommitEntry{
		{Hash: "1111111111111111111111111111111111111111", Author: "Alice", Date: "d", Subject: "fixup! refactor parser"},
		{Hash: "2222222222222222222222222222222222222222", Author: "Alice", Date: "d", Subject: "squash! tighten types"},
		{Hash: "3333333333333333333333333333333333333333", Author: "Alice", Date: "d", Subject: "wip: exploring new API"},
		{Hash: "4444444444444444444444444444444444444444", Author: "Alice", Date: "d", Subject: "quick fix for null ptr"},
		{Hash: "5555555555555555555555555555555555555555", Author: "Alice", Date: "d", Subject: "feat: ship real feature"},
	}
	metrics := ComputeMetrics(commits, MetricsConfig{})
	assert.Len(t, metrics.FollowUps, 4)
	kinds := make([]string, 0, len(metrics.FollowUps))
	for _, fu := range metrics.FollowUps {
		kinds = append(kinds, fu.Kind)
	}
	assert.Equal(t, []string{"fixup", "squash", "wip", "hotfix"}, kinds)
}

func TestComputeMetrics_FollowUpKindPriority(t *testing.T) {
	// A subject matching multiple patterns resolves to the highest-priority kind.
	// "fixup!" prefix wins over "wip" substring.
	commits := []CommitEntry{
		{Hash: "aaaa1234567890abcdef1234567890abcdef1234", Author: "Alice", Date: "d", Subject: "fixup! wip exploration"},
	}
	metrics := ComputeMetrics(commits, MetricsConfig{})
	assert.Len(t, metrics.FollowUps, 1)
	assert.Equal(t, "fixup", metrics.FollowUps[0].Kind)
}

func TestComputeMetrics_FlagsGeneratedHotspots(t *testing.T) {
	commits := []CommitEntry{
		{
			Hash: "aaaa1234567890abcdef1234567890abcdef1234", Author: "Alice", Date: "d",
			Stat: " components/methods.out.go | 10 +++++++---\n src/hand_written.go | 5 ++---\n rust/enums_out.rs | 3 ++-",
		},
		{
			Hash: "bbbb1234567890abcdef1234567890abcdef1234", Author: "Alice", Date: "d",
			Stat: " components/methods.out.go | 3 ++-\n src/hand_written.go | 4 ++-\n rust/enums_out.rs | 2 +-",
		},
		{
			Hash: "cccc1234567890abcdef1234567890abcdef1234", Author: "Alice", Date: "d",
			Stat: " components/methods.out.go | 2 +-\n src/hand_written.go | 1 +\n rust/enums_out.rs | 1 +",
		},
	}
	metrics := ComputeMetrics(commits, MetricsConfig{HotspotTopN: 10})
	flags := make(map[string]bool, len(metrics.IterationHotspots))
	for _, h := range metrics.IterationHotspots {
		flags[h.Path] = h.IsGenerated
	}
	assert.True(t, flags["components/methods.out.go"], ".out.go should be flagged generated")
	assert.True(t, flags["rust/enums_out.rs"], "_out.rs should be flagged generated")
	assert.False(t, flags["src/hand_written.go"], "hand-written file should not be flagged")
}

func TestComputeMetrics_FollowUpIsAuthorBaselineWhenMajorityWip(t *testing.T) {
	commits := []CommitEntry{
		{Hash: "1111111111111111111111111111111111111111", Author: "Alice", Date: "d", Subject: "wip: foo"},
		{Hash: "2222222222222222222222222222222222222222", Author: "Alice", Date: "d", Subject: "wip: bar"},
		{Hash: "3333333333333333333333333333333333333333", Author: "Alice", Date: "d", Subject: "wip: baz"},
		{Hash: "4444444444444444444444444444444444444444", Author: "Alice", Date: "d", Subject: "feat: ship"},
	}
	metrics := ComputeMetrics(commits, MetricsConfig{})
	assert.Len(t, metrics.FollowUps, 3, "follow-up entries still populated for downstream JSON consumers")
	assert.True(t, metrics.FollowUpIsAuthorBaseline, "3/4 WIP commits should flag author baseline")
}

func TestComputeMetrics_FollowUpNotBaselineWhenMinorityWip(t *testing.T) {
	commits := []CommitEntry{
		{Hash: "1111111111111111111111111111111111111111", Author: "Alice", Date: "d", Subject: "wip: foo"},
		{Hash: "2222222222222222222222222222222222222222", Author: "Alice", Date: "d", Subject: "feat: one"},
		{Hash: "3333333333333333333333333333333333333333", Author: "Alice", Date: "d", Subject: "feat: two"},
		{Hash: "4444444444444444444444444444444444444444", Author: "Alice", Date: "d", Subject: "feat: three"},
	}
	metrics := ComputeMetrics(commits, MetricsConfig{})
	assert.Len(t, metrics.FollowUps, 1)
	assert.False(t, metrics.FollowUpIsAuthorBaseline, "1/4 WIP commits should not flag author baseline")
}

func TestComputeMetrics_FollowUpBaselineAtExactHalf(t *testing.T) {
	commits := []CommitEntry{
		{Hash: "1111111111111111111111111111111111111111", Author: "Alice", Date: "d", Subject: "wip: foo"},
		{Hash: "2222222222222222222222222222222222222222", Author: "Alice", Date: "d", Subject: "wip: bar"},
		{Hash: "3333333333333333333333333333333333333333", Author: "Alice", Date: "d", Subject: "feat: one"},
		{Hash: "4444444444444444444444444444444444444444", Author: "Alice", Date: "d", Subject: "feat: two"},
	}
	metrics := ComputeMetrics(commits, MetricsConfig{})
	assert.True(t, metrics.FollowUpIsAuthorBaseline, "exactly half WIP should still flag baseline")
}

func TestIsGeneratedPath(t *testing.T) {
	assert.True(t, isGeneratedPath("a/b/methods.out.go"))
	assert.True(t, isGeneratedPath("a/b/factories.gen.go"))
	assert.True(t, isGeneratedPath("src/rust/enums_out.rs"))
	assert.True(t, isGeneratedPath("card.out.sql"))
	assert.False(t, isGeneratedPath("src/foo.go"))
	assert.False(t, isGeneratedPath("out.go"))
	assert.False(t, isGeneratedPath("a/b/output.go"))
	assert.False(t, isGeneratedPath("something.generated.go"))
}
