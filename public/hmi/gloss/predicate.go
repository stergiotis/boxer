package gloss

import (
	"regexp"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// Predicate is one condition on a column's Spec, carrying the text it
// reads as — `section=sensor`, `name~^temp`, `sem=secret` — so a rule
// declared in code shows the same way in the Glosses tab. Predicates are
// values: build them with the constructors below and combine them with
// All, Any and Not. The zero Predicate holds nothing and matches nothing.
type Predicate struct {
	text string
	test func(*Spec) bool
	err  error // a pattern that does not compile; surfaces at Register
}

// String is the predicate's text form.
func (inst Predicate) String() string { return inst.text }

// Matches evaluates the predicate on a parsed spec.
func (inst Predicate) Matches(spec *Spec) bool {
	return inst.test != nil && inst.err == nil && inst.test(spec)
}

// Err reports a predicate that cannot run — a pattern that did not compile,
// in it or in a predicate it combines.
func (inst Predicate) Err() error { return inst.err }

func exact(prefix string, want string, get func(*Spec) string) Predicate {
	return Predicate{text: prefix + "=" + want, test: func(s *Spec) bool { return get(s) == want }}
}

func matches(prefix string, pattern string, get func(*Spec) string) Predicate {
	p := Predicate{text: prefix + "~" + pattern}
	re, err := regexp.Compile(pattern)
	if err != nil {
		p.err = eb.Build().Str("pattern", pattern).Str("prefix", prefix).Errorf("pattern does not compile: %w", err)
		return p
	}
	p.test = func(s *Spec) bool { return re.MatchString(get(s)) }
	return p
}

func has(prefix string, want string, get func(*Spec) []string) Predicate {
	return Predicate{text: prefix + "=" + want, test: func(s *Spec) bool {
		return slices.Contains(get(s), want)
	}}
}

// Name matches the column's name exactly — the leeway column name for a
// leeway column, the result column name for any other. Any string-kinded
// type is accepted (a naming type, a constant).
func Name[T ~string](name T) Predicate {
	return exact("name", string(name), func(s *Spec) string { return s.Name })
}

// NameMatches matches the column's name against an RE2 pattern, unanchored
// and case-sensitive.
func NameMatches(pattern string) Predicate {
	return matches("name", pattern, func(s *Spec) string { return s.Name })
}

// Section matches a tagged column's section name exactly.
func Section[T ~string](section T) Predicate {
	return exact("section", string(section), func(s *Spec) string { return s.Section })
}

// Role matches a tagged column's role exactly — common.ColumnRoleValue,
// common.ColumnRoleLength, … (any string-kinded type).
func Role[T ~string](role T) Predicate {
	return exact("role", string(role), func(s *Spec) string { return s.Role })
}

// Item matches a backbone column's item type — ddl.IdPrefix,
// ddl.TimestampPrefix, ….
func Item[T ~string](item T) Predicate {
	return exact("item", string(item), func(s *Spec) string { return s.Item })
}

// CT matches the leeway canonical type exactly, as spelled in the spec
// line (`u64`, `f64`, `s`).
func CT[T ~string](ct T) Predicate {
	return exact("ct", string(ct), func(s *Spec) string { return s.CT })
}

// Arrow matches the host's arrow: token by prefix — `Arrow("list<")` for
// any list, `Arrow("float")` for float32 and float64.
func Arrow(prefix string) Predicate {
	return Predicate{text: "arrow~^" + prefix, test: func(s *Spec) bool { return strings.HasPrefix(s.Arrow, prefix) }}
}

// Enc matches an encoding aspect on the column, by the vocabulary's own
// enum — a misspelt aspect does not compile.
func Enc(a encodingaspects.AspectE) Predicate {
	return has("enc", a.String(), func(s *Spec) []string { return s.Enc })
}

// Sem matches a value-semantics aspect on the column.
func Sem(a valueaspects.AspectE) Predicate {
	return has("sem", a.String(), func(s *Spec) []string { return s.Sem })
}

// Use matches a use aspect on the column's section.
func Use(a useaspects.AspectE) Predicate {
	return has("use", a.String(), func(s *Spec) []string { return s.Use })
}

// SpecMatches matches the whole spec line against an RE2 pattern — what a
// `-- play: gloss` directive does, and the escape hatch for a condition
// the typed predicates cannot say.
func SpecMatches(pattern string) Predicate {
	return matches("spec", pattern, func(s *Spec) string { return s.Line })
}

// All holds when every predicate holds; with none it holds nothing. It
// reads `a ∧ b`; ∧ binds tighter than ∨, so an Any inside it keeps its
// parentheses and an All inside an Any needs none.
func All(preds ...Predicate) Predicate {
	return combine(" ∧ ", "", preds, func(s *Spec) bool {
		for _, p := range preds {
			if !p.Matches(s) {
				return false
			}
		}
		return len(preds) > 0
	})
}

// Any holds when at least one predicate holds. It reads `(a ∨ b)`.
func Any(preds ...Predicate) Predicate {
	return combine(" ∨ ", "()", preds, func(s *Spec) bool {
		for _, p := range preds {
			if p.Matches(s) {
				return true
			}
		}
		return false
	})
}

// Not inverts a predicate. It reads `¬(a)`.
func Not(p Predicate) Predicate {
	return Predicate{text: "¬(" + p.text + ")", err: p.err, test: func(s *Spec) bool { return !p.Matches(s) }}
}

// combine joins predicates' texts with sep, wrapped in the two runes of
// wrap when several (none for one), and carries the first error along.
func combine(sep string, wrap string, preds []Predicate, test func(*Spec) bool) Predicate {
	out := Predicate{test: test}
	texts := make([]string, len(preds))
	for i, p := range preds {
		texts[i] = p.text
		if p.err != nil && out.err == nil {
			out.err = p.err
		}
	}
	out.text = strings.Join(texts, sep)
	if len(preds) > 1 && wrap != "" {
		out.text = wrap[:1] + out.text + wrap[1:]
	}
	return out
}
