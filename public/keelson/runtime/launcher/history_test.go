package launcher

import (
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

func mkStat(id string, opens uint64, ageDays int, score float64) factsstore.AppLaunchStat {
	return factsstore.AppLaunchStat{
		AppId:  app.AppIdT(id),
		Opens:  opens,
		LastTs: time.Now().Add(-time.Duration(ageDays) * 24 * time.Hour),
		Score:  score,
	}
}

// TestBuildHistory_BonusStaysInsideTheCap is the invariant §SD8 rests on:
// history may reorder within a relevance band and never across one. If the
// bonus could reach one tier step (2*weightScale) a keyword hit on a
// frequently-used app would outrank a display-name hit on a rare one, which is
// the failure mode that makes learned ordering feel arbitrary.
func TestBuildHistory_BonusStaysInsideTheCap(t *testing.T) {
	h := buildHistory([]factsstore.AppLaunchStat{
		mkStat("a", 400, 0, 4000),
		mkStat("b", 1, 300, 0.0001),
		mkStat("c", 40, 2, 30),
	})
	for id, b := range h.bonus {
		assert.GreaterOrEqual(t, b, 1, "%s: an app with any history outranks one with none", id)
		assert.LessOrEqual(t, b, maxFrecencyBonus, "%s: bonus %d breaks the cap", id, b)
	}
	assert.Less(t, maxFrecencyBonus, weightManifestKeyword,
		"the cap must sit below the smallest field weight, or history could invent a match")
	assert.Less(t, maxFrecencyBonus, weightManifestTopic-weightManifestKeyword,
		"the cap must sit below the smallest gap between adjacent tiers")
}

// TestBuildHistory_BonusFollowsScoreOrder pins that the bonus is rank-derived
// rather than score-scaled. Raw scores are heavily skewed — one heavily-used
// app dwarfs the rest — so a linear scaling would hand it the whole budget and
// leave every other app indistinguishable from never-opened.
func TestBuildHistory_BonusFollowsScoreOrder(t *testing.T) {
	h := buildHistory([]factsstore.AppLaunchStat{
		mkStat("rare", 1, 200, 0.01),
		mkStat("hot", 500, 0, 5000),
		mkStat("middling", 20, 5, 12),
	})
	assert.Greater(t, h.bonus["hot"], h.bonus["middling"])
	assert.Greater(t, h.bonus["middling"], h.bonus["rare"])
	assert.Equal(t, maxFrecencyBonus, h.bonus["hot"], "the top app takes the whole bonus")
	assert.Greater(t, h.bonus["rare"], 0, "the last app still beats never-opened")
}

// TestBuildHistory_RecentsAreByTimeNotScore keeps the two orders apart. They
// come off one trail and answer different questions: "what was I just doing"
// is not "what do I use most", and an app opened once an hour ago belongs at
// the top of the first list and nowhere near the top of the second.
func TestBuildHistory_RecentsAreByTimeNotScore(t *testing.T) {
	h := buildHistory([]factsstore.AppLaunchStat{
		mkStat("workhorse", 500, 3, 5000),
		mkStat("just-now", 1, 0, 1),
	})
	require.Len(t, h.recents, 2)
	assert.Equal(t, app.AppIdT("just-now"), h.recents[0],
		"recents is most-recent-first, whatever the score says")
	assert.Greater(t, h.bonus["workhorse"], h.bonus["just-now"],
		"the bonus is the other order")
}

// TestBuildHistory_SingleApp covers the n==1 division the rank mapping does.
func TestBuildHistory_SingleApp(t *testing.T) {
	h := buildHistory([]factsstore.AppLaunchStat{mkStat("only", 3, 1, 2)})
	assert.Equal(t, maxFrecencyBonus, h.bonus["only"])
	assert.Equal(t, []app.AppIdT{"only"}, h.recents)
}

// TestRankFn_ClampsWhateverTheProviderReturns makes the cap an invariant of
// the ordering rather than a convention a provider is trusted to respect.
func TestRankFn_ClampsWhateverTheProviderReturns(t *testing.T) {
	var nilFn rankFn
	assert.Equal(t, 0, nilFn.bonus("anything"), "no provider means no bonus")

	wild := rankFn(func(id app.AppIdT) (bonus int) {
		if id == "high" {
			return 1 << 30
		}
		return -5
	})
	assert.Equal(t, maxFrecencyBonus, wild.bonus("high"))
	assert.Equal(t, 0, wild.bonus("low"))
}

// TestSortManifestHits_RelevanceDominatesHistory is §SD8 stated as the
// assertion the ADR's verification plan asks for: a maximally-frecent weak
// match never outranks a stronger one.
func TestSortManifestHits_RelevanceDominatesHistory(t *testing.T) {
	hits := []manifestHit{
		{m: mkTopicManifest("keyword-hit", "Zebra", app.TopicData), score: weightManifestKeyword, bonus: maxFrecencyBonus},
		{m: mkTopicManifest("display-hit", "Aardvark", app.TopicData), score: weightManifestDisplay, bonus: 0},
	}
	sortManifestHits(hits)
	assert.Equal(t, "Aardvark", hits[0].m.Display,
		"a display-name match with no history outranks a keyword match with all of it")

	// Within one band, history is the tiebreak — the whole point of having it.
	same := []manifestHit{
		{m: mkTopicManifest("cold", "Aardvark", app.TopicData), score: weightManifestDisplay, bonus: 1},
		{m: mkTopicManifest("warm", "Zebra", app.TopicData), score: weightManifestDisplay, bonus: maxFrecencyBonus},
	}
	sortManifestHits(same)
	assert.Equal(t, "Zebra", same[0].m.Display,
		"equally relevant, so the one actually used comes first — ahead of alphabetical order")
}

// TestResolveHalfLife_RejectsNonPositive covers the guard DurationVar cannot:
// a value that parses but cannot drive a decay.
func TestResolveHalfLife_RejectsNonPositive(t *testing.T) {
	HalfLife.SetForTest(t, "0s")
	assert.Equal(t, defaultHalfLife, resolveHalfLife(testLogger()))
}

// TestResolveHalfLife_HonoursTheKnob is the other half — a set value is used.
func TestResolveHalfLife_HonoursTheKnob(t *testing.T) {
	HalfLife.SetForTest(t, "48h")
	assert.Equal(t, 48*time.Hour, resolveHalfLife(testLogger()))
}

// testLogger is a discarding logger — these tests exercise the fallback paths,
// which warn, and the warning is not what is under test.
func testLogger() zerolog.Logger {
	return zerolog.Nop()
}
