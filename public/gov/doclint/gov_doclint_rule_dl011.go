//go:build llm_generated_opus47

package doclint

import (
	"iter"
	"os"

	"github.com/stergiotis/boxer/public/gov/docstd"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL011 — informational TODO list of open draft / proposed docs.
//
// Implements DOCUMENTATION_STANDARD §4 / §8: emits an info-level finding
// for every Markdown doc currently at status `draft` (descriptive) or
// `proposed` (ADR). Surfaces only at `--min-severity info`; never blocks
// a merge.
//
// Intended use is review-pass triage — "show me everything still
// pre-human-review so I can plan the next batch of stable / accepted
// flips".
//
// Files DL001 would already flag are silently skipped.
type RuleDL011 struct{}

func NewRuleDL011() (inst *RuleDL011) {
	inst = &RuleDL011{}
	return
}

func (inst *RuleDL011) Id() (id string) {
	id = "DL011"
	return
}

func (inst *RuleDL011) Check(roots []string) iter.Seq2[Finding, error] {
	return runMarkdownCheck("DL011", roots, checkOneDL011)
}

func checkOneDL011(path string, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("DL011 read: %w", err)
		return
	}
	meta, _, ok, parseErr := parseMdFrontMatter(data)
	if !ok || parseErr != nil {
		return
	}

	var label string
	switch meta.Status {
	case docstd.StatusDraft:
		label = docstd.StatusDraft
	case docstd.StatusProposed:
		label = docstd.StatusProposed
	default:
		return
	}

	f := Finding{
		RuleId:   "DL011",
		Severity: FindingSeverityInfo,
		Path:     path,
		Line:     1,
		Col:      1,
		Message:  "doc currently at status: " + label + " — pending review",
	}
	cont = yield(f, nil)
	return
}
