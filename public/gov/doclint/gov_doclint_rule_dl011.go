package doclint

import (
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"

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
	return func(yield func(Finding, error) bool) {
		for _, root := range roots {
			err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					if shouldSkipDir(d.Name()) {
						return filepath.SkipDir
					}
					return nil
				}
				base := filepath.Base(path)
				if !strings.HasSuffix(strings.ToLower(base), ".md") {
					return nil
				}
				if !IsInScopeForDL001(path, base) {
					return nil
				}
				cont, fErr := checkOneDL011(path, yield)
				if fErr != nil {
					return fErr
				}
				if !cont {
					return filepath.SkipAll
				}
				return nil
			})
			if err != nil {
				yield(Finding{}, eb.Build().Str("root", root).Errorf("DL011 walk: %w", err))
				return
			}
		}
	}
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
	case "draft":
		label = "draft"
	case "proposed":
		label = "proposed"
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
