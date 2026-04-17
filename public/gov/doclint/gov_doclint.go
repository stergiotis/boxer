package doclint

import (
	"iter"
)

type FindingSeverityE uint8

const (
	FindingSeverityInfo  FindingSeverityE = 1
	FindingSeverityWarn  FindingSeverityE = 2
	FindingSeverityError FindingSeverityE = 3
)

var AllFindingSeverities = []FindingSeverityE{
	FindingSeverityInfo,
	FindingSeverityWarn,
	FindingSeverityError,
}

func (inst FindingSeverityE) String() (s string) {
	switch inst {
	case FindingSeverityInfo:
		s = "info"
	case FindingSeverityWarn:
		s = "warn"
	case FindingSeverityError:
		s = "error"
	default:
		s = "unknown"
	}
	return
}

// Finding is a single rule violation discovered during a lint pass.
//
// Line and Col are 1-based; zero means "not pinpointed within the file".
type Finding struct {
	RuleId   string           `json:"rule"`
	Severity FindingSeverityE `json:"severity"`
	Path     string           `json:"path"`
	Line     int32            `json:"line,omitempty"`
	Col      int32            `json:"col,omitempty"`
	Message  string           `json:"message"`
}

// RuleI is implemented by every doclint rule.
//
// Check walks the supplied roots and yields findings as they are produced.
// A non-nil error in the second yield slot indicates a walk-time failure
// (e.g. unreadable file) and aborts that rule's pass.
type RuleI interface {
	Id() (id string)
	Check(roots []string) iter.Seq2[Finding, error]
}

// Linter aggregates rules and runs them in sequence.
//
// Zero value is usable; rules are added via Register.
type Linter struct {
	rules []RuleI
}

func NewLinter() (inst *Linter) {
	inst = &Linter{}
	return
}

func (inst *Linter) Register(r RuleI) {
	inst.rules = append(inst.rules, r)
}

// Run executes every registered rule against the given roots and yields
// findings as they are produced. A non-nil error aborts the run.
func (inst *Linter) Run(roots []string) iter.Seq2[Finding, error] {
	return func(yield func(Finding, error) bool) {
		for _, r := range inst.rules {
			for f, err := range r.Check(roots) {
				if !yield(f, err) {
					return
				}
				if err != nil {
					return
				}
			}
		}
	}
}

// shouldSkipDir is consulted by every rule's filesystem walker to keep
// vendored, generated, version-control, fixture, and template trees out
// of the regular lint scope.
//
// Excludes:
//   - .git           — version control metadata
//   - node_modules   — JS dependency tree
//   - vendor         — Go vendored deps
//   - testdata       — Go convention; per-rule fixtures live here
//   - templates      — scaffolding the standard ships under doc/templates/;
//                      its files have intentional draft/proposed status
//                      and would otherwise show up in DL011 reports
//
// Run doclint with an explicit path under any of these directories to
// process them deliberately.
func shouldSkipDir(name string) (skip bool) {
	switch name {
	case ".git", "node_modules", "vendor", "testdata", "templates":
		skip = true
	}
	return
}
