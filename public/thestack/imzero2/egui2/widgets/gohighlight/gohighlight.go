// Package gohighlight performs AST-aware syntax highlighting for Go source.
//
// It pairs go/scanner for lexical baseline classification with go/parser
// for semantic refinement: identifiers are walked through the AST so that
// function declarations, function calls, type names, struct fields, and
// package qualifiers each receive a distinct category. If parsing fails,
// the lexical-only output is returned (Highlight degrades gracefully).
//
// The output is a flat slice of byte-offset spans suitable for direct
// consumption by the codeview widget's retained CodeViewJob.Section calls.
package gohighlight

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strings"
)

// CategoryE classifies a span for highlighting.
type CategoryE int

const (
	CategoryPlain       CategoryE = iota // unclassified / default
	CategoryKeyword                      // package, import, func, if, ...
	CategoryOperator                     // +, -, *, /, &&, ||, <-, ...
	CategoryPunctuation                  // ( ) [ ] { } , . ; :
	CategoryIdentifier                   // bare ident (fallback)
	CategoryPackageName                  // package decl + import qualifier
	CategoryTypeName                     // type names (decl, use, generic params)
	CategoryFuncDecl                     // function declared
	CategoryFuncCall                     // function called
	CategoryFieldName                    // struct field decl/access
	CategoryBuiltin                      // len, cap, make, new, panic, ...
	CategoryConstName                    // const decl + iota
	CategoryLabel                        // goto/break/continue label
	CategoryStringLit                    // "..." `...`
	CategoryNumberLit                    // INT, FLOAT, IMAG
	CategoryRuneLit                      // '...'
	CategoryBoolLit                      // true, false
	CategoryNilLit                       // nil
	CategoryComment                      // // ... /* ... */
	CategoryDocComment                   // doc comment attached to a decl
	CategoryImportPath                   // string literal inside ImportSpec
	CategoryBuildTag                     // //go:build, //go:generate, ...
	CategoryWhitespace                   // gaps between tokens (spaces, tabs, newlines)
)

// Span represents a highlighted region of input source.
type Span struct {
	Start    int32
	Stop     int32
	Text     string
	Category CategoryE
}

// Highlight performs AST-aware semantic highlighting on Go source.
// It first lexes the input for baseline token classification, then parses
// and walks the AST to refine identifier spans into semantic categories
// (function declarations vs calls, type names, struct fields, etc.).
//
// If parsing fails or input is not a complete Go file, the spans returned
// reflect lexical-only classification (a graceful degradation).
func Highlight(src string) (spans []Span) {
	spans = lexHighlight(src)

	fset := token.NewFileSet()
	file, _ := parser.ParseFile(fset, "", src,
		parser.ParseComments|parser.SkipObjectResolution|parser.AllErrors)
	if file == nil {
		return
	}

	h := newHighlighter(fset, spans)
	h.refine(file)
	spans = h.spans
	return
}

// --- Phase 1: Lexical highlighting ---

func lexHighlight(src string) (spans []Span) {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))

	var s scanner.Scanner
	s.Init(file, []byte(src), nil, scanner.ScanComments)

	spans = make([]Span, 0, 64)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		// Auto-inserted semicolons (from newlines) have lit == "\n" and
		// don't correspond to source bytes — skip them.
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		offset := file.Offset(pos)
		text := lit
		if text == "" {
			text = tok.String()
		}
		spans = append(spans, Span{
			Start:    int32(offset),
			Stop:     int32(offset + len(text)),
			Text:     text,
			Category: classifyToken(tok, text),
		})
	}
	spans = fillWhitespaceGaps(spans, src)
	return
}

