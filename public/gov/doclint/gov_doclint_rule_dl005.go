package doclint

import (
	"io/fs"
	"iter"
	"path/filepath"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL005 — banned filenames absent under scoped paths.
//
// Implements DOCUMENTATION_STANDARD §6 (Banned Files): the listed names
// indicate that ad-hoc decision/idea/note files are sneaking back in and
// must be split into EXPLANATION.md and/or an ADR.
type RuleDL005 struct{}

var bannedFilenamesDL005 = map[string]struct{}{
	"SPEC.md":   {},
	"DESIGN.md": {},
	"ARCH.md":   {},
	"NOTES.md":  {},
	"MISC.md":   {},
	"TODO.md":   {},
	"IDEA.md":   {},
	"IDEAS.md":  {},
}

func NewRuleDL005() (inst *RuleDL005) {
	inst = &RuleDL005{}
	return
}

func (inst *RuleDL005) Id() (id string) {
	id = "DL005"
	return
}

func (inst *RuleDL005) Check(roots []string) iter.Seq2[Finding, error] {
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
				_, banned := bannedFilenamesDL005[base]
				if !banned {
					return nil
				}
				f := Finding{
					RuleId:   "DL005",
					Severity: FindingSeverityError,
					Path:     path,
					Line:     1,
					Col:      1,
					Message:  "banned filename '" + base + "' — split into EXPLANATION.md and/or an ADR per DOCUMENTATION_STANDARD §6",
				}
				if !yield(f, nil) {
					return filepath.SkipAll
				}
				return nil
			})
			if err != nil {
				yield(Finding{}, eb.Build().Str("root", root).Errorf("DL005 walk: %w", err))
				return
			}
		}
	}
}
