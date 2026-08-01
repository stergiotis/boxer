package play

// The Docs pane's data half: turning the name under the caret into
// documentation, from whatever DocsSourceI is installed.
//
// This file is the source-agnostic engine — debounce, cache, and the
// caret-to-candidate-name walk — and knows nothing about ClickHouse. The
// default source (ClickHouse's own system.documentation) lives in
// play_docs_clickhouse.go; see DocsSourceI (play_docs_source.go) for the
// seam a re-user installs a different one through, and
// doc/howto/play-pluggable-docs.md for how.
//
// What this file adds on top of a source's raw Lookup is a debounce (the
// caret moves per keystroke; a source should not be asked that often) and a
// small cache of parsed documents, because a reader flipping between two
// names would otherwise re-look-up both every time.

import (
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

const (
	// docsQuiescence is how long the caret's entity must hold still before a
	// lookup ships. The caret moves per keystroke and per arrow key; a
	// source should see neither. Short enough that a deliberate pause on a
	// name feels immediate.
	docsQuiescence = 250 * time.Millisecond
	// docsCacheMax is how many looked-up names are kept. Documentation
	// bodies can run large (ClickHouse's MergeTree entry is ~80 KB), so this
	// is bounded by the largest plausible working set rather than by memory
	// pressure.
	docsCacheMax = 32
)

// docEntry is one DocsSourceI result, with its rendered form attached once
// something asks for it.
type docEntry struct {
	DocsEntry
	// source is what produced this entry, kept for AbsolutiseLinks — the
	// rewrite happens lazily on first render, matching how markdown.Parse
	// itself is deferred (a name with several kinds fetches every one, and
	// an unseen kind's large body should cost nothing until shown).
	source DocsSourceI

	// doc is Body parsed, built on first render rather than on fetch: a name
	// with several kinds fetches every one of them, and a large body alone
	// is more Markdown than a reader looking at a different kind ever sees.
	doc *markdown.Doc
}

// rendered returns the parsed document, parsing on first use.
func (inst *docEntry) rendered() *markdown.Doc {
	if inst.doc == nil {
		md := inst.Body
		if inst.source != nil {
			md = inst.source.AbsolutiseLinks(md)
		}
		inst.doc = markdown.Parse([]byte(md))
	}
	return inst.doc
}

// docsResult is what one name's lookup produced.
type docsResult struct {
	entries []docEntry
	// err is the lookup's failure, if any. A source too old, or too
	// differently shaped, to answer lands here — an honest "this source
	// cannot answer", not an empty page.
	err error
}

// docsDriver owns the debounce, the parsed-document cache, and the installed
// DocsSourceI. It has no opinion on how a source finds its answers.
//
// Render-thread-only. A nil source (tests, a client-less session, or before
// SetDocsSource installs one) makes lookup a no-op and the pane says so, the
// same shape DiagnosticsDriver uses.
type docsDriver struct {
	source DocsSourceI

	// cache maps a lowercased name to its result; order is the LRU, most
	// recently used last.
	cache map[string]*docsResult
	order []string

	// pending is the name a lookup is in flight or armed for, "" when idle.
	pending string
	// armed/armedAt implement the debounce: armed is the name last seen under
	// the caret, armedAt when it first appeared there.
	armed   string
	armedAt time.Time

	// now is an injection point for tests; nil means time.Now.
	now func() time.Time
}

func newDocsDriver(source DocsSourceI) (inst *docsDriver) {
	inst = &docsDriver{source: source, cache: make(map[string]*docsResult, docsCacheMax)}
	return
}

// close tears down the installed source (PlayApp.Close). Idempotent, nil-safe.
func (inst *docsDriver) close() {
	if inst != nil && inst.source != nil {
		inst.source.Close()
	}
}

// cached answers from the cache alone, touching nothing else. It is the
// free half of the lookup: a caller walking several candidates uses it to
// skip the ones it already knows about without disturbing the debounce.
func (inst *docsDriver) cached(name string) (res *docsResult) {
	if inst == nil || name == "" {
		return
	}
	key := strings.ToLower(name)
	hit, ok := inst.cache[key]
	if !ok {
		return
	}
	inst.touch(key)
	return hit
}

// lookup returns what is known about `name`, driving the fetch as a side
// effect: arming the debounce, asking the source once the name holds still,
// and draining a finished lookup into the cache.
//
// CALL IT AT MOST ONCE PER FRAME. The debounce is a single slot, so two calls
// naming different entities restart each other's timer and neither ever
// reaches quiescence — a caller with several candidates screens them with
// [docsDriver.cached] and lookups only the one it decides to pursue.
//
// A cached name answers immediately and arms nothing. loading is true while
// this name's own query is in flight — never while some other name's is, so a
// pane showing a cached page does not flicker into a spinner because the caret
// passed over something else.
func (inst *docsDriver) lookup(name string) (res *docsResult, loading bool) {
	if inst == nil || name == "" {
		return
	}
	if hit := inst.cached(name); hit != nil {
		return hit, false
	}
	if inst.source == nil {
		return
	}
	key := strings.ToLower(name)
	if inst.now == nil {
		inst.now = time.Now
	}

	// Debounce: a name must hold still before it costs a round trip.
	if inst.armed != key {
		inst.armed = key
		inst.armedAt = inst.now()
		return nil, false
	}
	if inst.now().Sub(inst.armedAt) < docsQuiescence {
		return nil, false
	}

	inst.pending = key
	entries, ready, err := inst.source.Lookup(name)
	if !ready {
		return nil, true
	}
	stored := &docsResult{err: err}
	if err == nil {
		stored.entries = make([]docEntry, len(entries))
		for i, e := range entries {
			stored.entries[i] = docEntry{DocsEntry: e, source: inst.source}
		}
	}
	inst.store(key, stored)
	inst.pending = ""
	return stored, false
}

// store puts a result in the cache, evicting the least recently used.
func (inst *docsDriver) store(key string, res *docsResult) {
	if _, exists := inst.cache[key]; !exists && len(inst.order) >= docsCacheMax {
		oldest := inst.order[0]
		inst.order = inst.order[1:]
		delete(inst.cache, oldest)
	}
	inst.cache[key] = res
	inst.touch(key)
}

// touch moves key to the most-recently-used end.
func (inst *docsDriver) touch(key string) {
	for i, k := range inst.order {
		if k == key {
			inst.order = append(inst.order[:i], inst.order[i+1:]...)
			break
		}
	}
	inst.order = append(inst.order, key)
}

// SetDocsSource overrides the Docs pane's lookup, replacing whatever source
// is currently installed (closing it first). Passing nil restores ClickHouse's
// own system.documentation via NewClickHouseDocsSource — what an unconfigured
// PlayApp with a live client already uses. Takes effect on the next frame;
// there is nothing else to unregister. See doc/howto/play-pluggable-docs.md.
func (inst *PlayApp) SetDocsSource(src DocsSourceI) {
	if inst.docs != nil {
		inst.docs.close()
	}
	if src == nil && inst.client != nil {
		src = NewClickHouseDocsSource(inst.client)
	}
	inst.docs = newDocsDriver(src)
}

// docsCandidates is the ranked list of names to look up for one caret
// position: what the caret is on, then outwards through the calls enclosing
// it.
//
// The order encodes what a reader means by "what is this?". A caret on a name
// asks about that name. Failing that — a caret on a column, a comma, or a
// literal inside a call — the next most likely question is about the call it
// is an argument of, which is also what signature help would answer.
// Duplicates are dropped so `f(f(|))` does not ask twice.
func docsCandidates(e highlight.CaretEntity, ok bool) (out []string) {
	if !ok {
		return
	}
	seen := make(map[string]struct{}, 1+len(e.Enclosing))
	add := func(n string) {
		if n == "" {
			return
		}
		k := strings.ToLower(n)
		if _, dup := seen[k]; dup {
			return
		}
		seen[k] = struct{}{}
		out = append(out, n)
	}
	add(e.Name)
	for _, n := range e.Enclosing {
		add(n)
	}
	return
}
