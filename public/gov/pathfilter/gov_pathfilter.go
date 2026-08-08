// Package pathfilter matches repository-relative paths against exclusion
// patterns, for the governance checks a consuming repository configures
// (ADR-0179).
//
// It exists because the exclusion set was the last piece of lint policy still
// living as copied shell. A consumer repository carried it as a `grep -Ev`
// regex duplicated between its lint script and its editor hook — two copies of
// one policy, in a language with no way to share them, in the layer this ADR
// exists to stop copying.
//
// The pattern syntax is deliberately smaller than a regex, because the thing
// being expressed is small:
//
//	CLAUDE.md      a bare name matches that basename anywhere
//	*.out.md       a bare glob matches the basename anywhere
//	attic/         a trailing slash matches that directory at any depth
//	doc/gen/       …including a nested one, e.g. public/x/doc/gen/y.md
//	doc/adr/*.md   a pattern with a separator matches the whole relative path
//
// Anchoring differs from a regex on purpose: "attic/" excludes an attic
// directory wherever it sits, which is what a repository means by it, and what
// the shell versions were approximating with alternation.
package pathfilter

import (
	"path"
	"strings"
)

// Matcher tests paths against a set of exclusion patterns.
//
// The zero value matches nothing, which is the right default: a check with no
// configured exclusions examines everything.
type Matcher struct {
	patterns []string
}

func NewMatcher(patterns []string) (inst *Matcher) {
	inst = &Matcher{patterns: make([]string, 0, len(patterns))}
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		inst.patterns = append(inst.patterns, path.Clean(p)+suffixOf(p))
	}
	return
}

// suffixOf preserves a trailing slash through path.Clean, which strips it.
func suffixOf(p string) (s string) {
	if strings.HasSuffix(p, "/") {
		return "/"
	}
	return ""
}

// IsEmpty reports whether the matcher carries no patterns, so callers can skip
// the work of computing a relative path.
func (inst *Matcher) IsEmpty() (empty bool) {
	return inst == nil || len(inst.patterns) == 0
}

// Match reports whether rel — a slash-separated path relative to the repository
// root — is excluded.
func (inst *Matcher) Match(rel string) (excluded bool) {
	if inst.IsEmpty() {
		return false
	}
	rel = strings.TrimPrefix(path.Clean(strings.ReplaceAll(rel, "\\", "/")), "./")
	base := path.Base(rel)

	for _, p := range inst.patterns {
		switch {
		case strings.HasSuffix(p, "/"):
			dir := strings.TrimSuffix(p, "/")
			// At the root, or nested at any depth.
			if strings.HasPrefix(rel, dir+"/") || strings.Contains(rel, "/"+dir+"/") {
				return true
			}
		case strings.Contains(p, "/"):
			if ok, _ := path.Match(p, rel); ok {
				return true
			}
		default:
			if ok, _ := path.Match(p, base); ok {
				return true
			}
		}
	}
	return false
}
