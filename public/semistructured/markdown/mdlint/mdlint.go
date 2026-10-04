// Package mdlint checks one Obsidian-flavoured markdown document for
// constructs that render differently from what their author meant: a link to
// a heading that is not there, a fence that never closes, frontmatter that is
// not YAML (ADR-0282 §SD4).
//
// Rules look at one document. A wikilink to another page cannot be resolved
// here and is never reported; a link into the same document is. The checks
// run over an mdspan.Doc, so a construct inside a code block or code span is
// literal text and is not checked.
package mdlint

import (
	"cmp"
	"slices"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdspan"
)

// SeverityE ranks a finding.
type SeverityE uint8

const (
	SeverityInfo SeverityE = iota
	SeverityWarn
	SeverityError
)

var AllSeverities = []SeverityE{SeverityInfo, SeverityWarn, SeverityError}

func (inst SeverityE) String() string {
	switch inst {
	case SeverityInfo:
		return "info"
	case SeverityWarn:
		return "warn"
	case SeverityError:
		return "error"
	}
	return "unknown"
}

// Finding is one diagnostic. Lines and columns are 1-based, columns in
// runes; the end is exclusive.
type Finding struct {
	Rule     string
	Severity SeverityE
	Line     int32
	Col      int32
	EndLine  int32
	EndCol   int32
	Message  string
	// Span is the finding's byte range in the source.
	Span mdspan.Span
}

// RuleI is one check.
type RuleI interface {
	// Id is the stable identifier, `ML` and three digits.
	Id() string
	// Name is a short kebab-case label.
	Name() string
	// Summary says in one line what the rule reports.
	Summary() string
	Check(d *mdspan.Doc, r *Reporter)
}

// Reporter collects a rule's findings against one document.
type Reporter struct {
	doc      *mdspan.Doc
	rule     string
	findings []Finding
}

// Report adds a finding over span s.
func (inst *Reporter) Report(s mdspan.Span, sev SeverityE, msg string) {
	l, c := inst.doc.Index.Position(s.Start)
	el, ec := inst.doc.Index.Position(max(s.End, s.Start))
	inst.findings = append(inst.findings, Finding{
		Rule: inst.rule, Severity: sev, Message: msg, Span: s,
		Line: int32(l), Col: int32(c), EndLine: int32(el), EndCol: int32(ec),
	})
}

// Linter runs a set of rules.
type Linter struct {
	rules    []RuleI
	disabled map[string]bool
}

// NewLinter runs the given rules.
func NewLinter(rules ...RuleI) (inst *Linter) {
	inst = &Linter{rules: rules, disabled: make(map[string]bool, 4)}
	return
}

// NewDefaultLinter runs every rule of this package.
func NewDefaultLinter() *Linter { return NewLinter(AllRules()...) }

// Rules lists the registered rules.
func (inst *Linter) Rules() []RuleI { return inst.rules }

// Disable turns rules off by id.
func (inst *Linter) Disable(ids ...string) {
	for _, id := range ids {
		inst.disabled[id] = true
	}
}

// Enable turns rules back on by id.
func (inst *Linter) Enable(ids ...string) {
	for _, id := range ids {
		delete(inst.disabled, id)
	}
}

// IsEnabled says whether a rule runs.
func (inst *Linter) IsEnabled(id string) bool { return !inst.disabled[id] }

// Lint checks a parsed document. Findings come in source order.
func (inst *Linter) Lint(d *mdspan.Doc) (findings []Finding) {
	r := &Reporter{doc: d}
	for _, rule := range inst.rules {
		if inst.disabled[rule.Id()] {
			continue
		}
		r.rule = rule.Id()
		rule.Check(d, r)
	}
	findings = r.findings
	slices.SortStableFunc(findings, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(a.Span.Start, b.Span.Start), cmp.Compare(a.Rule, b.Rule))
	})
	return
}

// LintSource parses src and checks it.
func (inst *Linter) LintSource(src []byte) []Finding { return inst.Lint(mdspan.Parse(src)) }

// Within keeps the findings that touch lines ls.
func Within(findings []Finding, ls mdspan.LineSpan) (out []Finding) {
	for _, f := range findings {
		if int(f.EndLine) >= ls.First && int(f.Line) <= ls.Last {
			out = append(out, f)
		}
	}
	return
}
