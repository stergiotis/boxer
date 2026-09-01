package launcher

// The launcher's facet filters, moved here from windowhost by ADR-0214 §SD2
// so the one package that renders launcher surfaces is also the one that
// decides what they show. The types and their polarity arguments are
// ADR-0158 §SD6's, unchanged.

import (
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// topicFilterT is the set of topics the launcher restricts the view to, as a
// bitmask over app.AllTopics *positions* (TopicT is a string, so position is
// what a mask can address; it also means the mask never depends on the tokens
// themselves).
//
// The zero value selects nothing and means **no restriction** — the opposite
// polarity to [kindFilterT], which stores what is hidden. That is deliberate
// rather than an inconsistency: the two axes are used with different gestures.
// There are three kinds, you normally want all of them, and the useful action
// is "hide the demos" — so a hidden-set is the natural store. There are nine
// topics, you normally want one, and the useful action is "show me only code"
// — so a selected-set is. Both conventions make the zero value inert, which is
// the property that matters for an untouched host.
type topicFilterT uint32

// topicIndex resolves a topic to its position in app.AllTopics.
func topicIndex(t app.TopicT) (idx int, ok bool) {
	for i, x := range app.AllTopics {
		if x == t {
			idx = i
			ok = true
			return
		}
	}
	return
}

// isInert reports whether the filter restricts nothing.
func (inst topicFilterT) isInert() (ok bool) {
	ok = inst == 0
	return
}

// selectedAt reports whether the topic at position idx is selected.
func (inst topicFilterT) selectedAt(idx int) (ok bool) {
	ok = inst&(1<<idx) != 0
	return
}

// toggledAt returns the filter with position idx flipped.
func (inst topicFilterT) toggledAt(idx int) (out topicFilterT) {
	out = inst ^ (1 << idx)
	return
}

// shows reports whether topic t passes. An inert filter passes everything,
// which is what makes "nothing selected" mean "no restriction".
func (inst topicFilterT) shows(t app.TopicT) (ok bool) {
	if inst.isInert() {
		ok = true
		return
	}
	idx, known := topicIndex(t)
	if !known {
		return
	}
	ok = inst.selectedAt(idx)
	return
}

// showsAny reports whether a manifest carries at least one passing topic —
// the manifest-level question, since a manifest may carry several.
func (inst topicFilterT) showsAny(topics []app.TopicT) (ok bool) {
	if inst.isInert() {
		ok = true
		return
	}
	if slices.ContainsFunc(topics, inst.shows) {
		ok = true
		return
	}
	return
}

// kindFilterT is the set of app.KindE values the launcher **hides**.
//
// Storing the hidden set rather than the shown one is deliberate on two
// counts: the zero value hides nothing, so an untouched host shows everything
// and needs no initialisation; and "every kind hidden" stays distinguishable
// from "nothing configured yet", which a shown-set mask with a zero-means-all
// convention could not express.
type kindFilterT uint8

// shows reports whether a manifest of kind k survives the filter.
func (inst kindFilterT) shows(k app.KindE) (ok bool) {
	ok = inst&(1<<k) == 0
	return
}

// toggled returns the filter with k's visibility flipped.
func (inst kindFilterT) toggled(k app.KindE) (out kindFilterT) {
	out = inst ^ (1 << k)
	return
}

// hidesAnything reports whether the filter is doing something — used to tell
// "your query matched nothing" apart from "your toggles hid it".
func (inst kindFilterT) hidesAnything() (ok bool) {
	ok = inst != 0
	return
}

// filterT is the launcher's whole filter state in one value: the query
// string, the provenance toggles, and the topic chips. Every surface resolves
// through it.
type filterT struct {
	query  string
	kinds  kindFilterT
	topics topicFilterT
}

// isInert reports whether the filter would remove nothing.
func (inst filterT) isInert() (ok bool) {
	ok = strings.TrimSpace(inst.query) == "" &&
		!inst.kinds.hidesAnything() &&
		inst.topics.isInert()
	return
}

// admits applies the two facet axes — kind and topic — to one manifest. Split
// out because the query axis is scored rather than boolean, so the two halves
// of the filter no longer read as one condition.
func (inst filterT) admits(m app.Manifest) (ok bool) {
	ok = inst.kinds.shows(m.Kind) && inst.topics.showsAny(m.Topics)
	return
}

// manifestGroup is one browse section: a topic and the manifests filed under
// it, in display order.
type manifestGroup struct {
	Topic     app.TopicT
	Manifests []app.Manifest
}

// groupByTopic sections manifests by topic in app.AllTopics order, skipping
// topics the filter drops and topics no visible manifest carries. One
// manifest appears under every topic it declares (ADR-0158 §SD3).
func groupByTopic(manifests []app.Manifest, only topicFilterT) (groups []manifestGroup) {
	if len(manifests) == 0 {
		return
	}
	byTopic := make(map[app.TopicT][]app.Manifest, len(app.AllTopics))
	for _, m := range manifests {
		for _, t := range m.Topics {
			byTopic[t] = append(byTopic[t], m)
		}
	}
	groups = make([]manifestGroup, 0, len(app.AllTopics))
	for _, t := range app.AllTopics {
		// The chips drop whole sections, not just manifests. Filtering only
		// at the manifest level would leave a two-topic app visible under its
		// *unselected* topic as well, since a manifest that passes the filter
		// is still sectioned under everything it carries.
		if !only.shows(t) {
			continue
		}
		ms := byTopic[t]
		if len(ms) == 0 {
			continue
		}
		sortManifestsByDisplay(ms)
		groups = append(groups, manifestGroup{Topic: t, Manifests: ms})
	}
	return
}

// filterManifests returns the subset of manifests passing the whole filter
// state: provenance toggles, topic chips, and the query.
//
// An inert filter returns the input slice unchanged, so callers can treat "no
// filter" and "filter matches everything" identically. Kind and topic apply
// even when the query is empty: the chips and toggles govern the sectioned
// browse view too, not just search hits.
//
// The query is a pattern battery (ADR-0164 §SD2, see search.go), scored per
// manifest. Ordering therefore depends on whether one was typed: with a query
// the result is **ranked**; without one it follows the input, and the caller
// sections and sorts it.
func filterManifests(manifests []app.Manifest, f filterT, rank rankFn) (hits []app.Manifest) {
	if f.isInert() {
		hits = manifests
		return
	}
	b := launcherBattery(f.query)
	if b.IsZero() {
		hits = make([]app.Manifest, 0, len(manifests))
		for _, m := range manifests {
			if !f.admits(m) {
				continue
			}
			hits = append(hits, m)
		}
		return
	}
	scored := make([]manifestHit, 0, len(manifests))
	for _, m := range manifests {
		if !f.admits(m) {
			continue
		}
		score, ok := scoreManifest(m, &b)
		if !ok {
			continue
		}
		scored = append(scored, manifestHit{m: m, score: score, bonus: rank.bonus(m.Id)})
	}
	sortManifestHits(scored)
	hits = make([]app.Manifest, len(scored))
	for i := range scored {
		hits[i] = scored[i].m
	}
	return
}