// fillWhitespaceGaps inserts synthetic CategoryWhitespace spans between
// adjacent token spans (and at the head/tail of input) so that every byte
// of src is covered by exactly one span. Required by the Rust-side
// LayoutJob builder, which only renders bytes inside a LayoutSection —
// any uncovered byte is dropped at layout time, eating spaces/newlines
// between tokens. (SQL avoids this because its ANTLR lexer emits explicit
// whitespace tokens; go/scanner does not.)
func fillWhitespaceGaps(spans []Span, src string) (out []Span) {
	if len(src) == 0 {
		return spans
	}
	out = make([]Span, 0, len(spans)*2+1)
	cursor := int32(0)
	for _, s := range spans {
		if s.Start > cursor {
			out = append(out, Span{
				Start:    cursor,
				Stop:     s.Start,
				Text:     src[cursor:s.Start],
				Category: CategoryWhitespace,
			})
		}
		out = append(out, s)
		cursor = s.Stop
	}
	if int(cursor) < len(src) {
		out = append(out, Span{
			Start:    cursor,
			Stop:     int32(len(src)),
			Text:     src[cursor:],
			Category: CategoryWhitespace,
		})
	}
	return
}

func classifyToken(tok token.Token, text string) (cat CategoryE) {
	switch {
	case tok == token.IDENT:
		cat = CategoryIdentifier
	case tok == token.INT, tok == token.FLOAT, tok == token.IMAG:
		cat = CategoryNumberLit
	case tok == token.STRING:
		cat = CategoryStringLit
	case tok == token.CHAR:
		cat = CategoryRuneLit
	case tok == token.COMMENT:
		if strings.HasPrefix(text, "//go:") {
			cat = CategoryBuildTag
		} else {
			cat = CategoryComment
		}
	case tok.IsKeyword():
		cat = CategoryKeyword
	case tok.IsOperator():
		switch tok {
		case token.LPAREN, token.RPAREN,
			token.LBRACK, token.RBRACK,
			token.LBRACE, token.RBRACE,
			token.COMMA, token.PERIOD,
			token.SEMICOLON, token.COLON:
			cat = CategoryPunctuation
		default:
			cat = CategoryOperator
		}
	default:
		cat = CategoryPlain
	}
	return
}

// --- Phase 2: AST refinement ---

type highlighter struct {
	fset            *token.FileSet
	spans           []Span
	offsetToSpanIdx map[int32]int
}

func newHighlighter(fset *token.FileSet, spans []Span) (inst *highlighter) {
	inst = &highlighter{
		fset:            fset,
		spans:           spans,
		offsetToSpanIdx: make(map[int32]int, len(spans)),
	}
	for i := range spans {
		inst.offsetToSpanIdx[spans[i].Start] = i
	}
	return
}

func (inst *highlighter) setCat(pos token.Pos, cat CategoryE) {
	offset := int32(inst.fset.Position(pos).Offset)
	idx, ok := inst.offsetToSpanIdx[offset]
	if !ok {
		return
	}
	inst.spans[idx].Category = cat
}

func (inst *highlighter) markDoc(group *ast.CommentGroup) {
	if group == nil {
		return
	}
	for _, c := range group.List {
		offset := int32(inst.fset.Position(c.Slash).Offset)
		idx, ok := inst.offsetToSpanIdx[offset]
		if !ok {
			continue
		}
		// Build tags are more specific — preserve them.
		if inst.spans[idx].Category == CategoryBuildTag {
			continue
		}
		inst.spans[idx].Category = CategoryDocComment
	}
}

