package launcher

// The launcher's battery executor — the third under ADR-0164 §SD2, alongside
// help/search's in-process section scan (§SD3) and docsearchsql's ClickHouse
// multiMatch lane (§SD5). What crosses between them is the *battery*, never
// the executor: each one lives with its own corpus, and this one's corpus is
// the app registry.
//
// Moved here from windowhost by ADR-0214 §SD2, unchanged except for the
// frecency bonus §SD8 folds into the ordering.

import (
	"sort"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
)

// Field weights, mirroring the tier shape of help/search's §SD3 scorer: a
// pattern contributes the *strongest* tier it hit, so the tiers do not add up
// for one pattern and a word occurring in both the display name and a keyword
// still counts once, at display strength.
//
// Scaled by weightScale so §SD8's frecency bonus has room to reorder *within*
// a relevance band without ever crossing one. Id stays unweighted because it
// stays unmatched: it is the full import path, so every entry would answer to
// "github" (ADR-0158 §SD6). Title stays out as a longer-form variant of
// Display.
//
// Summary is deliberately NOT matched, though ADR-0214 added it and it is the
// longest text on the row. It is prose written to be *read* while choosing,
// and matching it would make the widest field the one most likely to hit —
// every "query" applet answering a query for "query" — which is the precise
// failure the tiered weights exist to avoid. Keywords are the field for
// retrieval text, and they are ungoverned so an author can put anything there.
const (
	weightScale           = 100
	weightManifestDisplay = 8 * weightScale
	weightManifestTopic   = 4 * weightScale
	weightManifestKeyword = 2 * weightScale
	// maxFrecencyBonus caps §SD8's history contribution strictly below the
	// smallest gap between achievable battery scores (2*weightScale). That
	// bound is the decision, not an implementation detail: within it history
	// reorders equally-relevant hits, and beyond it history would outrank
	// relevance — which is what makes learned ordering feel capricious.
	maxFrecencyBonus = weightScale - 1
)

// rankFn supplies §SD8's per-app frecency bonus. nil means no history is
// available — an in-memory facts store, or a first run — and every app scores
// the same, which is the authored-metadata ordering the launcher had before.
type rankFn func(id app.AppIdT) (bonus int)

// bonus resolves the bonus for one app, clamped into range. Clamping here
// rather than trusting the provider makes maxFrecencyBonus an invariant of
// the ordering rather than a convention a caller could break.
func (inst rankFn) bonus(id app.AppIdT) (b int) {
	if inst == nil {
		return
	}
	b = inst(id)
	if b < 0 {
		b = 0
	}
	if b > maxFrecencyBonus {
		b = maxFrecencyBonus
	}
	return
}

// launcherBattery compiles a launcher query. One place, so the filter and the
// surface that describes the filter cannot compile it differently.
//
// Deliberately *without* a thesaurus, unlike the help and snippet boxes.
// search.DefaultThesaurus's manifest half maps a keyword to its app's display
// name so a docs query typed in the user's vocabulary ("htop") reaches
// documentation written in the app's ("Process monitor"); here Keywords are a
// matched field already, so the same bridge on the query side would expand
// every token into a spelling of a manifest this executor was going to check
// anyway. Its ClickHouse-alias half names SQL functions, which no app is
// called. Skipping it also keeps a per-keystroke path off a whole-registry
// walk.
//
// Not memoised, also unlike those boxes: they cache because a battery is the
// cheap half of a corpus sweep worth avoiding. Here the "corpus" is a few
// dozen manifests of three short fields each, so compiling one to three short
// patterns per frame is not the expensive part of a frame that also emits a
// row per hit. If that ever stops being true, the memo belongs on Inst keyed
// by the query string — not on a second compile path that could disagree with
// this one.
func launcherBattery(query string) (b search.Battery) {
	b = search.ParseQuery(query)
	return
}

// manifestHit pairs a passing manifest with its battery score and its
// frecency bonus, so all three survive the sort together. A parallel score
// slice would not: sort permutes only what it is handed.
type manifestHit struct {
	m     app.Manifest
	score int
	bonus int
}

// scoreManifest evaluates every battery pattern against one manifest.
// ok=false when the manifest does not qualify — under RequireAll (every
// user-typed query) that means some pattern hit no field at all.
func scoreManifest(m app.Manifest, b *search.Battery) (score int, ok bool) {
	for pi := range b.Patterns {
		p := &b.Patterns[pi]
		w := 0
		switch {
		case p.Matches(m.Display):
			w = weightManifestDisplay
		case matchesAnyTopic(p, m.Topics):
			w = weightManifestTopic
		case matchesAnyKeyword(p, m.Keywords):
			w = weightManifestKeyword
		}
		if w == 0 {
			if b.RequireAll {
				score = 0
				return
			}
			continue
		}
		score += w
	}
	ok = score > 0
	return
}

// matchesAnyTopic reports whether the pattern hits any of the manifest's
// topics. Matched as text rather than compared as tokens, so `geo` finds
// `geo` and `top.*` finds `topology` — the vocabulary is closed but the query
// language over it is not.
func matchesAnyTopic(p *search.PatternT, topics []app.TopicT) (ok bool) {
	for _, t := range topics {
		if p.Matches(string(t)) {
			ok = true
			return
		}
	}
	return
}

// matchesAnyKeyword reports whether the pattern hits any of the manifest's
// retrieval keywords (ADR-0158 §SD4) — the field that makes "cpu" and "htop"
// reach a process monitor whose name says neither.
func matchesAnyKeyword(p *search.PatternT, keywords []string) (ok bool) {
	for _, k := range keywords {
		if p.Matches(k) {
			ok = true
			return
		}
	}
	return
}

// sortManifestHits orders search results: relevance descending, then the
// frecency bonus, then the same Display-then-Id comparator the browse
// sections use, so a tie breaks exactly where an unranked list would have put
// it.
//
// Relevance and bonus are compared as separate keys rather than summed, which
// is a stronger guarantee than maxFrecencyBonus alone: a hit cannot outrank a
// more relevant hit no matter what the history provider returns.
func sortManifestHits(hits []manifestHit) {
	sort.SliceStable(hits, func(i int, j int) (less bool) {
		if hits[i].score != hits[j].score {
			less = hits[i].score > hits[j].score
			return
		}
		if hits[i].bonus != hits[j].bonus {
			less = hits[i].bonus > hits[j].bonus
			return
		}
		di, dj := hits[i].m.Display, hits[j].m.Display
		if di == dj {
			less = hits[i].m.Id < hits[j].m.Id
			return
		}
		less = di < dj
		return
	})
}

// sortManifestsByDisplay reorders the slice in place by Display (then Id for
// ties) — the same comparator each browse section uses inside its bucket.
func sortManifestsByDisplay(manifests []app.Manifest) {
	sort.SliceStable(manifests, func(i, j int) (less bool) {
		di, dj := manifests[i].Display, manifests[j].Display
		if di == dj {
			less = manifests[i].Id < manifests[j].Id
			return
		}
		less = di < dj
		return
	})
}
