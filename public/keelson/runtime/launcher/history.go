package launcher

// Ranking from launch history (ADR-0214 §SD8).
//
// Two orders come off one trail, and keeping them separate is the decision:
//
//   - **Recents** — the menu's short list, most recent first. Answers "what
//     was I just doing".
//   - **The bonus** — a per-app weight folded into the row ordering, bounded
//     below one relevance tier (see search.go). Answers "of these equally
//     relevant apps, which do I actually use".
//
// Neither replaces relevance. A typed query is an explicit statement about
// what the person wants, and history is an inference about it; when the two
// disagree the statement wins, which is what maxFrecencyBonus enforces.

import (
	"context"
	"sort"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// HalfLife is the frecency decay knob (ADR-0009 registry). A launch this old
// counts half as much as one just now.
//
// The default is two weeks, chosen for the shape of this corpus rather than
// from a formula: the apps someone reaches for track what they are working on
// this sprint, and a half-life much shorter makes yesterday's work vanish
// while a much longer one keeps a month-old detour ranked above today's tool.
// It is an env var because that judgement is exactly the kind that wants
// changing without a rebuild.
var HalfLife = env.NewDuration(env.Spec{
	Name:        "BOXER_LAUNCHER_FRECENCY_HALFLIFE",
	Description: "launcher ranking decay: a launch this old counts half as much as one just now",
	Category:    env.CategorySystem,
	Default:     "336h",
})

// defaultHalfLife mirrors the Spec default, for the one case DurationVar
// cannot express: a value that parses but is zero or negative, which would
// make the decay meaningless (or the store reject the read).
const defaultHalfLife = 336 * time.Hour

// resolveHalfLife reads the knob. Unset and unparseable both land on the Spec
// default inside DurationVar; the guard here covers a parseable but unusable
// value, and warns rather than failing — a mistyped tuning knob should not
// cost someone their launcher.
func resolveHalfLife(logger zerolog.Logger) (d time.Duration) {
	d = HalfLife.Get()
	if d > 0 {
		return
	}
	logger.Warn().Dur("value", d).Dur("using", defaultHalfLife).
		Msg("launcher: frecency half-life must be positive; falling back to the default")
	d = defaultHalfLife
	return
}

// historyT is the cached answer from one AppLaunchStats read.
type historyT struct {
	// bonus is the per-app §SD8 contribution, already scaled into
	// [0, maxFrecencyBonus].
	bonus map[app.AppIdT]int
	// recents is app ids most-recent-first.
	recents []app.AppIdT
}

// BindHistory installs ranking from a facts store, if that store offers the
// history capability (§SD7).
//
// Absence is not an error and not logged as one: an in-memory facts store is
// what a run without ClickHouse gets, and a launcher that ranked by nothing is
// exactly what it should do then. Returns whether ranking was installed, so a
// caller that wants to say so can.
//
// The read happens once, here, rather than per frame. A launcher's ordering
// settling for the life of a process is the right trade against a ClickHouse
// round trip on a surface that opens in front of someone — and the trail this
// process is adding to is mostly its own opens, which the person just made and
// therefore already knows about.
func (inst *Inst) BindHistory(ctx context.Context, facts factsstore.FactsStoreI) (installed bool) {
	reader, ok := facts.(factsstore.AppLaunchHistoryReaderI)
	if !ok {
		return
	}
	halfLife := resolveHalfLife(inst.logger)
	stats, err := reader.AppLaunchStats(ctx, halfLife, 0)
	if err != nil {
		inst.logger.Warn().Err(err).
			Msg("launcher: launch-history read failed; ordering falls back to authored metadata")
		return
	}
	if len(stats) == 0 {
		// Nothing has been opened yet. Installing an all-zero ranking would be
		// indistinguishable from not installing one, so don't.
		return
	}
	h := buildHistory(stats)
	inst.rank = func(id app.AppIdT) (bonus int) {
		bonus = h.bonus[id]
		return
	}
	inst.recentFn = func() (ids []app.AppIdT) {
		ids = h.recents
		return
	}
	installed = true
	inst.logger.Info().Int("apps", len(stats)).Dur("halfLife", halfLife).
		Msg("launcher: ranking from launch history")
	return
}

// buildHistory turns the store's rows into the two orders the launcher wants.
//
// The bonus is a *rank*-derived value, not the raw score scaled: scores are
// unbounded and heavily skewed — one app opened forty times dwarfs everything
// — so scaling them linearly would give the top app the whole budget and every
// other app nothing. Spreading by position keeps the bonus discriminating
// across the whole list, which is what a tie-breaker within a relevance band
// needs to be.
func buildHistory(stats []factsstore.AppLaunchStat) (h historyT) {
	byScore := make([]factsstore.AppLaunchStat, len(stats))
	copy(byScore, stats)
	sort.SliceStable(byScore, func(i, j int) bool {
		if byScore[i].Score != byScore[j].Score {
			return byScore[i].Score > byScore[j].Score
		}
		return byScore[i].AppId < byScore[j].AppId
	})
	h.bonus = make(map[app.AppIdT]int, len(byScore))
	n := len(byScore)
	for i, st := range byScore {
		// Position i of n maps onto [1 … maxFrecencyBonus]. The last-placed app
		// still gets 1 rather than 0, so "opened once, long ago" outranks
		// "never opened" — which is information, and the only place it can be
		// expressed. A lone app is top-placed, not bottom-placed, so the n==1
		// case takes the whole bonus rather than the floor the general formula
		// would give it.
		if n == 1 {
			h.bonus[st.AppId] = maxFrecencyBonus
			continue
		}
		h.bonus[st.AppId] = 1 + (maxFrecencyBonus-1)*(n-1-i)/(n-1)
	}

	byRecency := make([]factsstore.AppLaunchStat, len(stats))
	copy(byRecency, stats)
	sort.SliceStable(byRecency, func(i, j int) bool {
		if !byRecency[i].LastTs.Equal(byRecency[j].LastTs) {
			return byRecency[i].LastTs.After(byRecency[j].LastTs)
		}
		return byRecency[i].AppId < byRecency[j].AppId
	})
	h.recents = make([]app.AppIdT, 0, len(byRecency))
	for _, st := range byRecency {
		h.recents = append(h.recents, st.AppId)
	}
	return
}
