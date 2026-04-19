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
	"gopkg.in/yaml.v3"
)

// RuleDL001 — every Markdown doc carries a compliant front-matter stanza.
//
// Implements DOCUMENTATION_STANDARD §4: the YAML stanza must declare a
// known 'type' and a 'status' value valid for that type. Rules DL002 and
// DL003 (status enum / review metadata) will share the same parser later;
// for now they are folded in here so a single rule covers the foundational
// front-matter contract.
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

var validTypesDL001 = map[string]struct{}{
	"reference":   {},
	"how-to":      {},
	"explanation": {},
	"tutorial":    {},
	"adr":         {},
}

var validStatusesDescriptive = map[string]struct{}{
	"draft":      {},
	"stable":     {},
	"deprecated": {},
	"superseded": {},
}

var validStatusesAdr = map[string]struct{}{
	"proposed":   {},
	"accepted":   {},
	"deprecated": {},
	"superseded": {},
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

	if meta.Type == "" {
		f := Finding{
			RuleId:   "DL001",
			Severity: FindingSeverityError,
			Path:     path,
			Line:     1,
			Col:      1,
			Message:  "front-matter missing required field 'type'",
		}
		cont = yield(f, nil)
		if !cont {
			return
		}
	} else {
		_, typeOk := validTypesDL001[meta.Type]
		if !typeOk {
			f := Finding{
				RuleId:   "DL001",
				Severity: FindingSeverityError,
				Path:     path,
				Line:     1,
				Col:      1,
				Message:  "front-matter 'type' value '" + meta.Type + "' is not one of: reference, how-to, explanation, tutorial, adr",
			}
			cont = yield(f, nil)
			if !cont {
				return
			}
		}
	}

	if meta.Status == "" {
		f := Finding{
			RuleId:   "DL001",
			Severity: FindingSeverityError,
			Path:     path,
			Line:     1,
			Col:      1,
			Message:  "front-matter missing required field 'status'",
		}
		cont = yield(f, nil)
		return
	}

	valid := validStatusesDescriptive
	if meta.Type == "adr" {
		valid = validStatusesAdr
	}
	_, statusOk := valid[meta.Status]
	if !statusOk {
		f := Finding{
			RuleId:   "DL001",
			Severity: FindingSeverityError,
			Path:     path,
			Line:     1,
			Col:      1,
			Message:  "front-matter 'status' value '" + meta.Status + "' is not valid for type '" + meta.Type + "'",
		}
		cont = yield(f, nil)
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
