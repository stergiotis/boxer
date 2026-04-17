package doclint

import (
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL007 — every in-repo Markdown link resolves to an existing path.
//
// Implements DOCUMENTATION_STANDARD §7: links between docs and code
// must point at real files or directories. External URLs (http,
// https, mailto, ftp, ssh, tel) are not validated — that would
// require network access — and pure anchor links (#section) are
// skipped because anchor existence is a future concern.
//
// For each remaining local URL the rule strips any '#anchor' /
// '?query' suffix, resolves the path against the containing file's
// directory, and stat's the result. Missing targets are errors;
// permission or other unexpected stat failures are warnings so the
// scan keeps going.
type RuleDL007 struct{}

func NewRuleDL007() (inst *RuleDL007) {
	inst = &RuleDL007{}
	return
}

func (inst *RuleDL007) Id() (id string) {
	id = "DL007"
	return
}

func (inst *RuleDL007) Check(roots []string) iter.Seq2[Finding, error] {
	return func(yield func(Finding, error) bool) {
		for _, root := range roots {
			err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					if shouldSkipDir(d.Name()) {
						return filepath.SkipDir
					}
					return nil
				}
				base := filepath.Base(p)
				if !strings.HasSuffix(strings.ToLower(base), ".md") {
					return nil
				}
				if !IsInScopeForDL001(p, base) {
					return nil
				}
				cont, fErr := checkOneDL007(p, yield)
				if fErr != nil {
					return fErr
				}
				if !cont {
					return filepath.SkipAll
				}
				return nil
			})
			if err != nil {
				yield(Finding{}, eb.Build().Str("root", root).Errorf("DL007 walk: %w", err))
				return
			}
		}
	}
}

func checkOneDL007(filePath string, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	var data []byte
	data, err = os.ReadFile(filePath)
	if err != nil {
		err = eb.Build().Str("path", filePath).Errorf("DL007 read: %w", err)
		return
	}
	_, body, ok, parseErr := parseMdFrontMatter(data)
	if !ok || parseErr != nil {
		body = data
	}
	lineOffset := frontMatterLineOffset(data, body)
	fileDir := filepath.Dir(filePath)

	for _, link := range extractInlineLinks(body) {
		if isExternalUrl(link.URL) {
			continue
		}
		clean, anchorOnly := stripUrlFragment(link.URL)
		if anchorOnly || clean == "" {
			continue
		}
		var resolved string
		if filepath.IsAbs(clean) {
			resolved = clean
		} else {
			resolved = filepath.Join(fileDir, clean)
		}
		_, statErr := os.Stat(resolved)
		if statErr == nil {
			continue
		}
		if os.IsNotExist(statErr) {
			f := Finding{
				RuleId:   "DL007",
				Severity: FindingSeverityError,
				Path:     filePath,
				Line:     link.Line + lineOffset,
				Col:      1,
				Message:  "link target '" + link.URL + "' does not exist (resolved to '" + resolved + "')",
			}
			cont = yield(f, nil)
			if !cont {
				return
			}
			continue
		}
		f := Finding{
			RuleId:   "DL007",
			Severity: FindingSeverityWarn,
			Path:     filePath,
			Line:     link.Line + lineOffset,
			Col:      1,
			Message:  "link target '" + link.URL + "' could not be stat'd: " + statErr.Error(),
		}
		cont = yield(f, nil)
		if !cont {
			return
		}
	}
	return
}