// markType marks every Ident in a type expression as TypeName.
// Handles all type expression shapes including Go 1.18+ type constraints
// (`~T`, `T | U`).
func (inst *highlighter) markType(expr ast.Expr) {
	if expr == nil {
		return
	}
	switch n := expr.(type) {
	case *ast.Ident:
		inst.setCat(n.NamePos, CategoryTypeName)
	case *ast.SelectorExpr:
		if x, ok := n.X.(*ast.Ident); ok {
			inst.setCat(x.NamePos, CategoryPackageName)
		} else {
			inst.markType(n.X)
		}
		inst.setCat(n.Sel.NamePos, CategoryTypeName)
	case *ast.StarExpr:
		inst.markType(n.X)
	case *ast.ArrayType:
		if n.Len != nil {
			inst.markExpr(n.Len)
		}
		inst.markType(n.Elt)
	case *ast.MapType:
		inst.markType(n.Key)
		inst.markType(n.Value)
	case *ast.ChanType:
		inst.markType(n.Value)
	case *ast.FuncType:
		inst.markFieldList(n.Params, false)
		inst.markFieldList(n.Results, false)
	case *ast.InterfaceType:
		inst.markInterfaceMethods(n.Methods)
	case *ast.StructType:
		inst.markFieldList(n.Fields, true)
	case *ast.IndexExpr:
		inst.markType(n.X)
		inst.markType(n.Index)
	case *ast.IndexListExpr:
		inst.markType(n.X)
		for _, idx := range n.Indices {
			inst.markType(idx)
		}
	case *ast.Ellipsis:
		inst.markType(n.Elt)
	case *ast.ParenExpr:
		inst.markType(n.X)
	case *ast.UnaryExpr:
		// Type-set element: ~T
		inst.markType(n.X)
	case *ast.BinaryExpr:
		// Type-set element: T | U
		inst.markType(n.X)
		inst.markType(n.Y)
	}
}

// markFieldList walks a struct/interface/func field list. When asStruct
// is true, field names become FieldName; otherwise (function params/
// results) names retain their default Identifier category.
func (inst *highlighter) markFieldList(fl *ast.FieldList, asStruct bool) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		inst.markDoc(f.Doc)
		if asStruct {
			for _, name := range f.Names {
				inst.setCat(name.NamePos, CategoryFieldName)
			}
		}
		inst.markType(f.Type)
	}
}

// markInterfaceMethods walks an interface type's method list. Method names
// become FuncDecl; embedded type names and type-set constraint elements
// are marked as TypeName.
func (inst *highlighter) markInterfaceMethods(fl *ast.FieldList) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		inst.markDoc(f.Doc)
		for _, name := range f.Names {
			inst.setCat(name.NamePos, CategoryFuncDecl)
		}
		inst.markType(f.Type)
	}
}

// markExpr handles non-type expressions appearing in function bodies and
// const/var initializers.
func (inst *highlighter) markExpr(expr ast.Expr) {
	if expr == nil {
		return
	}
	switch n := expr.(type) {
	case *ast.CallExpr:
		inst.markCallFun(n.Fun)
		for _, arg := range n.Args {
			inst.markExpr(arg)
		}
	case *ast.SelectorExpr:
		// Outside a call site, .Sel is a field/method reference.
		inst.setCat(n.Sel.NamePos, CategoryFieldName)
		inst.markExpr(n.X)
	case *ast.IndexExpr:
		inst.markExpr(n.X)
		inst.markExpr(n.Index)
	case *ast.IndexListExpr:
		inst.markExpr(n.X)
		for _, idx := range n.Indices {
			inst.markExpr(idx)
		}
	case *ast.SliceExpr:
		inst.markExpr(n.X)
		inst.markExpr(n.Low)
		inst.markExpr(n.High)
		inst.markExpr(n.Max)
	case *ast.ParenExpr:
		inst.markExpr(n.X)
	case *ast.UnaryExpr:
		inst.markExpr(n.X)
	case *ast.BinaryExpr:
		inst.markExpr(n.X)
		inst.markExpr(n.Y)
	case *ast.StarExpr:
		inst.markExpr(n.X)
	case *ast.TypeAssertExpr:
		inst.markExpr(n.X)
		if n.Type != nil {
			inst.markType(n.Type)
		}
	case *ast.CompositeLit:
		if n.Type != nil {
			inst.markType(n.Type)
		}
		for _, el := range n.Elts {
			inst.markExpr(el)
		}
	case *ast.KeyValueExpr:
		// Inside struct/map composite literals; for struct literals .Key is
		// an ident → FieldName.
		if k, ok := n.Key.(*ast.Ident); ok {
			inst.setCat(k.NamePos, CategoryFieldName)
		} else {
			inst.markExpr(n.Key)
		}
		inst.markExpr(n.Value)
	case *ast.FuncLit:
		if n.Type != nil {
			inst.markFieldList(n.Type.Params, false)
			inst.markFieldList(n.Type.Results, false)
		}
		if n.Body != nil {
			inst.markStmt(n.Body)
		}
	case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType,
		*ast.InterfaceType, *ast.StructType:
		// Bare type expression appearing in expression context (e.g. `make([]int, 0)`).
		inst.markType(n)
	}
	// *ast.Ident, *ast.BasicLit, *ast.Ellipsis: baseline classification stands.
}

