package search

// The zero-LLM thesaurus rung of the ADR-0164 search ladder (§SD7): a
// static table of alternate spellings folded into a battery at compile
// time. A token that names a known alias gains its canonical form as
// an alternation branch INSIDE its own pattern — `lcase` compiles to
// `(?i)(?:lcase|lower)` — so RequireAll semantics survive: the token
// still counts as one battery entry, satisfiable by either spelling.
//
// Two sources compose:
//
//   - ClickHouse function aliases, generated from the pinned engine's
//     own system.functions (chaliases.gen.go). The ADR's original
//     deferral assumed these need a live server round-trip; they do
//     not — the alias set is a property of the engine VERSION, which
//     this repository pins, so the table regenerates with the pin and
//     works offline, on M1 hosts, and inside the pure docsearch
//     expansion pass alike.
//   - Launcher keywords (ADR-0158 §SD4): a manifest's Keywords carry
//     the synonyms its display name cannot — "htop" reaches the
//     process monitor — and the same bridge helps a docs search reach
//     the app's documentation.

import (
	"regexp"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

//go:generate go run gen_chaliases.go

// Thesaurus maps a lower-cased token to alternate spellings. Alternates
// are literal words or phrases (never regexes); a multi-word alternate
// matches across any whitespace run. Nil is the empty thesaurus.
type Thesaurus map[string][]string

// alternates looks a raw token up, case-insensitively. Tokens carrying
// regex metacharacters simply miss — the keys are names, not patterns.
func (inst Thesaurus) alternates(tok string) (alts []string) {
	if inst == nil {
		return
	}
	alts = inst[strings.ToLower(tok)]
	return
}

// add appends alt under key, deduplicating case-insensitively and
// dropping self-references (an alternate equal to its key would inflate
// every pattern for nothing — the battery is case-insensitive already).
func (inst Thesaurus) add(key string, alt string) {
	k := strings.ToLower(key)
	if strings.EqualFold(key, alt) {
		return
	}
	for _, have := range inst[k] {
		if strings.EqualFold(have, alt) {
			return
		}
	}
	inst[k] = append(inst[k], alt)
}

// MergeThesauri folds the given tables into one, preserving order of
// first appearance per key.
func MergeThesauri(parts ...Thesaurus) (out Thesaurus) {
	out = make(Thesaurus, 64)
	for _, p := range parts {
		for k, alts := range p {
			for _, a := range alts {
				out.add(k, a)
			}
		}
	}
	return
}

// ThesaurusCHFunctions returns the ClickHouse function-alias table
// (alias → canonical name, one direction: documentation writes the
// canonical spelling, searchers type either). Sourced from the
// generated chaliases.gen.go; regenerate with the engine pin.
func ThesaurusCHFunctions() (th Thesaurus) {
	th = make(Thesaurus, len(chFunctionAliases))
	for alias, canonical := range chFunctionAliases {
		th.add(alias, canonical)
	}
	return
}

// ThesaurusFromManifests builds the keyword bridge from the app
// registry: each manifest keyword gains the manifest's display name as
// an alternate, so a query in the user's vocabulary ("htop") also
// matches documentation written in the app's own ("Process monitor").
func ThesaurusFromManifests() (th Thesaurus) {
	th = thesaurusFromManifests(app.AllManifests())
	return
}

// thesaurusFromManifests is the testable core of
// [ThesaurusFromManifests].
func thesaurusFromManifests(ms []app.Manifest) (th Thesaurus) {
	th = make(Thesaurus, 32)
	for i := range ms {
		display := strings.TrimSpace(ms[i].Display)
		if display == "" {
			continue
		}
		for _, kw := range ms[i].Keywords {
			kw = strings.TrimSpace(kw)
			if kw == "" || strings.ContainsAny(kw, " \t") {
				// Multi-word keywords exist for the launcher's
				// substring match; as battery tokens they can never be
				// typed (space splits tokens), so they have no key to
				// live under.
				continue
			}
			th.add(kw, display)
		}
	}
	return
}

// DefaultThesaurus composes the two standard sources. The manifests
// walk reflects the registry at call time; callers on a per-keystroke
// path cache the result (the registry is effectively frozen after
// init).
func DefaultThesaurus() (th Thesaurus) {
	th = MergeThesauri(ThesaurusCHFunctions(), ThesaurusFromManifests())
	return
}

// altBranch renders one alternate as a pattern branch: a quoted
// literal, any whitespace in the phrase matching any whitespace run.
func altBranch(alt string) (branch string) {
	words := strings.Fields(alt)
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = regexp.QuoteMeta(w)
	}
	branch = strings.Join(quoted, `\s+`)
	return
}
