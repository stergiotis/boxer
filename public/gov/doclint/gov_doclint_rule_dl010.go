//go:build llm_generated_opus47

package doclint

import (
	"bytes"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL010 — ADRs contain the required H2 sections.
//
// Implements DOCUMENTATION_STANDARD §1 ADR rules: every ADR file carries
// `Context`, `Decision`, `Alternatives`, `Consequences`, `Status` as H2
// headings. Order is not enforced; presence is. The optional
// `Design space (QOC)` section is not constrained by this rule.
//
// Only files whose front-matter declares `type: adr` are checked. Files
// DL001 would already flag (no stanza, malformed YAML) are silently
// skipped to avoid derived noise.
type RuleDL010 struct{}

func NewRuleDL010() (inst *RuleDL010) {
	inst = &RuleDL010{}
	return
}

func (inst *RuleDL010) Id() (id string) {
	id = "DL010"
	return
}

var requiredAdrSections = []string{
	"Context",
	"Decision",
	"Alternatives",
	"Consequences",
	"Status",
}

func (inst *RuleDL010) Check(roots []string) iter.Seq2[Finding, error] {
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
				cont, fErr := checkOneDL010(path, yield)
				if fErr != nil {
					return fErr
				}
				if !cont {
					return filepath.SkipAll
				}
				return nil
			})
			if err != nil {
				yield(Finding{}, eb.Build().Str("root", root).Errorf("DL010 walk: %w", err))
				return
			}
		}
	}
}

func checkOneDL010(path string, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("DL010 read: %w", err)
		return
	}
	meta, body, ok, parseErr := parseMdFrontMatter(data)
	if !ok || parseErr != nil {
		return
	}
	if meta.Type != "adr" {
		return
	}

	titles := extractH2Titles(body)
	for _, required := range requiredAdrSections {
		if isSectionPresent(titles, required) {
			continue
		}
		f := Finding{
			RuleId:   "DL010",
			Severity: FindingSeverityError,
			Path:     path,
			Line:     1,
			Col:      1,
			Message:  "ADR is missing required '## " + required + "' section",
		}
		cont = yield(f, nil)
		if !cont {
			return
		}
	}
	return
}

// extractH2Titles collects the set of H2 heading texts in body. Each entry
// is the trimmed string after `## `.
func extractH2Titles(body []byte) (titles map[string]struct{}) {
	titles = make(map[string]struct{})
	for _, raw := range bytes.Split(body, []byte("\n")) {
		line := bytes.TrimRight(raw, " \t\r")
		if !bytes.HasPrefix(line, []byte("## ")) {
			continue
		}
		title := string(bytes.TrimSpace(line[3:]))
		titles[title] = struct{}{}
	}
	return
}

// isSectionPresent returns true if `required` matches a section title
// either verbatim ("Status") or as a prefix followed by a space
// ("Status — overridden by ADR-0099"). The space-prefix form lets ADRs
// add descriptive suffixes without breaking presence checks.
func isSectionPresent(titles map[string]struct{}, required string) (present bool) {
	if _, ok := titles[required]; ok {
		present = true
		return
	}
	for title := range titles {
		if strings.HasPrefix(title, required+" ") {
			present = true
			return
		}
	}
	return
}
