package gloss

import (
	"mime"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// A Rule binds a gloss to every column whose spec (ADR-0186 §SD3) its
// condition holds for. Rules come from three places, in this precedence:
// in-band directives, in buffer order; a Repository's rule sets, in
// registration and declaration order; then the glosses' own affinities, in
// catalog order. Within a list, the first match wins.
//
// A rule's instance is bound once: Bind depends on the parameters alone, so
// every column a rule matches shares it.
type Rule struct {
	// Pattern is the condition's text — an RE2 source for a directive or an
	// affinity, a Predicate's String for a rule declared in code.
	Pattern string
	// MediaType is the canonical (folded, parameter-free) type; Params its
	// parameters as declared.
	MediaType string
	Params    map[string]string
	// Source says where the rule came from, for hover text and the Glosses
	// tab: a directive's line, "set <name>: <rule>", or "affinity".
	Source string
	// Set and Name are the rule set and rule a code rule was declared as;
	// empty for a directive or an affinity.
	Set  string
	Name string

	Instance InstanceI

	pred Predicate
}

// SourceSet prefixes the Source of every rule declared in a RuleSet.
const SourceSet = "set"

// Match reports whether the rule applies to a column with this spec line.
// MatchFirst and MatchAll parse the line once for a list of rules.
func (inst Rule) Match(specLine string) bool {
	spec := ParseSpec(specLine)
	return inst.pred.Matches(&spec)
}

// Matches reports whether the rule applies to a parsed spec.
func (inst Rule) Matches(spec *Spec) bool { return inst.pred.Matches(spec) }

// Token is the rule's media type with its parameters, in the compact form a
// directive is written in.
func (inst Rule) Token() string {
	return CompactMediaType(inst.MediaType, inst.Params)
}

// CompactMediaType spells a media type with its parameters as one word —
// `gloss/temperature;unit=K` — the form an alias and a directive use.
// mime.FormatMediaType would write `; unit=K`, which a whitespace-delimited
// directive token cannot carry. Parameters come out sorted by name, quoted
// per RFC 2045 when they need it (a quoted value containing "; " is not
// expressible as one word and is not a case a rule token supports).
func CompactMediaType(mediaType string, params map[string]string) string {
	return strings.ReplaceAll(mime.FormatMediaType(mediaType, params), "; ", ";")
}

// CompileRule validates a rule against the catalog and compiles its pattern.
// token is the media type in the same spelling an alias uses after the `@`
// (`gloss/temperature;unit=K`), pattern the RE2 source; every failure —
// unknown type, undeclared or refused parameter, empty or invalid pattern —
// is an error the host shows as a note, since a rule that is not a rule must
// not silently apply to nothing.
func (inst *Catalog) CompileRule(token string, pattern string, source string) (r Rule, err error) {
	mt, params, instance, err := inst.BindToken(token)
	if err != nil {
		return
	}
	if strings.TrimSpace(pattern) == "" {
		err = eb.Build().Str("mediaType", mt).Errorf("a rule needs a pattern to match against the spec line")
		return
	}
	pred := SpecMatches(pattern)
	if pred.err != nil {
		err = pred.err
		return
	}
	return Rule{Pattern: pattern, MediaType: mt, Params: params, Source: source, Instance: instance, pred: pred}, nil
}

// ParseToken splits a media-type token in the alias spelling into its
// canonical type and parameters — syntax only, no catalog: a slash, then
// mime.ParseMediaType. BindToken is the same plus the catalog checks.
func ParseToken(token string) (mediaType string, params map[string]string, err error) {
	if !strings.Contains(token, "/") {
		err = eb.Build().Str("token", token).Errorf("not a media type (no slash): %q", token)
		return
	}
	mediaType, params, err = mime.ParseMediaType(token)
	if err != nil {
		err = eb.Build().Str("token", token).Errorf("not a media type: %w", err)
	}
	return
}

// BindToken validates a media-type token in the alias spelling — a slash,
// a registered type, declared parameters — and binds it: the same checks
// ParseColumn applies past the gate, as an error rather than a Declaration
// reason, for callers that hold a token rather than a column name (a rule,
// the gloss(…) macro).
func (inst *Catalog) BindToken(token string) (mediaType string, params map[string]string, instance InstanceI, err error) {
	mt, params, err := ParseToken(token)
	if err != nil {
		return
	}
	g, ok := inst.byType[mt]
	if !ok {
		err = eb.Build().Str("mediaType", mt).Errorf("unknown media type %q — known: %s", mt, inst.knownTypes())
		return
	}
	if reason := checkParams(g, params); reason != "" {
		err = eb.Build().Str("mediaType", mt).Errorf("%s", reason)
		return
	}
	instance, err = g.Bind(params)
	if err != nil {
		return
	}
	mediaType = mt
	return
}

// SourceAffinity is the Source of every rule a gloss brings along itself.
const SourceAffinity = "affinity"

// AffinityRules compiles every registered gloss's affinities, in catalog
// order, each bound without parameters. An affinity that fails to compile is
// a programming error in the gloss and panics at first use rather than
// silently matching nothing.
func (inst *Catalog) AffinityRules() []Rule {
	if inst.affinities != nil {
		return inst.affinities
	}
	rules := make([]Rule, 0, 4)
	for _, g := range inst.order {
		for _, pat := range g.Affinities() {
			r, err := inst.CompileRule(g.MediaType(), pat, SourceAffinity)
			if err != nil {
				panic(err)
			}
			rules = append(rules, r)
		}
	}
	inst.affinities = rules
	return rules
}

// MatchFirst returns the first rule in rules matching the spec line — the
// caller lists directive rules before a repository's, so precedence is list
// order.
func MatchFirst(rules []Rule, specLine string) (r Rule, ok bool) {
	spec := ParseSpec(specLine)
	for i := range rules {
		if rules[i].Matches(&spec) {
			return rules[i], true
		}
	}
	return Rule{}, false
}

// MatchAll returns every rule in rules matching the spec line, in list
// order: MatchFirst's answer first, then the rules it shadows (ADR-0186
// §SD3). The host binds the first and lists the rest in the Glosses tab, so
// a rule that never fires can be seen not to. Nil when nothing matches.
func MatchAll(rules []Rule, specLine string) (matched []Rule) {
	spec := ParseSpec(specLine)
	for i := range rules {
		if rules[i].Matches(&spec) {
			matched = append(matched, rules[i])
		}
	}
	return matched
}
