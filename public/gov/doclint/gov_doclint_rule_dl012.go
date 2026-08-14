package doclint

import (
	"iter"
	"os"

	"github.com/stergiotis/boxer/public/gov/adrcorpus"
	"github.com/stergiotis/boxer/public/gov/docstd"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL012 — an ADR sub-item declaration that nearly parses.
//
// The `subtask` table is built by scanning ADRs for sub-item declarations —
// SDs, milestones, phases. A bullet that names a marker in almost the right
// shape contributes nothing, and says so nowhere: the list still renders, the
// ADR still reads as a milestone list, and only someone running a query
// notices that the progress is missing. Ten ADRs carried one such shape for
// months, hiding 45 declared sub-items of which 36 were already done.
//
// This rule makes that failure loud at the point of authorship. It is the
// counterpart to the parser's strictness rather than a relaxation of it: the
// em-dash discipline is load-bearing (it separates a declaration from prose
// that merely names a marker), so the fix is to report the near miss, not to
// widen what counts as a declaration.
//
// The shapes it recognizes come from [adrcorpus.FindNearMisses], which shares
// its marker and dash patterns with the parser itself. A detector carrying its
// own copy of the accepted form drifts from the thing it checks.
type RuleDL012 struct{}

func NewRuleDL012() (inst *RuleDL012) {
	inst = &RuleDL012{}
	return
}

func (inst *RuleDL012) Id() (id string) {
	id = "DL012"
	return
}

func (inst *RuleDL012) Check(roots []string) iter.Seq2[Finding, error] {
	return runMarkdownCheck("DL012", roots, checkOneDL012)
}

func checkOneDL012(path string, _ map[string]struct{}, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("DL012 read: %w", err)
		return
	}
	meta, body, ok, parseErr := parseMdFrontMatter(data)
	if !ok || parseErr != nil {
		return
	}
	if meta.Type != docstd.TypeADR {
		return
	}
	// Line numbers are file-relative: the body starts after the front matter,
	// so the offset is however many lines that consumed.
	offset := countLines(data[:len(data)-len(body)])
	for _, nm := range adrcorpus.FindNearMisses(string(body), offset) {
		f := Finding{
			RuleId:   "DL012",
			Severity: FindingSeverityWarn,
			Path:     path,
			Line:     int32(nm.Line),
			Col:      1,
			Message:  "sub-item '" + nm.Marker + "' does not parse as a declaration and is absent from the subtask table: " + nm.Reason,
		}
		cont = yield(f, nil)
		if !cont {
			return
		}
	}
	return
}

func countLines(b []byte) (n int) {
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return
}
