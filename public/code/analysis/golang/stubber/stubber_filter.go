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

// newStubBlock returns a fresh `panic("stub")` body.
// Each caller must get its own AST subtree — sharing a single node across
// multiple function bodies aliases token positions and comment slots, which
// go/format does not guarantee to handle.
func newStubBlock() *ast.BlockStmt {
	return &ast.BlockStmt{
		List: []ast.Stmt{
			&ast.ExprStmt{
				X: &ast.CallExpr{
					Fun:  &ast.Ident{Name: "panic"},
					Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"stub"`}},
				},
			},
		},
	}
}

// GoFilter processes Go source code to filter out private elements and public function bodies.
type GoFilter struct {
	BuildTag      string
	DeletePrivate bool

	// Per-file transient state; reset at the start of each Process call.
	// Not safe for concurrent Process invocations on the same instance.
	reachablePrivates *containers.HashSet[string]
	docsToStrip       map[*ast.CommentGroup]struct{}
}

func NewGoFilter(buildTag string, deletePrivate bool) *GoFilter {
	return &GoFilter{
		BuildTag:      buildTag,
		DeletePrivate: deletePrivate,
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

	// 2a. Initialize per-file state and, if --deletePrivate is on,
	// compute the set of top-level private names kept for reachability reasons.
	inst.reachablePrivates = nil
	inst.docsToStrip = nil
	if inst.DeletePrivate {
		inst.docsToStrip = make(map[*ast.CommentGroup]struct{}, 8)
		privateNames := inst.collectTopLevelPrivateNames(file)
		reachable := inst.computeReachablePrivates(file, privateNames)
		inst.reachablePrivates = reachable
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

	// 3a. Purge doc comment groups attached to decls/specs/fields we removed,
	// so they do not linger in file.Comments as orphan comments.
	if inst.DeletePrivate && len(inst.docsToStrip) > 0 {
		inst.stripQueuedDocs(file)
	}

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

// isPrivateReachable reports whether a top-level private identifier must be
// kept because it is referenced from surviving public code (transitively).
// Always returns false when DeletePrivate is off, preserving legacy behavior.
func (inst *GoFilter) isPrivateReachable(name string) bool {
	return inst.reachablePrivates != nil && inst.reachablePrivates.Has(name)
}

// queueDoc marks a doc comment group for removal from file.Comments. A nil
// doc or an inactive DeletePrivate flag are both no-ops.
func (inst *GoFilter) queueDoc(doc *ast.CommentGroup) {
	if doc == nil || inst.docsToStrip == nil {
		return
	}
	inst.docsToStrip[doc] = struct{}{}
}

func (inst *GoFilter) processFunc(fn *ast.FuncDecl) bool {
	isPrivate := !fn.Name.IsExported() && fn.Name.Name != "main" && fn.Name.Name != "init"
	if isPrivate {
		// Methods (with a receiver) are never reachability-kept: their fate
		// follows the receiver type, and the current policy strips them.
		if fn.Recv != nil || !inst.isPrivateReachable(fn.Name.Name) {
			inst.queueDoc(fn.Doc)
			return false
		}
	}
	// Validate Signature (remove if it relies on private types)
	if !inst.validFieldList(fn.Recv) ||
		!inst.validFieldList(fn.Type.Params) ||
		!inst.validFieldList(fn.Type.Results) ||
		!inst.validFieldList(fn.Type.TypeParams) {
		inst.queueDoc(fn.Doc)
		return false
	}

	if fn.Body != nil {
		fn.Body = newStubBlock()
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
	if len(gen.Specs) == 0 {
		// Entire GenDecl block is gone — its leading doc comment would orphan.
		inst.queueDoc(gen.Doc)
		return false
	}
	return true
}

func (inst *GoFilter) processSpec(spec ast.Spec) bool {
	switch s := spec.(type) {
	case *ast.ValueSpec:
		return inst.processValueSpec(s)
	case *ast.TypeSpec:
		if !s.Name.IsExported() && !inst.isPrivateReachable(s.Name.Name) {
			inst.queueDoc(s.Doc)
			return false
		}
		if !inst.validFieldList(s.TypeParams) {
			inst.queueDoc(s.Doc)
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
			inst.queueDoc(s.Doc)
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
		inst.queueDoc(s.Doc)
		return false
	}

	var keptNames []*ast.Ident
	var keptValues []ast.Expr

	for i, name := range s.Names {
		if name.IsExported() || inst.isPrivateReachable(name.Name) {
			keptNames = append(keptNames, name)
			if i < len(s.Values) {
				keptValues = append(keptValues, inst.sanitizeExpr(s.Values[i]))
			}
		}
	}

	s.Names = keptNames
	s.Values = keptValues
	if len(s.Names) == 0 {
		inst.queueDoc(s.Doc)
		return false
	}
	return true
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
			} else {
				inst.queueDoc(field.Doc)
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
		} else {
			inst.queueDoc(field.Doc)
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
			} else {
				inst.queueDoc(field.Doc)
			}
			continue
		}
		// Explicit Method
		if field.Names[0].IsExported() && inst.isExportedType(field.Type) {
			keptMethods = append(keptMethods, field)
		} else {
			inst.queueDoc(field.Doc)
		}
	}
	it.Methods.List = keptMethods
}

func (inst *GoFilter) sanitizeExpr(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case *ast.FuncLit:
		e.Body = newStubBlock()
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

// collectTopLevelPrivateNames returns the set of top-level private identifier
// names defined in file: non-method function names, type names, and var/const
// names. Methods (FuncDecl with a receiver) are excluded — their fate follows
// the receiver type, not package-level reachability.
func (inst *GoFilter) collectTopLevelPrivateNames(file *ast.File) *containers.HashSet[string] {
	names := containers.NewHashSet[string](16)
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil {
				continue
			}
			if !d.Name.IsExported() && d.Name.Name != "main" && d.Name.Name != "init" {
				names.Add(d.Name.Name)
			}
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !s.Name.IsExported() {
						names.Add(s.Name.Name)
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if !n.IsExported() {
							names.Add(n.Name)
						}
					}
				}
			}
		}
	}
	return names
}

// computeReachablePrivates returns the subset of privateNames that must be
// kept to avoid dangling references in the filtered output. Seeding is
// restricted to decls that survive the public filter; the frontier then
// expands transitively through kept private decls' own signatures and
// initializers until a fixed point.
//
// Function and FuncLit bodies are deliberately skipped during traversal
// because they will be replaced with panic("stub"), so identifiers inside
// them never reach the output.
func (inst *GoFilter) computeReachablePrivates(file *ast.File, privateNames *containers.HashSet[string]) *containers.HashSet[string] {
	reachable := containers.NewHashSet[string](8)
	if privateNames.IsEmpty() {
		return reachable
	}

	// Index private top-level decls/specs by name so we can expand the frontier.
	privateNodes := make(map[string]ast.Node, privateNames.Size())
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && privateNames.Has(d.Name.Name) {
				privateNodes[d.Name.Name] = d
			}
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if privateNames.Has(s.Name.Name) {
						privateNodes[s.Name.Name] = s
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if privateNames.Has(n.Name) {
							privateNodes[n.Name] = s
						}
					}
				}
			}
		}
	}

	// Seed: references from surviving public decls (i.e. decls the public
	// filter would keep without --deletePrivate). We conservatively include
	// every non-private top-level decl here; private decls are added only if
	// reachable.
	seed := containers.NewHashSet[string](16)
	for _, decl := range file.Decls {
		if inst.isPrivateTopLevelDecl(decl, privateNames) {
			continue
		}
		inst.collectDeclRefs(decl, seed)
	}

	frontier := make([]string, 0, 8)
	for name := range privateNodes {
		if seed.Has(name) {
			reachable.Add(name)
			frontier = append(frontier, name)
		}
	}

	for len(frontier) > 0 {
		name := frontier[len(frontier)-1]
		frontier = frontier[:len(frontier)-1]
		node, ok := privateNodes[name]
		if !ok {
			continue
		}
		refs := containers.NewHashSet[string](8)
		switch n := node.(type) {
		case *ast.FuncDecl:
			inst.collectDeclRefs(n, refs)
		case *ast.TypeSpec:
			if n.TypeParams != nil {
				inst.collectNodeRefs(n.TypeParams, refs)
			}
			if n.Type != nil {
				inst.collectNodeRefs(n.Type, refs)
			}
		case *ast.ValueSpec:
			if n.Type != nil {
				inst.collectNodeRefs(n.Type, refs)
			}
			for _, v := range n.Values {
				if v != nil {
					inst.collectNodeRefs(v, refs)
				}
			}
		}
		for refName := range privateNodes {
			if reachable.Has(refName) {
				continue
			}
			if refs.Has(refName) {
				reachable.Add(refName)
				frontier = append(frontier, refName)
			}
		}
	}

	return reachable
}

// isPrivateTopLevelDecl reports whether decl declares nothing but top-level
// private names (so it would not seed reachability).
func (inst *GoFilter) isPrivateTopLevelDecl(decl ast.Decl, privateNames *containers.HashSet[string]) bool {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Recv == nil && privateNames.Has(d.Name.Name)
	case *ast.GenDecl:
		if d.Tok == token.IMPORT {
			return false
		}
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if !privateNames.Has(s.Name.Name) {
					return false
				}
			case *ast.ValueSpec:
				for _, n := range s.Names {
					if !privateNames.Has(n.Name) {
						return false
					}
				}
			default:
				return false
			}
		}
		return len(d.Specs) > 0
	}
	return false
}

// collectDeclRefs walks the reference-bearing parts of a top-level decl
// (receiver, signature, type definition, initializer) and adds every
// encountered identifier to out. Function bodies are skipped because the
// filter replaces them with panic("stub").
func (inst *GoFilter) collectDeclRefs(decl ast.Decl, out *containers.HashSet[string]) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil {
			inst.collectNodeRefs(d.Recv, out)
		}
		if d.Type != nil {
			inst.collectNodeRefs(d.Type, out)
		}
	case *ast.GenDecl:
		if d.Tok == token.IMPORT {
			return
		}
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if s.TypeParams != nil {
					inst.collectNodeRefs(s.TypeParams, out)
				}
				if s.Type != nil {
					inst.collectNodeRefs(s.Type, out)
				}
			case *ast.ValueSpec:
				if s.Type != nil {
					inst.collectNodeRefs(s.Type, out)
				}
				for _, v := range s.Values {
					if v != nil {
						inst.collectNodeRefs(v, out)
					}
				}
			}
		}
	}
}

// collectNodeRefs fills out with identifier names referenced from node.
// FuncDecl/FuncLit bodies are skipped (they will be stubbed). For selector
// expressions pkg.Sym only the base pkg is followed — Sym names a member of
// an external namespace and cannot refer to a local private decl.
func (inst *GoFilter) collectNodeRefs(node ast.Node, out *containers.HashSet[string]) {
	if node == nil {
		return
	}
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			if x.Recv != nil {
				inst.collectNodeRefs(x.Recv, out)
			}
			if x.Type != nil {
				inst.collectNodeRefs(x.Type, out)
			}
			return false
		case *ast.FuncLit:
			if x.Type != nil {
				inst.collectNodeRefs(x.Type, out)
			}
			return false
		case *ast.SelectorExpr:
			if x.X != nil {
				inst.collectNodeRefs(x.X, out)
			}
			return false
		case *ast.Ident:
			out.Add(x.Name)
			return false
		}
		return true
	})
}

// stripQueuedDocs removes comment groups queued during filtering from
// file.Comments so they don't linger as orphan comments.
func (inst *GoFilter) stripQueuedDocs(file *ast.File) {
	n := 0
	for _, g := range file.Comments {
		if _, drop := inst.docsToStrip[g]; drop {
			continue
		}
		file.Comments[n] = g
		n++
	}
	file.Comments = file.Comments[:n]
}

func (inst *GoFilter) isExportedType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.IsExported() || inst.isPredeclared(t.Name) || inst.isPrivateReachable(t.Name)
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
