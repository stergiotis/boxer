package doclint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"iter"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL009 — exported Go symbols carry doc comments that begin with
// the identifier name and end with a period.
//
// Implements DOCUMENTATION_STANDARD §1 Reference + §4. Per Go convention,
// a doc comment begins with the name of the item it describes; CODING-
// STANDARDS additionally requires the comment to end with a sentence-
// terminating punctuation mark.
//
// Files excluded:
//
//   - *_test.go        — test code
//   - *.out.go         — generated source kept in repo
//   - *.gen.go         — generated as part of build
//   - *.idl.go         — FFFI IDL files
//   - testdata/, vendor/, etc. — handled by shouldSkipDir
//
// What gets checked:
//
//   - Top-level functions and methods.
//   - Top-level type declarations (incl. inside grouped `type ( … )`).
//   - Top-level var and const declarations (incl. grouped). For groups
//     sharing one doc comment, the comment is matched against the first
//     name; later names in the group inherit the same comment, so the
//     prefix check is satisfied iff the comment begins with that first
//     name (an acceptable simplification for v1).
//
// Severity is warn — style guidance, not correctness.
type RuleDL009 struct{}

func NewRuleDL009() (inst *RuleDL009) {
	inst = &RuleDL009{}
	return
}

func (inst *RuleDL009) Id() (id string) {
	id = "DL009"
	return
}

func (inst *RuleDL009) Check(roots []string) iter.Seq2[Finding, error] {
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
				if !IsInScopeForDL009(path) {
					return nil
				}
				cont, fErr := checkOneDL009(path, yield)
				if fErr != nil {
					return fErr
				}
				if !cont {
					return filepath.SkipAll
				}
				return nil
			})
			if err != nil {
				yield(Finding{}, eb.Build().Str("root", root).Errorf("DL009 walk: %w", err))
				return
			}
		}
	}
}

// IsInScopeForDL009 returns true if the path is a Go source file that
// the standard expects to carry doc comments — i.e. not a test file
// and not generated source.
func IsInScopeForDL009(path string) (in bool) {
	if !strings.HasSuffix(path, ".go") {
		return
	}
	base := filepath.Base(path)
	if strings.HasSuffix(base, "_test.go") {
		return
	}
	if strings.HasSuffix(base, ".out.go") {
		return
	}
	if strings.HasSuffix(base, ".gen.go") {
		return
	}
	if strings.HasSuffix(base, ".idl.go") {
		return
	}
	in = true
	return
}

func checkOneDL009(filePath string, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	fset := token.NewFileSet()
	file, parseErr := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if parseErr != nil {
		f := Finding{
			RuleId:   "DL009",
			Severity: FindingSeverityWarn,
			Path:     filePath,
			Line:     1,
			Col:      1,
			Message:  "DL009 could not parse Go source: " + parseErr.Error(),
		}
		cont = yield(f, nil)
		return
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			cont = checkFuncDeclDL009(filePath, fset, d, yield)
		case *ast.GenDecl:
			cont = checkGenDeclDL009(filePath, fset, d, yield)
		}
		if !cont {
			return
		}
	}
	return
}

func checkFuncDeclDL009(filePath string, fset *token.FileSet, d *ast.FuncDecl, yield func(Finding, error) bool) (cont bool) {
	cont = true
	name := d.Name.Name
	if !ast.IsExported(name) {
		return
	}
	pos := fset.Position(d.Pos())
	kind := "function"
	if d.Recv != nil {
		kind = "method"
	}
	if d.Doc == nil {
		f := Finding{
			RuleId:   "DL009",
			Severity: FindingSeverityInfo,
			Path:     filePath,
			Line:     int32(pos.Line),
			Col:      int32(pos.Column),
			Message:  "exported " + kind + " '" + name + "' is missing a doc comment",
		}
		cont = yield(f, nil)
		return
	}
	text := strings.TrimSpace(d.Doc.Text())
	cont = checkDocCommentText(filePath, pos, name, kind, text, yield)
	return
}

