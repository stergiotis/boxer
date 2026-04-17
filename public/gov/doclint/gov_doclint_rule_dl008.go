package doclint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"iter"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL008 — Go doc-link references resolve to a real symbol.
//
// Implements DOCUMENTATION_STANDARD §1 Reference + §4 / §5. Go 1.19+
// doc comments use `[Name]` (current package) or `[pkg.Name]`
// (qualified) to link to symbols rendered on pkgsite. References that
// don't resolve degrade to plain bracketed text — silent rot.
//
// v1 scope: ONLY checks single-identifier `[Name]` references against
// the current package's exported symbols. The qualified `[pkg.Name]`
// and method `[Type.Method]` forms are reserved for v2 — they need
// per-file import resolution plus, for methods, type method-set
// traversal. The standard's §8 invariant table notes the limitation
// alongside the rule ID.
//
// Heuristic: an `[X]` candidate is a doc-link iff X starts with an
// uppercase letter and is at least two characters long. This excludes
//
//   - `[T any]` and other generic-type-parameter brackets
//   - `[3]int` array literals embedded in prose
//   - `[]byte` slice types embedded in prose
//   - lowercase identifiers (Go doc convention reserves `[Name]`
//     references for exported symbols)
//
// Indented lines (Go doc-comment code blocks) are skipped so example
// snippets inside doc comments don't spawn false positives.
//
// Severity is warn — broken doc links are real defects but each one
// is cheap to fix.
type RuleDL008 struct {
	pkgSymbols map[string]map[string]struct{}
}

func NewRuleDL008() (inst *RuleDL008) {
	inst = &RuleDL008{}
	return
}

func (inst *RuleDL008) Id() (id string) {
	id = "DL008"
	return
}

var dl008LinkRe = regexp.MustCompile(`\[([A-Z][A-Za-z0-9_]+)\]`)

func (inst *RuleDL008) Check(roots []string) iter.Seq2[Finding, error] {
	return func(yield func(Finding, error) bool) {
		err := inst.buildIndex(roots)
		if err != nil {
			yield(Finding{}, err)
			return
		}
		for _, root := range roots {
			walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, we error) error {
				if we != nil {
					return we
				}
				if d.IsDir() {
					if shouldSkipDir(d.Name()) {
						return filepath.SkipDir
					}
					return nil
				}
				if !IsInScopeForDL009(path) {
					return nil
				}
				cont, fErr := inst.checkOne(path, yield)
				if fErr != nil {
					return fErr
				}
				if !cont {
					return filepath.SkipAll
				}
				return nil
			})
			if walkErr != nil {
				yield(Finding{}, eb.Build().Str("root", root).Errorf("DL008 walk: %w", walkErr))
				return
			}
		}
	}
}

// buildIndex scans all non-test .go files under the roots and records
// each directory's exported top-level symbol set. Generated and IDL
// files are intentionally INCLUDED here (unlike the lint pass) because
// their declared symbols are real targets — only test files are
// excluded.
func (inst *RuleDL008) buildIndex(roots []string) (err error) {
	inst.pkgSymbols = make(map[string]map[string]struct{})
	for _, root := range roots {
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, we error) error {
			if we != nil {
				return we
			}
			if d.IsDir() {
				if shouldSkipDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			base := filepath.Base(path)
			if strings.HasSuffix(base, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return nil
			}
			dir := filepath.Dir(path)
			sink := inst.pkgSymbols[dir]
			if sink == nil {
				sink = make(map[string]struct{})
				inst.pkgSymbols[dir] = sink
			}
			collectExportedSymbolsDL008(file, sink)
			return nil
		})
		if err != nil {
			err = eb.Build().Str("root", root).Errorf("DL008 index build: %w", err)
			return
		}
	}
	return
}

func collectExportedSymbolsDL008(file *ast.File, sink map[string]struct{}) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil {
				continue
			}
			if ast.IsExported(d.Name.Name) {
				sink[d.Name.Name] = struct{}{}
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if ast.IsExported(s.Name.Name) {
						sink[s.Name.Name] = struct{}{}
					}
				case *ast.ValueSpec:
					for _, ident := range s.Names {
						if ast.IsExported(ident.Name) {
							sink[ident.Name] = struct{}{}
						}
					}
				}
			}
		}
	}
}

func (inst *RuleDL008) checkOne(filePath string, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	fset := token.NewFileSet()
	file, parseErr := parser.ParseFile(fset, filePath, nil, parser.ParseComments|parser.SkipObjectResolution)
	if parseErr != nil {
		return
	}
	dir := filepath.Dir(filePath)
	pkgSyms := inst.pkgSymbols[dir]

	visit := func(doc *ast.CommentGroup) bool {
		if doc == nil {
			return true
		}
		text := doc.Text()
		basePos := fset.Position(doc.Pos())
		for _, cand := range findDL008Candidates(text, basePos) {
			if _, ok := pkgSyms[cand.name]; ok {
				continue
			}
			f := Finding{
				RuleId:   "DL008",
				Severity: FindingSeverityWarn,
				Path:     filePath,
				Line:     int32(cand.line),
				Col:      int32(cand.col),
				Message:  "doc-link [" + cand.name + "] does not resolve to an exported symbol in this package",
			}
			if !yield(f, nil) {
				return false
			}
		}
		return true
	}

	if !visit(file.Doc) {
		cont = false
		return
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !visit(d.Doc) {
				cont = false
				return
			}
		case *ast.GenDecl:
			if !visit(d.Doc) {
				cont = false
				return
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !visit(s.Doc) {
						cont = false
						return
					}
				case *ast.ValueSpec:
					if !visit(s.Doc) {
						cont = false
						return
					}
				}
			}
		}
	}
	return
}

type dl008Cand struct {
	name string
	line int
	col  int
}

func findDL008Candidates(text string, basePos token.Position) (cands []dl008Cand) {
	for i, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ") {
			continue
		}
		codeSpans := findBacktickSpans(line)
		matches := dl008LinkRe.FindAllStringSubmatchIndex(line, -1)
		for _, m := range matches {
			if isInsideAnyByteSpan(m[0], m[1], codeSpans) {
				continue
			}
			name := line[m[2]:m[3]]
			cands = append(cands, dl008Cand{
				name: name,
				line: basePos.Line + i,
				col:  m[0] + 1,
			})
		}
	}
	return
}

// findBacktickSpans returns [start, end) byte ranges for inline code
// spans (delimited by single backticks) in line. Unmatched trailing
// backticks are ignored.
func findBacktickSpans(line string) (spans [][2]int) {
	inSpan := false
	spanStart := 0
	for i := 0; i < len(line); i++ {
		if line[i] != '`' {
			continue
		}
		if inSpan {
			spans = append(spans, [2]int{spanStart, i + 1})
			inSpan = false
			continue
		}
		spanStart = i
		inSpan = true
	}
	return
}

// isInsideAnyByteSpan reports whether the [start, end) range is fully
// contained in any of the given byte spans.
func isInsideAnyByteSpan(start, end int, spans [][2]int) (inside bool) {
	for _, s := range spans {
		if start >= s[0] && end <= s[1] {
			inside = true
			return
		}
	}
	return
}
