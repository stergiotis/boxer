package trailviewer

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// CaseKindE is a case's key kind (ADR-0350 §SD1, proposed). The set is
// fixed by the ADR: a scene and a catalog argument name a case the same
// way, and the viewer never reads mart columns at run time to extend it.
type CaseKindE uint8

const (
	CaseAll CaseKindE = iota
	CaseRun
	CaseWindow
	CaseApp
	CaseConversation
	CaseTask
	CaseSubject
	CasePrincipal
	CaseEntity
	CaseDestination
)

// AllCaseKinds lists the kinds in the order the combo shows them.
var AllCaseKinds = []CaseKindE{CaseRun, CaseWindow, CaseApp, CaseConversation, CaseTask, CaseSubject, CasePrincipal, CaseEntity, CaseDestination, CaseAll}

var caseKindNames = [...]string{"all", "run", "window", "app", "conversation", "task", "subject", "principal", "entity", "destination"}

// caseKindHints say what a case's value is, for the value box and the
// catalog's error.
var caseKindHints = [...]string{
	"no value: every row in the time window",
	"a run id",
	"run:instance — a run id and a window's instance key",
	"an app id",
	"a conversation id",
	"a task id",
	"a data subject, a number",
	"a principal reference",
	"one reference as type:value",
	"an egress destination host",
}

func (inst CaseKindE) String() (s string) {
	if int(inst) < len(caseKindNames) {
		return caseKindNames[inst]
	}
	return "unknown"
}

// Hint says what the kind's value is.
func (inst CaseKindE) Hint() (s string) {
	if int(inst) < len(caseKindHints) {
		return caseKindHints[inst]
	}
	return ""
}

// agentic reports whether rows of the case carry the conversation and task
// columns worth showing.
func (inst CaseKindE) agentic() bool { return inst == CaseConversation || inst == CaseTask }

func caseKindNameList() (s string) {
	return strings.Join(caseKindNames[:], ", ")
}

// ParseCaseKind reads a kind's name.
func ParseCaseKind(s string) (k CaseKindE, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for i, n := range caseKindNames {
		if n == s {
			return CaseKindE(i), true
		}
	}
	return
}

// Case is one key kind and one value.
type Case struct {
	Kind  CaseKindE
	Value string
}

// validate checks the value is one the kind can select by.
func (inst Case) validate() (err error) {
	v := strings.TrimSpace(inst.Value)
	switch inst.Kind {
	case CaseAll:
		if v != "" {
			err = app.RefuseOperation("the all case takes no value")
		}
		return
	case CaseWindow:
		if _, _, ok := inst.window(); !ok {
			err = app.RefuseOperation("a window case's value is run:instance, the instance a number")
		}
		return
	case CaseSubject:
		if _, perr := strconv.ParseUint(v, 10, 64); perr != nil {
			err = app.RefuseOperation("a subject case's value is a number")
		}
		return
	case CaseEntity:
		if t, _, found := strings.Cut(v, ":"); !found || t == "" {
			err = app.RefuseOperation("an entity case's value is one reference as type:value")
		}
		return
	}
	if inst.Kind > CaseDestination {
		return app.RefuseOperation("the case kind is one of " + caseKindNameList())
	}
	if v == "" {
		err = app.RefuseOperation("a " + inst.Kind.String() + " case needs a value: " + inst.Kind.Hint())
	}
	return
}

// window splits a window case's value at its last colon.
func (inst Case) window() (run string, instance uint64, ok bool) {
	v := strings.TrimSpace(inst.Value)
	i := strings.LastIndexByte(v, ':')
	if i <= 0 {
		return
	}
	n, err := strconv.ParseUint(v[i+1:], 10, 64)
	if err != nil {
		return
	}
	return v[:i], n, true
}

func (inst Case) String() (s string) {
	if inst.Kind == CaseAll {
		return "all"
	}
	return inst.Kind.String() + " " + inst.Value
}

// Window is the time window a case is read over: the span ending now when
// Last is set, else [From, To).
type Window struct {
	Last     time.Duration
	From, To time.Time
}

// defaultWindow is the last hour.
var defaultWindow = Window{Last: time.Hour}

// Resolve is the window's bounds at now.
func (inst Window) Resolve(now time.Time) (from, to time.Time) {
	if inst.Last > 0 {
		return now.Add(-inst.Last), now
	}
	return inst.From, inst.To
}

func (inst Window) validate() (err error) {
	switch {
	case inst.Last < 0:
		err = app.RefuseOperation("last is not negative")
	case inst.Last == 0 && (inst.From.IsZero() || inst.To.IsZero()):
		err = app.RefuseOperation("a window is last, or both from and to")
	case inst.Last == 0 && !inst.To.After(inst.From):
		err = app.RefuseOperation("to is after from")
	}
	return
}