func checkGenDeclDL009(filePath string, fset *token.FileSet, d *ast.GenDecl, yield func(Finding, error) bool) (cont bool) {
	cont = true
	groupDoc := ""
	if d.Doc != nil {
		groupDoc = strings.TrimSpace(d.Doc.Text())
	}

	var kind string
	switch d.Tok {
	case token.TYPE:
		kind = "type"
	case token.VAR:
		kind = "var"
	case token.CONST:
		kind = "const"
	default:
		return
	}

	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			cont = checkTypeSpecDL009(filePath, fset, s, groupDoc, kind, yield)
		case *ast.ValueSpec:
			cont = checkValueSpecDL009(filePath, fset, s, groupDoc, kind, yield)
		}
		if !cont {
			return
		}
	}
	return
}

func checkTypeSpecDL009(filePath string, fset *token.FileSet, s *ast.TypeSpec, groupDoc, kind string, yield func(Finding, error) bool) (cont bool) {
	cont = true
	name := s.Name.Name
	if !ast.IsExported(name) {
		return
	}
	pos := fset.Position(s.Pos())
	text := groupDoc
	if s.Doc != nil {
		text = strings.TrimSpace(s.Doc.Text())
	}
	if text == "" {
		f := Finding{
			RuleId:   "DL009",
			Severity: FindingSeverityInfo,
			Path:     filePath,
			Line:     int32(pos.Line),
			Col:      int32(pos.Column),
			Message:  "exported " + kind + " '" + name + "' is missing a doc comment",
		}
		cont = yield(f, nil)
		return
	}
	cont = checkDocCommentText(filePath, pos, name, kind, text, yield)
	return
}

func checkValueSpecDL009(filePath string, fset *token.FileSet, s *ast.ValueSpec, groupDoc, kind string, yield func(Finding, error) bool) (cont bool) {
	cont = true
	text := groupDoc
	if s.Doc != nil {
		text = strings.TrimSpace(s.Doc.Text())
	}
	for _, ident := range s.Names {
		name := ident.Name
		if !ast.IsExported(name) {
			continue
		}
		pos := fset.Position(ident.Pos())
		if text == "" {
			f := Finding{
				RuleId:   "DL009",
				Severity: FindingSeverityInfo,
				Path:     filePath,
				Line:     int32(pos.Line),
				Col:      int32(pos.Column),
				Message:  "exported " + kind + " '" + name + "' is missing a doc comment",
			}
			cont = yield(f, nil)
			if !cont {
				return
			}
			continue
		}
		cont = checkDocCommentText(filePath, pos, name, kind, text, yield)
		if !cont {
			return
		}
	}
	return
}

// checkDocCommentText verifies a non-empty doc comment text against the
// "begins with name, ends with sentence-terminator" contract.
func checkDocCommentText(filePath string, pos token.Position, name, kind, text string, yield func(Finding, error) bool) (cont bool) {
	cont = true
	if !strings.HasPrefix(text, name) {
		f := Finding{
			RuleId:   "DL009",
			Severity: FindingSeverityWarn,
			Path:     filePath,
			Line:     int32(pos.Line),
			Col:      int32(pos.Column),
			Message:  "doc comment for exported " + kind + " '" + name + "' does not begin with '" + name + "'",
		}
		cont = yield(f, nil)
		if !cont {
			return
		}
	}
	if !endsWithSentenceTerminator(text) {
		f := Finding{
			RuleId:   "DL009",
			Severity: FindingSeverityWarn,
			Path:     filePath,
			Line:     int32(pos.Line),
			Col:      int32(pos.Column),
			Message:  "doc comment for exported " + kind + " '" + name + "' does not end with '.', '!', or '?'",
		}
		cont = yield(f, nil)
	}
	return
}

func endsWithSentenceTerminator(text string) (ok bool) {
	if text == "" {
		return
	}
	last := text[len(text)-1]
	switch last {
	case '.', '!', '?':
		ok = true
	}
	return
}
