package gloss

import (
	"strings"
	"sync"
)

var (
	defaultRepositoryOnce sync.Once
	defaultRepository     *Repository
)

// DefaultRepository is the process-wide rule repository over the default
// catalog: what a host hands its windows when it was given none. A
// deployment registers its rule sets on it before the first window mounts,
// from an init of its own package, or builds a repository of its own and
// hands that to the host instead.
func DefaultRepository() *Repository {
	defaultRepositoryOnce.Do(func() {
		defaultRepository = NewRepository(nil)
	})
	return defaultRepository
}

// SourceAlias names a binding that came from the column's own name, a
// `label@media/type` declaration, rather than from a rule.
const SourceAlias = "alias"

// Resolution is how one column, or one value standing for a column, is shown:
// the gloss a declaration or the first matching rule binds, where that came
// from, and whether the gloss takes the value's kind. The zero value is a
// plain column.
type Resolution struct {
	// Label is the header label a declaration supplies (`t` of `t@gloss/…`);
	// empty for an undeclared column, which keeps its own name.
	Label string
	// MediaType is the resolved gloss, empty when the column is plain;
	// Params its bound parameters.
	MediaType string
	Params    map[string]string
	// Instance renders the values; nil when plain or refused.
	Instance InstanceI
	// Source says where the binding came from: SourceAlias, or the rule's
	// provenance and pattern (`directive line 3: name:.*temp`,
	// `affinity: \bsem:secret\b`).
	Source string
	// Shadowed are the rules that matched the spec line but did not bind:
	// every later match behind the winning rule, and every match behind a
	// declaration, which is never offered to the rules.
	Shadowed []Rule
	// SpecLine is the written-out form the rules matched against.
	SpecLine string
	// Reason is why a declaration could not be honoured: an unknown type, a
	// bad parameter. The values then render plain.
	Reason string
	// RowOK and ElemOK are the gloss's verdicts on the value kind and, for a
	// list column, on its element kind; a refusal keeps the values plain and
	// its reason is shown on hover.
	RowOK      bool
	RowReason  string
	ElemOK     bool
	ElemReason string
}

// Resolve binds a gloss for the column name, whose spec line is specLine and
// whose values are of kind (elemKind for a list column's items; pass kind
// again for a scalar). A declaration in the name wins and is never offered
// to the rules; otherwise the first of rules matching the spec line binds,
// in list order. A nil catalog is Default().
func Resolve(cat *Catalog, name string, specLine string, rules []Rule, kind ValueKindE, elemKind ValueKindE) (r Resolution) {
	if cat == nil {
		cat = Default()
	}
	r.SpecLine = specLine
	matches := MatchAll(rules, specLine)
	if d, declared := cat.ParseColumn(name); declared {
		r.Label = d.Label
		r.MediaType = d.MediaType
		r.Params = d.Params
		r.Source = SourceAlias
		r.Shadowed = matches
		if d.Reason != "" {
			r.Reason = d.Reason
			return
		}
		r.Instance = d.Instance
	} else if len(matches) > 0 {
		m := matches[0]
		r.MediaType = m.MediaType
		r.Params = m.Params
		r.Source = m.Source + ": " + m.Pattern
		r.Instance = m.Instance
		r.Shadowed = matches[1:]
	} else {
		return
	}
	r.RowOK, r.RowReason = r.Instance.Accepts(kind)
	r.ElemOK, r.ElemReason = r.Instance.Accepts(elemKind)
	return
}

// Glossed reports whether a value of the column renders through its gloss.
func (inst *Resolution) Glossed() bool { return inst.Instance != nil && inst.RowOK }

// GlossedElem reports the same for a list column's items.
func (inst *Resolution) GlossedElem() bool { return inst.Instance != nil && inst.ElemOK }

// Face is the face of one value: the gloss's when the column is glossed
// and the value is not null, the plain text otherwise. glossed says which.
func (inst *Resolution) Face(cell CellI) (face Inline, glossed bool) {
	if cell.IsNull() {
		return Inline{}, false
	}
	if !inst.Glossed() {
		return Inline{Text: cell.Text()}, false
	}
	return inst.Instance.Inline(cell), true
}

// Link is the URL a gloss/url value opens, the value itself trimmed; empty
// for any other gloss, a column that is not glossed, or a value that is not
// text.
func (inst *Resolution) Link(cell CellI) string {
	if inst.MediaType != MediaTypeURL || !inst.Glossed() {
		return ""
	}
	raw, ok := cell.Raw()
	if !ok {
		return ""
	}
	return strings.TrimSpace(raw)
}

// Hover is the account of a column a header's hover gives: what glosses it
// and from where, or why a declaration was refused, then the spec line the
// rules saw. Empty for a plain column with no spec line to show.
func (inst *Resolution) Hover() string {
	var head string
	token := CompactMediaType(inst.MediaType, inst.Params)
	switch {
	case inst.MediaType == "":
	case inst.Reason != "":
		head = "gloss " + token + " refused: " + inst.Reason
	case inst.Instance != nil && !inst.RowOK:
		head = "gloss " + token + " (" + inst.Source + ") — not applied: " + inst.RowReason
	default:
		head = "glossed as " + token + " (" + inst.Source + ")"
	}
	if inst.SpecLine == "" {
		return head
	}
	if head == "" {
		return "spec: " + inst.SpecLine
	}
	return head + " — spec: " + inst.SpecLine
}