// String is how the window reads beside the picker.
func (inst Window) String() (s string) {
	if inst.Last > 0 {
		return "last " + inst.Last.String()
	}
	return inst.From.UTC().Format(time.RFC3339) + " – " + inst.To.UTC().Format(time.RFC3339)
}

// FacetE is a faceted column (ADR-0350 §SD4).
type FacetE uint8

const (
	FacetKind FacetE = iota
	FacetStatus
	FacetApp
	FacetDomainAction
	FacetPrincipal
)

// AllFacets lists the facets in the order the pane shows them.
var AllFacets = []FacetE{FacetKind, FacetStatus, FacetApp, FacetDomainAction, FacetPrincipal}

var facetNames = [...]string{"kind", "status", "app", "domain.action", "principal"}

func (inst FacetE) String() (s string) {
	if int(inst) < len(facetNames) {
		return facetNames[inst]
	}
	return "unknown"
}

// ParseFacet reads a facet's name.
func ParseFacet(s string) (f FacetE, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for i, n := range facetNames {
		if n == s {
			return FacetE(i), true
		}
	}
	return
}

// Term is one value a facet includes or excludes.
type Term struct {
	Facet   FacetE
	Value   string
	Exclude bool
}

// Filter is the terms the table is narrowed by: within a facet, included
// values are ORed and excluded values removed; facets are ANDed.
type Filter struct {
	Terms []Term
}

// normalized sorts the terms and drops duplicates, so two filters that
// select the same rows compare equal by key.
func (inst Filter) normalized() (out Filter) {
	out.Terms = slices.Clone(inst.Terms)
	slices.SortFunc(out.Terms, func(a, b Term) int {
		switch {
		case a.Facet != b.Facet:
			return int(a.Facet) - int(b.Facet)
		case a.Exclude != b.Exclude:
			if a.Exclude {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Value, b.Value)
	})
	out.Terms = slices.Compact(out.Terms)
	return
}

// key is a comparable rendering of the filter.
func (inst Filter) key() (s string) {
	b := strings.Builder{}
	for _, t := range inst.normalized().Terms {
		b.WriteString(t.Facet.String())
		if t.Exclude {
			b.WriteString("!=")
		} else {
			b.WriteString("=")
		}
		b.WriteString(strconv.Quote(t.Value))
		b.WriteString(";")
	}
	return b.String()
}

// String is the filter as the pane states it.
func (inst Filter) String() (s string) {
	if len(inst.Terms) == 0 {
		return "no filter"
	}
	parts := make([]string, 0, len(inst.Terms))
	for _, t := range inst.normalized().Terms {
		op := " = "
		if t.Exclude {
			op = " ≠ "
		}
		parts = append(parts, t.Facet.String()+op+t.Value)
	}
	return strings.Join(parts, ", ")
}

// with is the filter with v of f included or excluded — a click on a facet
// value — replacing any term the value already had.
func (inst Filter) with(f FacetE, v string, exclude bool) (out Filter) {
	out = inst.without(f, v)
	out.Terms = append(out.Terms, Term{Facet: f, Value: v, Exclude: exclude})
	out = out.normalized()
	return
}

// without is the filter with no term for v of f.
func (inst Filter) without(f FacetE, v string) (out Filter) {
	for _, t := range inst.Terms {
		if t.Facet == f && t.Value == v {
			continue
		}
		out.Terms = append(out.Terms, t)
	}
	return
}

// term reports whether v of f has a term, and which.
func (inst Filter) term(f FacetE, v string) (exclude bool, ok bool) {
	for _, t := range inst.Terms {
		if t.Facet == f && t.Value == v {
			return t.Exclude, true
		}
	}
	return
}

// split is the included and excluded values of f.
func (inst Filter) split(f FacetE) (incl []string, excl []string) {
	for _, t := range inst.normalized().Terms {
		if t.Facet != f {
			continue
		}
		if t.Exclude {
			excl = append(excl, t.Value)
		} else {
			incl = append(incl, t.Value)
		}
	}
	return
}

// scope is what one read of the table is keyed by: comparable, so a fill
// for a scope the window has left is dropped.
type scope struct {
	Case   Case
	Window Window
	Filter string
	Strip  StripByE
}

// StripByE is what the strip's bars are coloured by.
type StripByE uint8

const (
	StripByKind StripByE = iota
	StripByStatus
)

func (inst StripByE) String() string {
	if inst == StripByStatus {
		return "status"
	}
	return "kind"
}

// TabE is the detail pane's tab: the row's fields, its previous-state
// diff, or the rows around it. M4's tree tab joins the same resource.
type TabE uint8

const (
	TabFields TabE = iota
	TabPrevious
	TabContext
)

var tabNames = [...]string{"fields", "previous", "context"}

func (inst TabE) String() string {
	if int(inst) < len(tabNames) {
		return tabNames[inst]
	}
	return "unknown"
}

// ParseTab reads a tab's name.
func ParseTab(s string) (t TabE, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for i, n := range tabNames {
		if n == s {
			return TabE(i), true
		}
	}
	return
}
