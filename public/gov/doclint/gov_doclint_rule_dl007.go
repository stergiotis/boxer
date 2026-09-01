package doclint

import (
	"iter"
	"net/url"
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
// '?query' suffix, percent-decodes the remainder (a link is a URL, so
// a file named 'Architecture Overview.md' is written
// 'Architecture%20Overview.md'; stat'ing the encoded form reported an
// existing file as missing), resolves the path against the containing
// file's directory, and stat's the result. Missing targets are errors;
// permission or other unexpected stat failures are warnings so the
// scan keeps going.
//
// A target git ignores counts as missing, at the same error severity.
// It stat's fine in a working checkout and is absent from every clean
// one, so without this the finding appears for the first time in CI.
// That is not hypothetical: links into a git-ignored doc tree were
// committed here and passed every local run until a CI-shaped checkout
// reported them. Ignored state is read once per root by gitIgnoredSet;
// when git is unavailable the set is empty and the rule degrades to the
// plain existence check.
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
	return runMarkdownCheck("DL007", roots, checkOneDL007)
}

func checkOneDL007(filePath string, ignored map[string]struct{}, yield func(Finding, error) bool) (cont bool, err error) {
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
		clean = percentDecodePath(clean)
		var resolved string
		if filepath.IsAbs(clean) {
			resolved = clean
		} else {
			resolved = filepath.Join(fileDir, clean)
		}
		_, statErr := os.Stat(resolved)
		if statErr == nil {
			if !isGitIgnoredTree(ignored, resolved) {
				continue
			}
			f := Finding{
				RuleId:   "DL007",
				Severity: FindingSeverityError,
				Path:     filePath,
				Line:     link.Line + lineOffset,
				Col:      1,
				Message: "link target '" + link.URL + "' is git-ignored (resolved to '" + resolved +
					"'): it exists in this checkout but never in a clean one",
			}
			cont = yield(f, nil)
			if !cont {
				return
			}
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

// percentDecodePath returns the path with percent-escapes decoded. A
// malformed escape ('%zz', a trailing '%') leaves the input untouched:
// the literal form is then what gets stat'd, which is also what a file
// named that way would need.
func percentDecodePath(p string) (out string) {
	out = p
	if !strings.Contains(p, "%") {
		return
	}
	dec, err := url.PathUnescape(p)
	if err == nil {
		out = dec
	}
	return
}