// markCallFun marks the function reference of a call expression. The leaf
// "called" identifier becomes FuncCall (or Builtin / TypeName when the
// name is predeclared); other parts of the Fun expression are walked
// normally so .Sel chains receive FieldName.
func (inst *highlighter) markCallFun(fun ast.Expr) {
	if fun == nil {
		return
	}
	switch f := fun.(type) {
	case *ast.Ident:
		cat := CategoryFuncCall
		switch {
		case isBuiltin(f.Name):
			cat = CategoryBuiltin
		case isPredeclaredType(f.Name):
			// Predeclared-type conversion: int(x), string(b), ...
			cat = CategoryTypeName
		}
		inst.setCat(f.NamePos, cat)
	case *ast.SelectorExpr:
		inst.setCat(f.Sel.NamePos, CategoryFuncCall)
		inst.markExpr(f.X)
	case *ast.IndexExpr:
		inst.markCallFun(f.X)
		inst.markExpr(f.Index)
	case *ast.IndexListExpr:
		inst.markCallFun(f.X)
		for _, idx := range f.Indices {
			inst.markExpr(idx)
		}
	case *ast.ParenExpr:
		inst.markCallFun(f.X)
	case *ast.FuncLit:
		inst.markExpr(f)
	default:
		// Arbitrary expression returning a callable — walk normally.
		inst.markExpr(fun)
	}
}

// markStmt walks function-body statements, dispatching to markExpr / markType
// / markGenDecl as required by each statement shape.
func (inst *highlighter) markStmt(stmt ast.Stmt) {
	if stmt == nil {
		return
	}
	switch n := stmt.(type) {
	case *ast.BlockStmt:
		for _, s := range n.List {
			inst.markStmt(s)
		}
	case *ast.DeclStmt:
		if d, ok := n.Decl.(*ast.GenDecl); ok {
			inst.markGenDecl(d)
		}
	case *ast.AssignStmt:
		for _, e := range n.Lhs {
			inst.markExpr(e)
		}
		for _, e := range n.Rhs {
			inst.markExpr(e)
		}
	case *ast.IncDecStmt:
		inst.markExpr(n.X)
	case *ast.ExprStmt:
		inst.markExpr(n.X)
	case *ast.GoStmt:
		inst.markExpr(n.Call)
	case *ast.DeferStmt:
		inst.markExpr(n.Call)
	case *ast.ReturnStmt:
		for _, r := range n.Results {
			inst.markExpr(r)
		}
	case *ast.BranchStmt:
		if n.Label != nil {
			inst.setCat(n.Label.NamePos, CategoryLabel)
		}
	case *ast.LabeledStmt:
		inst.setCat(n.Label.NamePos, CategoryLabel)
		inst.markStmt(n.Stmt)
	case *ast.IfStmt:
		inst.markStmt(n.Init)
		inst.markExpr(n.Cond)
		inst.markStmt(n.Body)
		inst.markStmt(n.Else)
	case *ast.ForStmt:
		inst.markStmt(n.Init)
		inst.markExpr(n.Cond)
		inst.markStmt(n.Post)
		inst.markStmt(n.Body)
	case *ast.RangeStmt:
		inst.markExpr(n.Key)
		inst.markExpr(n.Value)
		inst.markExpr(n.X)
		inst.markStmt(n.Body)
	case *ast.SwitchStmt:
		inst.markStmt(n.Init)
		inst.markExpr(n.Tag)
		inst.markStmt(n.Body)
	case *ast.TypeSwitchStmt:
		inst.markStmt(n.Init)
		inst.markStmt(n.Assign)
		inst.markStmt(n.Body)
	case *ast.CaseClause:
		// CaseClause expressions are regular exprs in Switch and type exprs
		// in TypeSwitch. markExpr already routes bare type-expr nodes into
		// markType, so this works for both.
		for _, e := range n.List {
			inst.markExpr(e)
		}
		for _, s := range n.Body {
			inst.markStmt(s)
		}
	case *ast.SelectStmt:
		inst.markStmt(n.Body)
	case *ast.CommClause:
		inst.markStmt(n.Comm)
		for _, s := range n.Body {
			inst.markStmt(s)
		}
	case *ast.SendStmt:
		inst.markExpr(n.Chan)
		inst.markExpr(n.Value)
	}
}

