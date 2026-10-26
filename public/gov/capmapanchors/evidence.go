package capmapanchors

import (
	"sort"
	"strconv"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
)

// Evidence is what the tree shows for a competence's anchors (ADR-0337
// §SD3): one [Signal] per competence per thing the tree carries, and from
// the signals the readiness rung the anchors support. It states what is
// there; it writes no claim and averages nothing.

// Signal is one row of the `competenceevidence` table.
type Signal struct {
	// Slug is the competence; Anchor is the anchor the signal was read off.
	Slug   string
	Anchor string
	// Name says which signal, one of the Signal* constants; Value is what
	// the tree said, as text, so one column serves every signal.
	Name  string
	Value string
	// Rung is the readiness rung this signal is evidence for, or 0 when it
	// is a fact with no rung of its own (an anchor that merely resolves).
	Rung int
}

// The signal names. Stable tokens: they are stored and queried by.
const (
	// SignalAnchored says the anchor resolved; its value is the resolution.
	SignalAnchored = "anchored"
	// SignalDecision carries a decision's status.
	SignalDecision = "decision"
	// SignalMilestones carries a decision's done/total sub-item tally.
	SignalMilestones = "milestones"
	// SignalTests carries a package's test lanes: none, default,
	// integration, or default+integration.
	SignalTests = "tests"
	// SignalSurface says an app, applet or scene is present: something a
	// person can reach the competence through.
	SignalSurface = "surface"
	// SignalDocumented says a howto or explanation page exists.
	SignalDocumented = "documented"
	// SignalPositioned says a positioning page exists.
	SignalPositioned = "positioned"
	// SignalMeasured says a trial exists.
	SignalMeasured = "measured"
)

// The rungs the signals speak for. The ninth — operated as the toolkit's own
// workload, with its facts to show it — is not derivable from a checkout, so
// the supported rung tops out at eight and a ninth claim always shows the
// gap, which is the honest reading until a facts-backed signal exists.
//
// deferred: coverage (`coverage_pkgs`), app operation counts
// (`app_operations`) and the commit digest's recency are the three signals
// ADR-0337 §SD3 names that this cut leaves out; each needs a loaded snapshot
// or a store, and none changes the rung rule.
const (
	rungNamed      = 1
	rungDecided    = 2
	rungPartial    = 3
	rungEndToEnd   = 4
	rungExercised  = 5
	rungDocumented = 6
	rungPositioned = 7
	rungMeasured   = 8
	MaxDerivedRung = rungMeasured
)

// SignalsFor reads the signals for one competence off its resolved anchor
// relations. The resolver's index supplies what resolution alone does not
// carry: the milestone tally and the test lanes. Order is by anchor then
// signal, so the table is stable across reads.
func (inst *TreeResolver) SignalsFor(slug string, rels []capmapcorpus.Relation) (signals []Signal) {
	idx := inst.index()
	seen := map[string]struct{}{}
	for _, rel := range rels {
		if rel.SourceSlug != slug || rel.Kind != capmapcorpus.RelationKindRealisedBy {
			continue
		}
		if _, dup := seen[rel.Target]; dup {
			continue
		}
		seen[rel.Target] = struct{}{}
		a, err := capmapcorpus.ParseAnchor(rel.Target)
		if err != nil {
			continue
		}
		signals = append(signals, Signal{Slug: slug, Anchor: rel.Target, Name: SignalAnchored,
			Value: rel.Resolution.String(), Rung: anchoredRung(rel.Resolution)})
		if rel.Resolution != capmapcorpus.ResolutionDirect {
			continue
		}
		signals = append(signals, anchorSignals(slug, a, rel.TargetStatus, idx)...)
	}
	sort.SliceStable(signals, func(i, j int) bool {
		if signals[i].Anchor != signals[j].Anchor {
			return signals[i].Anchor < signals[j].Anchor
		}
		return signals[i].Name < signals[j].Name
	})
	return signals
}

func anchoredRung(r capmapcorpus.ResolutionE) (rung int) {
	if r == capmapcorpus.ResolutionDirect {
		return rungNamed
	}
	return 0
}

// anchorSignals reads what one resolved anchor is evidence for.
func anchorSignals(slug string, a capmapcorpus.Anchor, status string, idx *index) (signals []Signal) {
	sig := func(name, value string, rung int) {
		signals = append(signals, Signal{Slug: slug, Anchor: a.String(), Name: name, Value: value, Rung: rung})
	}
	switch a.Kind {
	case capmapcorpus.AnchorKindAdr:
		decided := status == "accepted"
		sig(SignalDecision, status, rungIf(decided, rungDecided))
		if idx != nil {
			if n, err := strconv.Atoi(a.Key); err == nil {
				if adr, ok := idx.adrs[n]; ok && adr.SubtasksTotal > 0 {
					rung := 0
					switch {
					case decided && adr.SubtasksDone == adr.SubtasksTotal:
						rung = rungEndToEnd
					case decided && adr.SubtasksDone > 0:
						rung = rungPartial
					}
					sig(SignalMilestones, strconv.Itoa(adr.SubtasksDone)+"/"+strconv.Itoa(adr.SubtasksTotal), rung)
				}
			}
		}
	case capmapcorpus.AnchorKindPkg:
		var info pkgInfo
		if idx != nil {
			info = idx.pkgs[a.Key]
		}
		switch {
		case info.DefaultTests && info.IntegrationTests:
			sig(SignalTests, "default+integration", rungExercised)
		case info.IntegrationTests:
			sig(SignalTests, "integration", rungExercised)
		case info.DefaultTests:
			sig(SignalTests, "default", rungPartial)
		default:
			sig(SignalTests, "none", 0)
		}
	case capmapcorpus.AnchorKindApp, capmapcorpus.AnchorKindApplet:
		sig(SignalSurface, string(a.Kind), rungEndToEnd)
	case capmapcorpus.AnchorKindScene:
		sig(SignalSurface, string(a.Kind), rungExercised)
	case capmapcorpus.AnchorKindHowto, capmapcorpus.AnchorKindExplanation:
		sig(SignalDocumented, string(a.Kind), rungDocumented)
	case capmapcorpus.AnchorKindPositioning:
		sig(SignalPositioned, a.Key, rungPositioned)
	case capmapcorpus.AnchorKindTrial:
		sig(SignalMeasured, a.Key, rungMeasured)
	}
	return signals
}

func rungIf(cond bool, rung int) (r int) {
	if cond {
		return rung
	}
	return 0
}

// SupportedRung is the readiness rung the signals support, by ISO/IEC
// 33020's rule: the highest rung every one of whose lower rungs some signal
// holds. A rung with no signal beneath it is not reached however strong the
// signals above it, which is what makes a missing anchor visible as a gap
// rather than averaged away. Zero when nothing resolved.
func SupportedRung(signals []Signal) (rung int) {
	held := map[int]bool{}
	for _, s := range signals {
		if s.Rung > 0 {
			held[s.Rung] = true
		}
	}
	// A signal for a rung is also evidence of every rung it presupposes
	// within its own ladder: end to end implies built in part, and
	// exercised implies both.
	if held[rungExercised] {
		held[rungEndToEnd] = true
	}
	if held[rungEndToEnd] {
		held[rungPartial] = true
	}
	for r := rungNamed; r <= MaxDerivedRung; r++ {
		if !held[r] {
			return r - 1
		}
	}
	return MaxDerivedRung
}
