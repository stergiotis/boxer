package doclint

import (
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL003 — review metadata required when status is stable / accepted.
//
// Implements DOCUMENTATION_STANDARD §4: docs whose status declares them
// authoritative must carry both `reviewed-by` and `reviewed-date`. The
// date must parse as YYYY-MM-DD; an unparseable value is downgraded to a
// warning so the missing-field error remains the headline issue.
//
// Files that DL001 would already flag (no stanza, malformed YAML) are
// silently skipped here so a single root cause does not produce a swarm
// of derived findings.
type RuleDL003 struct{}

func NewRuleDL003() (inst *RuleDL003) {
	inst = &RuleDL003{}
	return
}

func (inst *RuleDL003) Id() (id string) {
	id = "DL003"
	return
}

func (inst *RuleDL003) Check(roots []string) iter.Seq2[Finding, error] {
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
				cont, fErr := checkOneDL003(path, yield)
				if fErr != nil {
					return fErr
				}
				if !cont {
					return filepath.SkipAll
				}
				return nil
			})
			if err != nil {
				yield(Finding{}, eb.Build().Str("root", root).Errorf("DL003 walk: %w", err))
				return
			}
		}
	}
}

func checkOneDL003(path string, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("DL003 read: %w", err)
		return
	}
	meta, _, ok, parseErr := parseMdFrontMatter(data)
	if !ok || parseErr != nil {
		// DL001 owns these failure modes; staying silent here avoids
		// duplicating findings.
		return
	}

	requiresReview := false
	switch meta.Status {
	case "stable", "accepted":
		requiresReview = true
	}
	if !requiresReview {
		return
	}

	if meta.ReviewedBy == "" {
		f := Finding{
			RuleId:   "DL003",
			Severity: FindingSeverityError,
			Path:     path,
			Line:     1,
			Col:      1,
			Message:  "front-matter status is '" + meta.Status + "' but 'reviewed-by' is missing or empty",
		}
		cont = yield(f, nil)
		if !cont {
			return
		}
	}

	if meta.ReviewedDate == "" {
		f := Finding{
			RuleId:   "DL003",
			Severity: FindingSeverityError,
			Path:     path,
			Line:     1,
			Col:      1,
			Message:  "front-matter status is '" + meta.Status + "' but 'reviewed-date' is missing or empty",
		}
		cont = yield(f, nil)
		return
	}

	if !IsValidDateYMD(meta.ReviewedDate) {
		f := Finding{
			RuleId:   "DL003",
			Severity: FindingSeverityWarn,
			Path:     path,
			Line:     1,
			Col:      1,
			Message:  "'reviewed-date' value '" + meta.ReviewedDate + "' is not a valid YYYY-MM-DD date",
		}
		cont = yield(f, nil)
	}
	return
}

// IsValidDateYMD returns true if s parses as a calendar date in YYYY-MM-DD
// form (e.g. 2026-04-17). Rejects invalid days like 2026-13-45.
func IsValidDateYMD(s string) (ok bool) {
	_, err := time.Parse("2006-01-02", s)
	ok = err == nil
	return
}
