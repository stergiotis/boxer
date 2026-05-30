//go:build llm_generated_opus47

package doclint

import (
	"bytes"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/gov/docstd"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"gopkg.in/yaml.v3"
)

// RuleDL001 — every Markdown doc carries a compliant front-matter stanza.
//
// Implements DOCUMENTATION_STANDARD §4: the YAML stanza must declare a
// known 'type' and a 'status' value valid for that type. The type/status
// enums and the conformance check live in [docstd], shared with the
// keelson help library; this rule owns front-matter extraction, YAML
// parsing, and mapping each [docstd.Violation] to a DL001 finding.
//
// Files outside the doc-standard scope (testdata, generated, changelog
// summaries, Claude Code SKILL files) are skipped silently.
type RuleDL001 struct{}

func NewRuleDL001() (inst *RuleDL001) {
	inst = &RuleDL001{}
	return
}

func (inst *RuleDL001) Id() (id string) {
	id = "DL001"
	return
}

type frontMatterDL001 struct {
	Type         string `yaml:"type"`
	Audience     string `yaml:"audience,omitempty"`
	Status       string `yaml:"status"`
	ReviewedBy   string `yaml:"reviewed-by,omitempty"`
	ReviewedDate string `yaml:"reviewed-date,omitempty"`
}

// IsInScopeForDL001 returns true if path/base should be evaluated by DL001.
// The intent is to exclude generated, vendored, fixture, and out-of-standard
// files (changelog summaries, Claude Code SKILL files, and the module-root
// README.md that serves as the GitHub landing page per §3).
func IsInScopeForDL001(path string, base string) (in bool) {
	if strings.HasSuffix(base, ".gen.md") {
		return
	}
	sep := string(filepath.Separator)
	if strings.Contains(path, sep+"testdata"+sep) {
		return
	}
	if strings.Contains(path, sep+"changelog"+sep) {
		return
	}
	if base == "SKILL.md" {
		return
	}
	if base == "README.md" {
		dir := filepath.Dir(path)
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return
		}
	}
	in = true
	return
}

func (inst *RuleDL001) Check(roots []string) iter.Seq2[Finding, error] {
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
				cont, fErr := checkOneDL001(path, yield)
				if fErr != nil {
					return fErr
				}
				if !cont {
					return filepath.SkipAll
				}
				return nil
			})
			if err != nil {
				yield(Finding{}, eb.Build().Str("root", root).Errorf("DL001 walk: %w", err))
				return
			}
		}
	}
}

func checkOneDL001(path string, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("DL001 read: %w", err)
		return
	}

	fm, _, ok := ExtractFrontMatter(data)
	if !ok {
		f := Finding{
			RuleId:   "DL001",
			Severity: FindingSeverityError,
			Path:     path,
			Line:     1,
			Col:      1,
			Message:  "missing or malformed YAML front-matter stanza (expected '---' on line 1, then YAML, then '---')",
		}
		cont = yield(f, nil)
		return
	}

	var meta frontMatterDL001
	parseErr := yaml.Unmarshal(fm, &meta)
	if parseErr != nil {
		f := Finding{
			RuleId:   "DL001",
			Severity: FindingSeverityError,
			Path:     path,
			Line:     1,
			Col:      1,
			Message:  "YAML front-matter parse error: " + parseErr.Error(),
		}
		cont = yield(f, nil)
		return
	}

	// allowADR=true: repo-wide linting accepts 'type: adr'. The enums and
	// the conformance check are shared with the keelson help library via
	// docstd; DL001 only frames each violation as an error finding.
	for _, v := range docstd.ValidateFrontmatter(meta.Type, meta.Status, true) {
		f := Finding{
			RuleId:   "DL001",
			Severity: FindingSeverityError,
			Path:     path,
			Line:     1,
			Col:      1,
			Message:  v.Message,
		}
		cont = yield(f, nil)
		if !cont {
			return
		}
	}

	return
}

// ExtractFrontMatter returns the YAML front-matter (between leading `---`
// markers), the remainder of the document, and whether front-matter was
// found. Tolerant of CRLF line endings and a leading UTF-8 BOM.
//
// The returned fm slice has CRLF normalised to LF; body is returned
// unchanged after the closing `---` line.
func ExtractFrontMatter(data []byte) (fm []byte, body []byte, ok bool) {
	data = bytes.TrimPrefix(data, []byte("\ufeff"))
	normalised := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	lines := bytes.Split(normalised, []byte("\n"))
	if len(lines) == 0 {
		return
	}
	if string(lines[0]) != "---" {
		return
	}
	for i := 1; i < len(lines); i++ {
		if string(lines[i]) == "---" {
			fm = bytes.Join(lines[1:i], []byte("\n"))
			if i+1 < len(lines) {
				body = bytes.Join(lines[i+1:], []byte("\n"))
			}
			ok = true
			return
		}
	}
	return
}