// markGenDecl handles var/const/type/import declarations, both top-level
// and inside function bodies (via DeclStmt).
func (inst *highlighter) markGenDecl(d *ast.GenDecl) {
	inst.markDoc(d.Doc)
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.ImportSpec:
			inst.markDoc(s.Doc)
			if s.Name != nil {
				inst.setCat(s.Name.NamePos, CategoryPackageName)
			}
			if s.Path != nil {
				inst.setCat(s.Path.ValuePos, CategoryImportPath)
			}
		case *ast.TypeSpec:
			inst.markDoc(s.Doc)
			inst.setCat(s.Name.NamePos, CategoryTypeName)
			if s.TypeParams != nil {
				for _, tp := range s.TypeParams.List {
					for _, name := range tp.Names {
						inst.setCat(name.NamePos, CategoryTypeName)
					}
					inst.markType(tp.Type)
				}
			}
			inst.markType(s.Type)
		case *ast.ValueSpec:
			inst.markDoc(s.Doc)
			if d.Tok == token.CONST {
				for _, name := range s.Names {
					inst.setCat(name.NamePos, CategoryConstName)
				}
			}
			// VAR: names retain default Identifier classification.
			if s.Type != nil {
				inst.markType(s.Type)
			}
			for _, val := range s.Values {
				inst.markExpr(val)
			}
		}
	}
}

func (inst *highlighter) refine(file *ast.File) {
	inst.markDoc(file.Doc)
	if file.Name != nil {
		inst.setCat(file.Name.NamePos, CategoryPackageName)
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			inst.markGenDecl(d)
		case *ast.FuncDecl:
			inst.markDoc(d.Doc)
			if d.Recv != nil {
				for _, recv := range d.Recv.List {
					inst.markType(recv.Type)
				}
			}
			if d.Name != nil {
				inst.setCat(d.Name.NamePos, CategoryFuncDecl)
			}
			if d.Type != nil {
				if d.Type.TypeParams != nil {
					for _, tp := range d.Type.TypeParams.List {
						for _, name := range tp.Names {
							inst.setCat(name.NamePos, CategoryTypeName)
						}
						inst.markType(tp.Type)
					}
				}
				inst.markFieldList(d.Type.Params, false)
				inst.markFieldList(d.Type.Results, false)
			}
			if d.Body != nil {
				inst.markStmt(d.Body)
			}
		}
	}

	// Comments not attached to any decl (orphan comments) — refine in-place.
	// File.Comments contains every comment group; doc-comments are also
	// referenced from file.Doc / *.Doc fields, so we skip those by checking
	// whether the comment span has already been promoted to DocComment.
	// (Plain // comments stay CategoryComment.)

	// Final pass: refine remaining bare identifiers via predeclared-name
	// recognition. This catches predeclared types, builtins, and the
	// predeclared constants that were never reached during AST walking
	// (e.g. via partial parses).
	for i := range inst.spans {
		s := &inst.spans[i]
		if s.Category != CategoryIdentifier {
			continue
		}
		switch {
		case isPredeclaredType(s.Text):
			s.Category = CategoryTypeName
		case isBuiltin(s.Text):
			s.Category = CategoryBuiltin
		case s.Text == "true" || s.Text == "false":
			s.Category = CategoryBoolLit
		case s.Text == "nil":
			s.Category = CategoryNilLit
		case s.Text == "iota":
			s.Category = CategoryConstName
		}
	}
}

