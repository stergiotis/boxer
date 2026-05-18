//go:build llm_generated_opus47

package codelint

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"
)

// directivePattern matches a `//boxer:lint disable=CSNNN[,CSNNN...] reason="..."`
// trailing comment. Multiple rule IDs may appear comma-separated. A reason
// clause is required; suppressions without a reason are themselves flagged
// (reserved CS999) and are not honoured here.
var directivePattern = regexp.MustCompile(`//\s*boxer:lint\s+disable=([A-Z]+\d+(?:\s*,\s*[A-Z]+\d+)*)\s+reason="[^"]+"`)

// fileDisables indexes per-file the set of (line, ruleId) suppressions.
type fileDisables struct {
	byLine map[int]map[string]struct{}
}

func newFileDisables() (inst *fileDisables) {
	inst = &fileDisables{byLine: make(map[int]map[string]struct{})}
	return
}

func (inst *fileDisables) add(line int, ruleId string) {
	set, ok := inst.byLine[line]
	if !ok {
		set = make(map[string]struct{})
		inst.byLine[line] = set
	}
	set[ruleId] = struct{}{}
}

func (inst *fileDisables) has(line int, ruleId string) (disabled bool) {
	set, ok := inst.byLine[line]
	if !ok {
		return
	}
	_, disabled = set[ruleId]
	return
}

// collectDisables scans a file's comments for suppression directives,
// indexing them by line number.
func collectDisables(fset *token.FileSet, file *ast.File) (d *fileDisables) {
	d = newFileDisables()
	for _, group := range file.Comments {
		for _, c := range group.List {
			matches := directivePattern.FindStringSubmatch(c.Text)
			if matches == nil {
				continue
			}
			line := fset.Position(c.Slash).Line
			for _, id := range strings.Split(matches[1], ",") {
				id = strings.TrimSpace(id)
				if id == "" {
					continue
				}
				d.add(line, id)
			}
		}
	}
	return
}
