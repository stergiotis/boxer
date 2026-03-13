//go:build llm_generated_gemini3pro

package stubber

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"path"
	"strconv"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/code/analysis/golang/common"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"golang.org/x/tools/imports"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Shared panic stub to reduce allocations
var stubBlock = &ast.BlockStmt{
	List: []ast.Stmt{
		&ast.ExprStmt{
			X: &ast.CallExpr{
				Fun:  &ast.Ident{Name: "panic"},
				Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"stub"`}},
			},
		},
	},
}

// GoFilter processes Go source code to filter out private elements and public function bodies.
type GoFilter struct {
	BuildTag string
}

func NewGoFilter(buildTag string) *GoFilter {
	return &GoFilter{
		BuildTag: buildTag,
	}
}

// Process parses source, filters private elements, handles imports, and writes to w.
// filename is required for goimports to apply correct formatting rules.
func (inst *GoFilter) Process(ctx context.Context, filename string, r io.Reader, w io.Writer) (err error) {
	// 1. Read Source
	src, err := io.ReadAll(r)
	if err != nil {
		return eh.Errorf("unable to read source: %w", err)
	}

	// 2. Parse AST
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return eh.Errorf("unable to parse source: %w", err)
	}

	// 3. Filter Declarations
	var keptDecls []ast.Decl
	for _, decl := range file.Decls {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if inst.shouldKeepDecl(decl) {
			keptDecls = append(keptDecls, decl)
		}
	}
	file.Decls = keptDecls

	// 4. Anonymize Unused Imports
	// Converts unused imports to `_ "pkg"` so they aren't removed by goimports.
	inst.anonymizeUnusedImports(file)

	// 5. Update Build Constraints (New Step)
	// Attempts to merge the tag into the AST. Returns false if no directive existed.
	buildTagInAst := inst.updateBuildConstraints(file)

	// 6. Render AST to buffer
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return eh.Errorf("unable to format AST: %w", err)
	}

	// 7. Prepend Build Tag (if not merged in AST)
	outputBytes := buf.Bytes()
	if !buildTagInAst && inst.BuildTag != "" {
		// Prepend standard new directive
		prefix := fmt.Sprintf("//go:build %s\n\n", inst.BuildTag)
		outputBytes = append([]byte(prefix), outputBytes...)
	}

	// 8. Process Imports and Format (goimports)
	formatted, err := imports.Process(filename, outputBytes, nil)
	if err != nil {
		return eb.Build().Str("filename", filename).Errorf("unable to process imports: %w", err)
	}

	// 9. Write Output
	if _, err := w.Write(formatted); err != nil {
		return eh.Errorf("unable to write output: %w", err)
	}

	return nil
}

func (inst *GoFilter) shouldKeepDecl(decl ast.Decl) bool {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return inst.processFunc(d)
	case *ast.GenDecl:
		return inst.processGenDecl(d)
	default:
		return true
	}
}

func (inst *GoFilter) processFunc(fn *ast.FuncDecl) bool {
	if !fn.Name.IsExported() && fn.Name.Name != "main" && fn.Name.Name != "init" {
		return false
	}
	// Validate Signature (remove if it relies on private types)
	if !inst.validFieldList(fn.Recv) ||
		!inst.validFieldList(fn.Type.Params) ||
		!inst.validFieldList(fn.Type.Results) ||
		!inst.validFieldList(fn.Type.TypeParams) {
		return false
	}

	if fn.Body != nil {
		fn.Body = stubBlock
	}
	return true
}

func (inst *GoFilter) processGenDecl(gen *ast.GenDecl) bool {
	if gen.Tok == token.IMPORT {
		// Iterate over import specs and convert them to side effect imports
		for _, spec := range gen.Specs {
			if imp, ok := spec.(*ast.ImportSpec); ok {
				//// Skip if already a side effect (_) or dot (.) import
				//if imp.Name != nil && (imp.Name.Name == "_" || imp.Name.Name == ".") {
				//	continue
				//}
				if imp.Name == nil || imp.Name.Name == "" {
					// Force the import to be a side effect import
					imp.Name = &ast.Ident{Name: "_"}
				}
			}
		}
		return true
	}

	// Filter Specs into new slice
	var keptSpecs []ast.Spec
	for _, spec := range gen.Specs {
		if inst.processSpec(spec) {
			keptSpecs = append(keptSpecs, spec)
		}
	}
	gen.Specs = keptSpecs
	return len(gen.Specs) > 0
}

func (inst *GoFilter) processSpec(spec ast.Spec) bool {
	switch s := spec.(type) {
	case *ast.ValueSpec:
		return inst.processValueSpec(s)
	case *ast.TypeSpec:
		if !s.Name.IsExported() {
			return false
		}
		if !inst.validFieldList(s.TypeParams) {
			return false
		}

		// Filter composite types instead of strictly validating them
		switch t := s.Type.(type) {
		case *ast.StructType:
			inst.processStructFields(t)
			return true
		case *ast.InterfaceType:
			inst.processInterfaceMethods(t)
			return true
		}

		// For aliases or simple composites (Arrays, Maps), strictly validate
		if !inst.isExportedType(s.Type) {
			return false
		}
		return true
	default:
		return true
	}
}

func (inst *GoFilter) processValueSpec(s *ast.ValueSpec) bool {
	// If the variable explicitly uses a private type, remove it.
	if s.Type != nil && !inst.isExportedType(s.Type) {
		return false
	}

	var keptNames []*ast.Ident
	var keptValues []ast.Expr

	for i, name := range s.Names {
		if name.IsExported() {
			keptNames = append(keptNames, name)
			if i < len(s.Values) {
				keptValues = append(keptValues, inst.sanitizeExpr(s.Values[i]))
			}
		}
	}

	s.Names = keptNames
	s.Values = keptValues
	return len(s.Names) > 0
}

func (inst *GoFilter) processStructFields(st *ast.StructType) {
	if st.Fields == nil {
		return
	}

	var keptFields []*ast.Field
	for _, field := range st.Fields.List {
		// Embedded field
		if len(field.Names) == 0 {
			if inst.isExportedType(field.Type) {
				keptFields = append(keptFields, field)
			}
			continue
		}

		// Named fields
		var keptNames []*ast.Ident
		for _, name := range field.Names {
			if name.IsExported() {
				keptNames = append(keptNames, name)
			}
		}

		if len(keptNames) > 0 {
			field.Names = keptNames
			keptFields = append(keptFields, field)
		}
	}
	st.Fields.List = keptFields
}

func (inst *GoFilter) processInterfaceMethods(it *ast.InterfaceType) {
	if it.Methods == nil {
		return
	}
	var keptMethods []*ast.Field
	for _, field := range it.Methods.List {
		// Embedded Interface
		if len(field.Names) == 0 {
			if inst.isExportedType(field.Type) {
				keptMethods = append(keptMethods, field)
			}
			continue
		}
		// Explicit Method
		if field.Names[0].IsExported() && inst.isExportedType(field.Type) {
			keptMethods = append(keptMethods, field)
		}
	}
	it.Methods.List = keptMethods
}

func (inst *GoFilter) sanitizeExpr(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case *ast.FuncLit:
		e.Body = stubBlock
		return e
	case *ast.CompositeLit:
		var keptElts []ast.Expr
		for _, elt := range e.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok && !key.IsExported() {
					continue
				}
			}
			keptElts = append(keptElts, inst.sanitizeExpr(elt))
		}
		e.Elts = keptElts
		return e
	case *ast.UnaryExpr:
		e.X = inst.sanitizeExpr(e.X)
	case *ast.BinaryExpr:
		e.X = inst.sanitizeExpr(e.X)
		e.Y = inst.sanitizeExpr(e.Y)
	case *ast.ParenExpr:
		e.X = inst.sanitizeExpr(e.X)
	}
	return expr
}
func (inst *GoFilter) anonymizeUnusedImports(file *ast.File) {
	// 1. Identify used package names
	usedPackages := containers.NewHashSet[string](128)
	ast.Inspect(file, func(n ast.Node) bool {
		// Skip imports themselves to avoid self-reference
		if _, ok := n.(*ast.ImportSpec); ok {
			return false
		}
		// Found usage "pkg.Symbol"
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				usedPackages.Add(id.Name)
			}
		}
		return true
	})

	// 2. Iterate over imports and modify unused ones
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}

		for _, spec := range gen.Specs {
			imp, ok := spec.(*ast.ImportSpec)
			if !ok {
				continue
			}

			// Determine the local name of the package
			var name string
			if imp.Name != nil {
				name = imp.Name.Name
			}

			// If it's already a side effect (_) or dot (.) import, leave it.
			if name == "_" || name == "." {
				continue
			}

			// If implied name, calculate it from path (e.g. "net/http" -> "http")
			if name == "" {
				val := imp.Path.Value
				// Unquote safe because parser validates it
				if u, err := strconv.Unquote(val); err == nil {
					val = u
				}
				name = path.Base(val)
			}

			// If not used, force it to be a side effect import
			if !usedPackages.Has(name) {
				imp.Name = &ast.Ident{Name: "_"}
			}
		}
	}
}

func (inst *GoFilter) isExportedType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.IsExported() || inst.isPredeclared(t.Name)
	case *ast.StarExpr:
		return inst.isExportedType(t.X)
	case *ast.SelectorExpr:
		return t.Sel.IsExported()
	case *ast.ArrayType:
		return inst.isExportedType(t.Elt)
	case *ast.MapType:
		return inst.isExportedType(t.Key) && inst.isExportedType(t.Value)
	case *ast.ChanType:
		return inst.isExportedType(t.Value)
	case *ast.Ellipsis:
		return inst.isExportedType(t.Elt)
	case *ast.ParenExpr:
		return inst.isExportedType(t.X)
	case *ast.IndexExpr:
		return inst.isExportedType(t.X) && inst.isExportedType(t.Index)
	case *ast.IndexListExpr:
		if !inst.isExportedType(t.X) {
			return false
		}
		for _, x := range t.Indices {
			if !inst.isExportedType(x) {
				return false
			}
		}
		return true
	case *ast.FuncType:
		return inst.validFieldList(t.Params) && inst.validFieldList(t.Results) && inst.validFieldList(t.TypeParams)
	case *ast.StructType:
		// Inline structs in signatures must be totally public to be kept
		if t.Fields == nil {
			return true
		}
		for _, f := range t.Fields.List {
			if len(f.Names) == 0 && !inst.isExportedType(f.Type) {
				return false
			}
			for _, n := range f.Names {
				if !n.IsExported() {
					return false
				}
			}
		}
		return true
	case *ast.InterfaceType:
		if t.Methods == nil {
			return true
		}
		for _, m := range t.Methods.List {
			if len(m.Names) > 0 && !m.Names[0].IsExported() {
				return false
			}
			if !inst.isExportedType(m.Type) {
				return false
			}
		}
		return true
	case *ast.BinaryExpr:
		// e.g. [constA + constB]byte. Check both operands.
		return inst.isExportedType(t.X) && inst.isExportedType(t.Y)

	case *ast.UnaryExpr:
		// e.g. [-constA]byte or [^constB]int.
		return inst.isExportedType(t.X)

	case *ast.BasicLit:
		// Literals (1, "foo", 3.14) are always safe/public.
		return true
	default:
		log.Info().Type("typeOfExpr", expr).Msg("found unexpected/unhandled AST node, assuming it contains code to be stubbed")
		return false
	}
}

func (inst *GoFilter) validFieldList(fields *ast.FieldList) bool {
	if fields == nil {
		return true
	}
	for _, f := range fields.List {
		if !inst.isExportedType(f.Type) {
			return false
		}
	}
	return true
}

func (inst *GoFilter) isPredeclared(name string) bool {
	return common.PredeclaredTypes.Has(name)
}

// updateBuildConstraints injects the build tag into existing directives in the AST.
// It returns true if the tag was merged into an existing directive, false if a new directive needs to be prepended.
func (inst *GoFilter) updateBuildConstraints(file *ast.File) bool {
	if inst.BuildTag == "" {
		return true // No tag needed, consider "handled"
	}

	var found bool
	var buildComment *ast.Comment
	var buildExpr constraint.Expr

	// Iterate over all comment groups (file header comments are usually here)
	for _, group := range file.Comments {
		// Filter list in-place to remove legacy +build comments
		n := 0
		for _, c := range group.List {
			// Remove legacy "// +build" lines to ensure source of truth is the new //go:build
			if constraint.IsPlusBuild(c.Text) {
				continue
			}

			// Check for modern "//go:build"
			if constraint.IsGoBuild(c.Text) {
				if !found {
					// Parse the existing expression
					if expr, err := constraint.Parse(c.Text); err == nil {
						buildExpr = expr
						buildComment = c
						found = true
					}
				}
				// Keep the comment; we will mutate buildComment.Text later
			}

			group.List[n] = c
			n++
		}
		group.List = group.List[:n]
	}

	if found {
		// Merge Logic: (ExistingExpr) && NewTag
		newTag := &constraint.TagExpr{Tag: inst.BuildTag}
		merged := &constraint.AndExpr{X: buildExpr, Y: newTag}

		// Update the comment text in the AST
		buildComment.Text = "//go:build " + merged.String()
		return true
	}

	return false
}