// --- Predeclared identifier sets (Go universe block) ---

func isPredeclaredType(name string) (ok bool) {
	switch name {
	case "any", "bool", "byte", "comparable",
		"complex64", "complex128", "error",
		"float32", "float64",
		"int", "int8", "int16", "int32", "int64",
		"rune", "string",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
		ok = true
	}
	return
}

func isBuiltin(name string) (ok bool) {
	switch name {
	case "append", "cap", "clear", "close", "complex",
		"copy", "delete", "imag", "len", "make",
		"max", "min", "new", "panic", "print",
		"println", "real", "recover":
		ok = true
	}
	return
}

// --- Line-range slicing ---

// LineCount returns the number of lines in src. An empty input has zero
// lines; otherwise the count is one more than the number of '\n' bytes
// that aren't at the very end (a trailing newline doesn't introduce an
// empty phantom line).
func LineCount(src string) (n int32) {
	if len(src) == 0 {
		return
	}
	n = 1
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' && i+1 < len(src) {
			n++
		}
	}
	return
}

// computeLineStarts returns the byte offset at which each line begins.
// Line N (1-based) starts at out[N-1]. A trailing '\n' does not add an
// extra entry — see LineCount for the matching counting convention.
func computeLineStarts(src string) (out []int32) {
	if len(src) == 0 {
		return
	}
	out = make([]int32, 1, 64)
	out[0] = 0
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' && i+1 < len(src) {
			out = append(out, int32(i+1))
		}
	}
	return
}

// HighlightLines highlights src and returns the byte slice and spans
// covering 1-based lines [firstLine, lastLine] (inclusive on both ends).
//
// The full source is parsed so that AST refinement still applies — spans
// crossing the window boundaries are clipped, not dropped. Returned span
// Start/Stop offsets index into slice (not into src), and slice's first
// byte corresponds to firstLine's first byte in src.
//
// firstLine is clamped to [1, totalLines]; lastLine is clamped to
// [firstLine, totalLines]. An empty (slice, spans) result indicates the
// requested window has no content (firstLine past EOF).
func HighlightLines(src string, firstLine int32, lastLine int32) (slice string, spans []Span) {
	starts := computeLineStarts(src)
	totalLines := int32(len(starts))
	if totalLines == 0 {
		return
	}
	if firstLine < 1 {
		firstLine = 1
	}
	if firstLine > totalLines {
		return
	}
	if lastLine > totalLines {
		lastLine = totalLines
	}
	if lastLine < firstLine {
		return
	}

	byteStart := starts[firstLine-1]
	var byteEnd int32
	if lastLine >= totalLines {
		byteEnd = int32(len(src))
	} else {
		byteEnd = starts[lastLine]
	}
	if byteStart >= byteEnd {
		return
	}

	slice = src[byteStart:byteEnd]
	full := Highlight(src)
	spans = make([]Span, 0, len(full))
	for _, s := range full {
		if s.Stop <= byteStart || s.Start >= byteEnd {
			continue
		}
		start := s.Start
		if start < byteStart {
			start = byteStart
		}
		stop := s.Stop
		if stop > byteEnd {
			stop = byteEnd
		}
		spans = append(spans, Span{
			Start:    start - byteStart,
			Stop:     stop - byteStart,
			Text:     src[start:stop],
			Category: s.Category,
		})
	}
	return
}
