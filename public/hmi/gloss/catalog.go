package gloss

import (
	"fmt"
	"iter"
	"mime"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Sep introduces a column's declaration: `<label>@<media type>`. It is
// ADR-0122 §SD2's separator with its reasoning intact — measured against
// clickhouse-local, `@` parses backtick-quoted and is a syntax error unquoted,
// so a forgotten backtick fails at once, where `#` would open a line comment.
const Sep = "@"

// Catalog is the ordered set of registered glosses. Registration order is
// affinity order (ADR-0186 §SD3: first match wins) and the order the reject
// message lists the known types in.
type Catalog struct {
	byType map[string]GlossI
	order  []GlossI
	// affinities caches AffinityRules; Register drops it.
	affinities []Rule
}

func NewCatalog() *Catalog {
	return &Catalog{byType: make(map[string]GlossI, 16), order: make([]GlossI, 0, 16)}
}

// Register adds a gloss; a media type already present is an error, never a
// silent replacement — two hosts registering the same name would otherwise
// depend on wiring order.
func (inst *Catalog) Register(g GlossI) (err error) {
	mt := g.MediaType()
	if _, dup := inst.byType[mt]; dup {
		return eb.Build().Str("mediaType", mt).Errorf("gloss already registered")
	}
	inst.byType[mt] = g
	inst.order = append(inst.order, g)
	inst.affinities = nil
	return nil
}

// MustRegister is Register for wiring code, where a duplicate is a
// programming error.
func (inst *Catalog) MustRegister(g GlossI) {
	if err := inst.Register(g); err != nil {
		panic(err)
	}
}

func (inst *Catalog) Lookup(mediaType string) (g GlossI, ok bool) {
	g, ok = inst.byType[mediaType]
	return
}

// All iterates the glosses in registration order.
func (inst *Catalog) All() iter.Seq[GlossI] {
	return func(yield func(GlossI) bool) {
		for _, g := range inst.order {
			if !yield(g) {
				return
			}
		}
	}
}

func (inst *Catalog) Len() int { return len(inst.order) }

// knownTypes lists the catalog for a reject message, in registration order.
func (inst *Catalog) knownTypes() string {
	names := make([]string, 0, len(inst.order))
	for _, g := range inst.order {
		names = append(names, g.MediaType())
	}
	return strings.Join(names, ", ")
}

// Declaration is a parsed `<label>@<media type>` column name.
//
// A Reason means the column declared *something* — the token after `@`
// carried a slash — that the catalog cannot honour: an unknown type, a
// malformed one, an undeclared or refused parameter. The host then shows the
// cell as it would have looked undeclared, with the reason beside it. That is
// the point of the slash gate (ADR-0123 §SD2): `notes@text/markdwn` must not
// quietly render as plain text, but `user@example.com` must not be nagged.
type Declaration struct {
	Label     string
	MediaType string // canonical (folded, parameter-free); the raw token when it did not parse
	Params    map[string]string
	Instance  InstanceI // nil iff Reason != ""
	Reason    string
}

// ParseColumn resolves a column name against the ADR-0123 §SD2 contract,
// unchanged by ADR-0186 except that "known type" means "in the catalog":
//
//	no `@`                          → not declared
//	`@` but no `/` after it         → not declared (dot_done@success, user@example.com)
//	`/`, parses, registered, params ok → declared, Instance set
//	`/`, parses, unknown type       → declared, Reason
//	`/`, fails to parse             → declared, Reason
//	registered, undeclared/refused param → declared, Reason
//
// The gate is the slash, NOT "mime.ParseMediaType succeeds": that function
// does not require one — ParseMediaType("success") returns ("success", nil).
func (inst *Catalog) ParseColumn(name string) (d Declaration, declared bool) {
	label, token, found := strings.Cut(name, Sep)
	if !found || !strings.Contains(token, "/") {
		return d, false
	}
	if label == "" {
		// `@text/markdown` — a declaration with nothing to call it. Show the
		// raw name rather than an empty caption.
		label = name
	}
	d.Label = label
	mt, params, err := mime.ParseMediaType(token)
	if err != nil {
		d.MediaType = token
		d.Reason = fmt.Sprintf("not a media type: %s", err)
		return d, true
	}
	d.MediaType = mt
	d.Params = params
	g, ok := inst.byType[mt]
	if !ok {
		d.Reason = fmt.Sprintf("unknown media type %q — known: %s", mt, inst.knownTypes())
		return d, true
	}
	if reason := checkParams(g, params); reason != "" {
		d.Reason = reason
		return d, true
	}
	instance, err := g.Bind(params)
	if err != nil {
		d.Reason = err.Error()
		return d, true
	}
	d.Instance = instance
	return d, true
}

// checkParams enforces the declared parameter names and closed value sets
// before Bind sees them, so a gloss's Bind can assume a valid map. An
// undeclared parameter is as loud as an unknown type: `;unti=K` must not
// render as °C.
func checkParams(g GlossI, params map[string]string) (reason string) {
	specs := g.Params()
	names := make([]string, 0, len(params))
	for k := range params {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		i := slices.IndexFunc(specs, func(s ParamSpec) bool { return s.Name == k })
		if i < 0 {
			if len(specs) == 0 {
				return fmt.Sprintf("%s takes no parameters (got %s=%q)", g.MediaType(), k, params[k])
			}
			return fmt.Sprintf("unknown parameter %q for %s — declared: %s", k, g.MediaType(), paramNames(specs))
		}
		if specs[i].Values != nil && !slices.Contains(specs[i].Values, params[k]) {
			return fmt.Sprintf("%s=%q is not allowed for %s — one of: %s", k, params[k], g.MediaType(), strings.Join(specs[i].Values, ", "))
		}
	}
	return ""
}

func paramNames(specs []ParamSpec) string {
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

// Default is the built-in catalog: the ADR-0123 content family first, in its
// original vocabulary order, then the ADR-0186 presentation glosses.
func Default() *Catalog {
	c := NewCatalog()
	for _, g := range contentFamily() {
		c.MustRegister(g)
	}
	for _, g := range presentationFamily() {
		c.MustRegister(g)
	}
	// Consumer extensions last, so a built-in affinity rule still wins a
	// contested column — see RegisterDefault.
	for _, g := range RegisteredDefaults() {
		c.MustRegister(g)
	}
	return c
}
