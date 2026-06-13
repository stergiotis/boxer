// Package ignoreann parses line-level `// designlint:ignore=<rule-id> (reason)`
// suppressions per
// [tier1-mechanical.md §annotations](../../../../../doc/design-system/policy/tier1-mechanical.md).
//
// Designed to be cheap to build once per file and queried O(1) per call site:
// each Analyzer builds an Index per *ast.File it visits, then calls
// Suppressed(pos, ruleID) before reporting. A suppression at line N covers
// both line N (trailing comment) and line N+1 (preceding-line comment).
package ignoreann

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"
)

// ignoreRe matches `// designlint:ignore=L1,L2,...` optionally followed by a
// `(reason)` clause. The reason is documented as mandatory in
// tier1-mechanical.md but enforcing that is a v2 concern; v1 is permissive.
var ignoreRe = regexp.MustCompile(`//\s*designlint:ignore=([A-Za-z0-9_,]+)`)

// Index is a per-file lookup from line number to the set of suppressed rule
// IDs. Construct via Build; query via Suppressed.
type Index struct {
	fset *token.FileSet
	// line -> set of rule IDs ignored at that line
	ignored map[int]map[string]struct{}
}

// Build scans the file's comments and produces a queryable Index.
func Build(fset *token.FileSet, file *ast.File) (idx *Index) {
	idx = &Index{fset: fset, ignored: make(map[int]map[string]struct{})}
	if file == nil {
		return
	}
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			if !strings.HasPrefix(c.Text, "//") {
				continue
			}
			m := ignoreRe.FindStringSubmatch(c.Text)
			if m == nil {
				continue
			}
			line := fset.PositionFor(c.Slash, false).Line
			for _, raw := range strings.Split(m[1], ",") {
				id := strings.TrimSpace(raw)
				if id == "" {
					continue
				}
				idx.add(line, id)
				idx.add(line+1, id)
			}
		}
	}
	return
}

func (inst *Index) add(line int, id string) {
	set := inst.ignored[line]
	if set == nil {
		set = make(map[string]struct{})
		inst.ignored[line] = set
	}
	set[id] = struct{}{}
}

// Suppressed reports whether ruleID is suppressed at pos. ruleID is the
// rule's catalogue identifier (e.g. "L2", "L5", "L9").
func (inst *Index) Suppressed(pos token.Pos, ruleID string) (ok bool) {
	if inst == nil {
		return
	}
	line := inst.fset.PositionFor(pos, false).Line
	if set := inst.ignored[line]; set != nil {
		_, ok = set[ruleID]
	}
	return
}
