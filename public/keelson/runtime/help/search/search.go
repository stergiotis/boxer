// Package search answers pattern-battery queries over a help library's
// section-grained corpus (ADR-0164 §SD2/§SD3).
//
// The query model is the battery: an ordered set of case-insensitive
// RE2 patterns plus a mode (every pattern must hit vs. hit count
// ranks). One battery feeds every executor — the in-process scan this
// package implements today, and the ClickHouse multiMatch* lane
// (ADR-0164 §SD5) later — so search semantics cannot drift per
// surface. RE2 rejects backreferences and lookaround at compile time,
// which keeps every battery that compiles here inside the subset the
// hyperscan executor accepts too.
//
// There is deliberately no inverted index and no persistence: the
// embedded corpus is on the order of 100 KB and an RE2 sweep of it is
// well under a millisecond, so the index is rebuilt never and scanned
// on every query *change* (callers cache hits keyed by the query
// string; see helphost). If a corpus outgrows the scan, it belongs on
// the facts plane, not in a cleverer in-process structure.
package search

import (
	"regexp"
	"strings"
)

// PatternT is one compiled battery entry. Raw is the token as the user
// typed it (or the generator emitted it); Literal reports that Raw
// failed to compile as a regexp and was degraded to a quote-meta
// literal match instead — surfaced so a UI can hint "matching
// literally" rather than silently changing semantics mid-keystroke
// (a half-typed `quantile(` must keep matching as text).
type PatternT struct {
	Raw     string
	Literal bool
	re      *regexp.Regexp
}

// Matches reports whether the pattern occurs in text.
func (inst *PatternT) Matches(text string) (ok bool) {
	ok = inst.re.MatchString(text)
	return
}

// EffectiveSource returns the pattern text the compiled matcher
// actually runs — `(?i)` plus Raw, quote-meta'd when the token
// degraded to a literal. This is what the facts-plane executor
// (ADR-0164 §SD5) splices into multiMatch*: RE2 rejected
// backreferences and lookaround at Compile, so the returned text is
// inside the subset hyperscan accepts, and both executors see the
// same pattern byte for byte.
func (inst *PatternT) EffectiveSource() (src string) {
	src = inst.re.String()
	return
}

// find returns the first match's byte bounds in text, ok=false when
// absent. Used for context-line extraction.
func (inst *PatternT) find(text string) (start int, end int, ok bool) {
	loc := inst.re.FindStringIndex(text)
	if loc == nil {
		return
	}
	start, end, ok = loc[0], loc[1], true
	return
}

// Battery is the compiled query (ADR-0164 §SD2). RequireAll is the
// user-typed mode: a section must be hit by every pattern to qualify.
// Generated batteries (text2regex, ADR-0164 §SD6) run with RequireAll
// false, where the number of distinct patterns hit ranks the section.
type Battery struct {
	Patterns   []PatternT
	RequireAll bool
}

// IsZero reports an empty battery — the "no query" state that matches
// nothing (not everything: an empty search box shows the browse view,
// never an all-corpus result dump).
func (inst *Battery) IsZero() (zero bool) {
	zero = len(inst.Patterns) == 0
	return
}

// Compile builds a battery from raw pattern tokens. Every token is
// compiled case-insensitively; a token that is not a valid regexp
// degrades to a quote-meta literal (PatternT.Literal) instead of
// erroring. Empty tokens are dropped. The degenerate battery of one
// literal token reproduces launcher-style substring search exactly
// (ADR-0158 §SD6 precedent).
func Compile(tokens []string, requireAll bool) (b Battery) {
	b.RequireAll = requireAll
	if len(tokens) == 0 {
		return
	}
	b.Patterns = make([]PatternT, 0, len(tokens))
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		p := PatternT{Raw: tok}
		re, err := regexp.Compile("(?i)" + tok)
		if err != nil {
			re = regexp.MustCompile("(?i)" + regexp.QuoteMeta(tok))
			p.Literal = true
		}
		p.re = re
		b.Patterns = append(b.Patterns, p)
	}
	return
}

// ParseQuery compiles a user-typed query string: whitespace-separated
// tokens, each its own pattern, all required (RequireAll). Spaces
// therefore mean AND, not "literal space" — the predictable search-box
// reading; a literal multi-word phrase is reachable as `foo\s+bar`.
func ParseQuery(q string) (b Battery) {
	b = Compile(strings.Fields(q), true)
	return
}
