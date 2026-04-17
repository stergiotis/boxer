package doclint

import (
	"io/fs"
	"iter"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL006 — link text must not be a bare directory name.
//
// Implements DOCUMENTATION_STANDARD §7: cross-package Markdown
// references must carry enough information that the reader can find
// the target. The bad pattern is `[leeway](../../../leeway)` where
// the link text is a single bare directory name and the reader has
// no orientation about which `leeway` is meant.
//
// Heuristic — a link is flagged when ALL of:
//
//   - URL is local (not http/https/mailto/etc) and not a pure anchor.
//   - URL contains '/' (some directory navigation).
//   - URL last segment has no '.' (target appears to be a directory,
//     not a file).
//   - Link text, with surrounding backticks stripped, equals the URL
//     last segment (case-insensitive) — i.e., text is just the bare
//     directory name.
//
// All four conditions together rule out the common safe cases:
// links to `*.md` files (extension excludes them), repo-relative
// paths used as link text (text contains '/'), fully-qualified Go
// import paths (text contains '/' and 'github.com/...'), anchor
// links, and external URLs.
//
// Severity is `warn` because this is style guidance, not a
// correctness violation.
type RuleDL006 struct{}

func NewRuleDL006() (inst *RuleDL006) {
	inst = &RuleDL006{}
	return
}

func (inst *RuleDL006) Id() (id string) {
	id = "DL006"
	return
}

func (inst *RuleDL006) Check(roots []string) iter.Seq2[Finding, error] {
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
				cont, fErr := checkOneDL006(p, yield)
				if fErr != nil {
					return fErr
				}
				if !cont {
					return filepath.SkipAll
				}
				return nil
			})
			if err != nil {
				yield(Finding{}, eb.Build().Str("root", root).Errorf("DL006 walk: %w", err))
				return
			}
		}
	}
}

func checkOneDL006(filePath string, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	var data []byte
	data, err = os.ReadFile(filePath)
	if err != nil {
		err = eb.Build().Str("path", filePath).Errorf("DL006 read: %w", err)
		return
	}
	_, body, ok, parseErr := parseMdFrontMatter(data)
	if !ok || parseErr != nil {
		body = data
	}
	lineOffset := frontMatterLineOffset(data, body)

	for _, link := range extractInlineLinks(body) {
		if isExternalUrl(link.URL) {
			continue
		}
		clean, anchorOnly := stripUrlFragment(link.URL)
		if anchorOnly || clean == "" {
			continue
		}
		if !strings.Contains(clean, "/") {
			continue
		}
		last := path.Base(clean)
		if strings.Contains(last, ".") {
			continue
		}
		text := stripBackticks(link.Text)
		if !strings.EqualFold(text, last) {
			continue
		}
		f := Finding{
			RuleId:   "DL006",
			Severity: FindingSeverityWarn,
			Path:     filePath,
			Line:     link.Line + lineOffset,
			Col:      1,
			Message:  "link text '" + text + "' is a bare directory name; use a fully qualified Go import path or a more descriptive label",
		}
		cont = yield(f, nil)
		if !cont {
			return
		}
	}
	return
}
