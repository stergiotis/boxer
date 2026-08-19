package windowhost

// The launcher's battery executor — the third under ADR-0164 §SD2,
// alongside help/search's in-process section scan (§SD3) and
// docsearchsql's ClickHouse multiMatch lane (§SD5). What crosses between
// them is the *battery*, never the executor: each one lives with its own
// corpus, and this one's corpus is the app registry.
//
// The battery replaces the substring-plus-subsequence matcher ADR-0158
// §SD6 shipped with. ADR-0164 named that matcher as the behaviour "a
// degenerate battery (one literal) reproduces exactly", which is what
// makes this a substitution rather than a new feature: a query of one
// plain word still means what it meant.

import (
	"slices"
	"sort"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
)

// Field weights, mirroring the tier shape of help/search's §SD3 scorer:
// a pattern contributes the *strongest* tier it hit, so the tiers do not
// add up for one pattern and a word occurring in both the display name
// and a keyword still counts once, at display strength.
//
// The values are the launcher's own — this corpus has three short fields
// rather than title/heading/body — but the ordering argument is the same
// one: the name someone reads in the menu outranks the subject it was
// filed under, which outranks the retrieval synonyms nobody sees.
//
// Id stays unweighted because it stays unmatched: it is the full import
// path, so every entry would answer to "github" (ADR-0158 §SD6). Title
// stays out as a longer-form variant of Display.
const (
	weightManifestDisplay = 8
	weightManifestTopic   = 4
	weightManifestKeyword = 2
)

// launcherBattery compiles a launcher query. One place, so the filter
// and the surface that describes the filter cannot compile it
// differently.
//
// Deliberately *without* a thesaurus, unlike the help and snippet boxes.
// search.DefaultThesaurus's manifest half maps a keyword to its app's
// display name so a docs query typed in the user's vocabulary ("htop")
// reaches documentation written in the app's ("Process monitor"); here
// Keywords are a matched field already, so the same bridge on the query
// side would expand every token into a spelling of a manifest this
// executor was going to check anyway. Its ClickHouse-alias half names
// SQL functions, which no app is called. Skipping it also keeps a
// per-keystroke path off a whole-registry walk.
//
// Not memoised, also unlike those boxes: they cache because a battery is
// the cheap half of a corpus sweep worth avoiding. Here the "corpus" is
// a few dozen manifests of three short fields each, so compiling one to
// three short patterns per frame is not the expensive part of a frame
// that also emits a button per hit. If that ever stops being true, the
// memo belongs on Inst keyed by the query string — not on a second
// compile path that could disagree with this one.
func launcherBattery(query string) (b search.Battery) {
	b = search.ParseQuery(query)
	return
}

// manifestHit pairs a passing manifest with its battery score, so the
// two survive the sort together. A parallel score slice would not: sort
// permutes only what it is handed.
type manifestHit struct {
	m     app.Manifest
	score int
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
// `geo` and `top.*` finds `topology` — the vocabulary is closed but the
// query language over it is not.
func matchesAnyTopic(p *search.PatternT, topics []app.TopicT) (ok bool) {
	for _, t := range topics {
		if p.Matches(string(t)) {
			ok = true
			return
		}
	}
	return
}

// matchesAnyKeyword reports whether the pattern hits any of the
// manifest's retrieval keywords (ADR-0158 §SD4) — the field that makes
// "cpu" and "htop" reach a process monitor whose name says neither.
func matchesAnyKeyword(p *search.PatternT, keywords []string) (ok bool) {
	if slices.ContainsFunc(keywords, p.Matches) {
		ok = true
		return
	}
	return
}

// sortManifestHits orders search results: score descending, then the
// same Display-then-Id comparator the browse sections use, so a tie
// breaks exactly where an unranked list would have put it.
//
// This is authored-metadata ranking only. ADR-0158 §SD10 defers
// *frecency* — how often and how recently an app was actually opened —
// because it needs a launch record in boxer.facts; nothing here observes
// behaviour, so that deferral is untouched.
func sortManifestHits(hits []manifestHit) {
	sort.SliceStable(hits, func(i int, j int) (less bool) {
		if hits[i].score != hits[j].score {
			less = hits[i].score > hits[j].score
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
