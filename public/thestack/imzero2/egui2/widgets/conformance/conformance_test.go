package conformance

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

const bindingsImportPath = "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"

// exempt names the packages under widgets/ that are not widgets — value
// libraries, tokenizers, layout engines, painter helpers, gates and test
// harnesses — and so are outside ADR-0267's contract (survey §6). Keys are
// paths relative to widgets/.
var exempt = map[string]string{
	"axisruler":                 "painter helper",
	"basemap":                   "tile source resolver",
	"camera":                    "view transform library",
	"codeview":                  "retained job builders",
	"color":                     "value type",
	"colormap":                  "value library",
	"conformance":               "this package",
	"ecdfdigest":                "bridge",
	"gohighlight":               "tokenizer",
	"graphview/scenetest":       "test harness",
	"icicle":                    "layout engine",
	"icicle/view":               "painter over a layout engine",
	"imagedecode":               "decoder",
	"implot":                    "plotting lane, ADR-0149",
	"inspector":                 "tether infrastructure",
	"jsonhighlight":             "tokenizer",
	"layeredgraph":              "layout seam",
	"layeredgraph/goccyengine":  "layout engine",
	"layeredgraph/view":         "painter over a layout engine",
	"lazypane":                  "gate",
	"legend":                    "painter helper",
	"markdownhighlight":         "tokenizer",
	"pipelineview":              "layout engine",
	"pipelineview/view":         "painter over a layout engine",
	"regexhighlight":            "tokenizer",
	"sankey":                    "layout engine",
	"sankey/view":               "painter over a layout engine",
	"scctree":                   "data adapter",
	"timeline/layout":           "layout library",
	"timerangepicker":           "value types",
	"timerangepicker/evaluator": "library",
	"timerangepicker/presets":   "library",
	"timerangepicker/validator": "library",
	"treemap/layout":            "layout library",
}

// rule is one mechanical check of ADR-0267. Ids are the ADR's W-numbers.
type rule struct {
	id    string
	title string
	check func(p *pkg) (findings []string)
}

var rules = []rule{
	{"W1", "entry verbs are Render / Paint / Send, not Show, Draw or Render<Adverb>", checkW1Verbs},
	{"W2", "no exported type named Inst, Widget or Renderer", checkW2Names},
	{"W3", "Render returns one struct (Result or Events), not a bool, tuple or sequence", checkW3Returns},
	{"W4", "ids arrive as Ids + ScopeKey (Input) or (ids, scopeKey) at New; no idPrefix", checkW4Ids},
	{"W5", "child ids from PrepareStr(literal) / PrepareSeq(ordinal), not constants, concatenation or Sprintf", checkW5ChildIds},
	{"W6", "no absolute ids from strings or conversions; only MakeAbsoluteIdHighEntropy over a scope-derived id", checkW6Absolute},
	{"W7", "probe seqs from WidgetIdStack.ProbeSeq, not the stack-free c.ProbeSeq or a probeSalt", checkW7Probes},
	{"W11", "options are a struct: no With* functional options, no copy-returning setters on a value receiver", checkW11Options},
	{"W13", "interaction is returned, not delivered to On* / *Listener callbacks", checkW13Callbacks},
	{"W14", "a package that starts goroutines exports Close", checkW14Close},
}

// allowlist is the remaining migration: package → rules it still violates.
// Entries leave as the ADR-0267 phases land; a new package may not enter.
var allowlist = map[string][]string{
	// Frozen 2026-09-29 (ADR-0267 M0); M1 removed the W7 entries it closed,
	// M2 the nineteen immediate-mode packages it migrated, M3 kanban and
	// schemaview, M4 the ten semi-retained packages. Regenerate with
	// WIDGET_CONFORMANCE_DUMP=1.
	"cardgrid":             {"W5"},
	"ecdf":                 {"W14"},
	"graphview":            {"W3"},
	"leewaywidgets":        {"W4"},
	"markdown":             {"W11", "W3"},
	"portolan":             {"W4", "W5"},
	"portolan/flowoverlay": {"W1"},
	"portolan/landoverlay": {"W1"},
	"timeline":             {"W11", "W13"},
	"timescrubber":         {"W4"},
	"treemap":              {"W11", "W13", "W5", "W6"},
	"waveform":             {"W14", "W4"},
}

type pkg struct {
	rel          string
	files        []*ast.File
	bindingsName string // import name of the bindings package in this pkg, "" if not imported
	exportedFns  []*ast.FuncDecl
	types        map[string]*ast.TypeSpec
	fset         *token.FileSet
}

func loadPackages(t *testing.T, root string) (out []*pkg) {
	t.Helper()
	dirs := map[string]struct{}{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			dirs[filepath.Dir(path)] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for dir := range dirs {
		rel, _ := filepath.Rel(root, dir)
		rel = filepath.ToSlash(rel)
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, parser.ParseComments)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		for _, ap := range pkgs {
			p := &pkg{rel: rel, fset: fset, types: map[string]*ast.TypeSpec{}}
			for _, f := range ap.Files {
				p.files = append(p.files, f)
				for _, imp := range f.Imports {
					if strings.Trim(imp.Path.Value, `"`) == bindingsImportPath {
						p.bindingsName = "bindings"
						if imp.Name != nil {
							p.bindingsName = imp.Name.Name
						}
					}
				}
				for _, d := range f.Decls {
					switch d := d.(type) {
					case *ast.FuncDecl:
						if d.Name.IsExported() && (d.Recv == nil || receiverExported(d)) {
							p.exportedFns = append(p.exportedFns, d)
						}
					case *ast.GenDecl:
						for _, s := range d.Specs {
							if ts, ok := s.(*ast.TypeSpec); ok {
								p.types[ts.Name.Name] = ts
							}
						}
					}
				}
			}
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out
}

func receiverExported(d *ast.FuncDecl) bool {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return false
	}
	return ast.IsExported(receiverTypeName(d))
}

func receiverTypeName(d *ast.FuncDecl) string {
	t := d.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if ix, ok := t.(*ast.IndexExpr); ok {
		t = ix.X
	}
	if ixl, ok := t.(*ast.IndexListExpr); ok {
		t = ixl.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func receiverIsPointer(d *ast.FuncDecl) bool {
	_, ok := d.Recv.List[0].Type.(*ast.StarExpr)
	return ok
}

func (p *pkg) at(n ast.Node) string {
	pos := p.fset.Position(n.Pos())
	return fmt.Sprintf("%s:%d", filepath.Base(pos.Filename), pos.Line)
}

func fnLabel(d *ast.FuncDecl) string {
	if d.Recv != nil {
		return receiverTypeName(d) + "." + d.Name.Name
	}
	return d.Name.Name
}

// ---- W1

func checkW1Verbs(p *pkg) (out []string) {
	names := map[string]bool{}
	for _, d := range p.exportedFns {
		names[fnLabel(d)] = true
	}
	for _, d := range p.exportedFns {
		n := d.Name.Name
		switch {
		case n == "RenderInline" || n == "RenderControls" || n == "RenderReport":
			out = append(out, fmt.Sprintf("%s %s", p.at(d), fnLabel(d)))
		case n == "Draw":
			// A Draw that returns something computes a draw list; one that
			// returns nothing paints, and the verb for that is Paint.
			if d.Type.Results == nil || len(d.Type.Results.List) == 0 {
				out = append(out, fmt.Sprintf("%s %s", p.at(d), fnLabel(d)))
			}
		case strings.HasPrefix(n, "Show"):
			// Show/Hide is a house opposite pair, and ShowX(bool) is a setter
			// (W11's business). Show without either is a render verb.
			hide := strings.Replace(fnLabel(d), "Show", "Hide", 1)
			if names[hide] || singleBoolParam(d) {
				continue
			}
			out = append(out, fmt.Sprintf("%s %s (no %s)", p.at(d), fnLabel(d), hide))
		}
	}
	return
}

func singleBoolParam(d *ast.FuncDecl) bool {
	ps := d.Type.Params.List
	if len(ps) != 1 || len(ps[0].Names) > 1 {
		return false
	}
	id, ok := ps[0].Type.(*ast.Ident)
	return ok && id.Name == "bool"
}

// ---- W2

func checkW2Names(p *pkg) (out []string) {
	for _, n := range []string{"Inst", "Widget", "Renderer"} {
		if ts, ok := p.types[n]; ok {
			out = append(out, fmt.Sprintf("%s type %s", p.at(ts), n))
		}
	}
	return
}

// ---- W3

var basicNames = map[string]bool{
	"bool": true, "error": true, "string": true, "byte": true, "rune": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true, "any": true,
}

func checkW3Returns(p *pkg) (out []string) {
	for _, d := range p.exportedFns {
		if !strings.HasPrefix(d.Name.Name, "Render") {
			continue
		}
		res := d.Type.Results
		if res == nil || len(res.List) == 0 {
			continue
		}
		count := 0
		for _, f := range res.List {
			count += max(1, len(f.Names))
		}
		if count > 1 {
			out = append(out, fmt.Sprintf("%s %s returns %d values", p.at(d), fnLabel(d), count))
			continue
		}
		switch t := res.List[0].Type.(type) {
		case *ast.Ident:
			if basicNames[t.Name] {
				out = append(out, fmt.Sprintf("%s %s returns %s", p.at(d), fnLabel(d), t.Name))
			}
		case *ast.SelectorExpr:
			// a qualified struct is fine
		default:
			out = append(out, fmt.Sprintf("%s %s returns a non-struct", p.at(d), fnLabel(d)))
		}
	}
	return
}

// ---- W4

func checkW4Ids(p *pkg) (out []string) {
	// A painter helper (Paint, no Render) draws into a canvas the host owns
	// and takes no ids; its Input carries style and data only.
	if ts, ok := p.types["Input"]; ok && p.hasRender() {
		if st, ok := ts.Type.(*ast.StructType); ok {
			have := map[string]bool{}
			for _, f := range st.Fields.List {
				for _, n := range f.Names {
					have[n.Name] = true
				}
			}
			if !have["Ids"] || !have["ScopeKey"] {
				out = append(out, fmt.Sprintf("%s Input lacks Ids/ScopeKey", p.at(ts)))
			}
		}
	}
	for _, d := range p.exportedFns {
		params := d.Type.Params.List
		for _, f := range params {
			for _, n := range f.Names {
				if n.Name == "idPrefix" {
					out = append(out, fmt.Sprintf("%s %s takes idPrefix", p.at(d), fnLabel(d)))
				}
			}
		}
		if d.Recv == nil && strings.HasPrefix(d.Name.Name, "New") && len(params) > 0 && p.isStackType(params[0].Type) {
			second := ""
			if len(params[0].Names) > 1 {
				second = "string?"
			} else if len(params) > 1 {
				if id, ok := params[1].Type.(*ast.Ident); ok {
					second = id.Name
				}
			}
			if second != "string" {
				out = append(out, fmt.Sprintf("%s %s takes ids without a scope key", p.at(d), fnLabel(d)))
			}
		}
	}
	return
}

func (p *pkg) isStackType(t ast.Expr) bool {
	s, ok := t.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := s.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == p.bindingsName && sel.Sel.Name == "WidgetIdStack"
}

// ---- W5

func checkW5ChildIds(p *pkg) (out []string) {
	p.eachCall(func(call *ast.CallExpr, sel *ast.SelectorExpr) {
		if len(call.Args) == 0 {
			return
		}
		arg := call.Args[0]
		switch sel.Sel.Name {
		case "PrepareSeq":
			if _, ok := arg.(*ast.BasicLit); ok {
				out = append(out, fmt.Sprintf("%s PrepareSeq(constant)", p.at(call)))
			}
		case "PrepareStr":
			switch a := arg.(type) {
			case *ast.BinaryExpr:
				out = append(out, fmt.Sprintf("%s PrepareStr(a + b)", p.at(call)))
			case *ast.CallExpr:
				if s, ok := a.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Sprintf" {
					out = append(out, fmt.Sprintf("%s PrepareStr(fmt.Sprintf)", p.at(call)))
				}
			}
		}
	})
	return
}

// ---- W6

func checkW6Absolute(p *pkg) (out []string) {
	p.eachCall(func(call *ast.CallExpr, sel *ast.SelectorExpr) {
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Name != p.bindingsName {
			return
		}
		switch sel.Sel.Name {
		case "MakeAbsoluteIdStr", "MakeAbsoluteIdSeq", "AbsoluteWidgetId":
			out = append(out, fmt.Sprintf("%s %s.%s", p.at(call), x.Name, sel.Sel.Name))
		}
	})
	return
}

// ---- W7

func checkW7Probes(p *pkg) (out []string) {
	p.eachCall(func(call *ast.CallExpr, sel *ast.SelectorExpr) {
		if x, ok := sel.X.(*ast.Ident); ok && x.Name == p.bindingsName && sel.Sel.Name == "ProbeSeq" {
			out = append(out, fmt.Sprintf("%s stack-free %s.ProbeSeq", p.at(call), x.Name))
		}
	})
	for _, ts := range p.types {
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			continue
		}
		for _, f := range st.Fields.List {
			for _, n := range f.Names {
				if strings.Contains(strings.ToLower(n.Name), "probesalt") {
					out = append(out, fmt.Sprintf("%s field %s.%s", p.at(f), ts.Name.Name, n.Name))
				}
			}
		}
	}
	return
}

// ---- W11

func checkW11Options(p *pkg) (out []string) {
	renders := p.renderingTypes()
	for _, d := range p.exportedFns {
		res := d.Type.Results
		if res == nil || len(res.List) != 1 {
			continue
		}
		rt := res.List[0].Type
		if ix, ok := rt.(*ast.IndexExpr); ok {
			rt = ix.X
		}
		rid, isIdent := rt.(*ast.Ident)
		if !isIdent {
			continue
		}
		if d.Recv == nil && strings.HasPrefix(d.Name.Name, "With") && (rid.Name == "Option" || rid.Name == "RenderOpt") {
			out = append(out, fmt.Sprintf("%s %s functional option", p.at(d), fnLabel(d)))
			continue
		}
		// A copy-returning setter is a value-receiver method returning its own
		// type on a type that renders. Fluids (the F shape) are built this way
		// on purpose; value arithmetic (Point.Add) returns its type too, but
		// such types do not render.
		if d.Recv != nil && !receiverIsPointer(d) && rid.Name == receiverTypeName(d) && len(d.Type.Params.List) > 0 &&
			!strings.HasSuffix(rid.Name, "Fluid") && renders[rid.Name] {
			out = append(out, fmt.Sprintf("%s %s copy-returning setter", p.at(d), fnLabel(d)))
		}
	}
	return
}

// hasRender reports whether p exports a Render* function or method — what
// tells an IM/SR widget from a painter helper or a fluid.
func (p *pkg) hasRender() bool {
	for _, d := range p.exportedFns {
		if strings.HasPrefix(d.Name.Name, "Render") {
			return true
		}
	}
	return false
}

// renderingTypes names the receiver types in p with an exported Render* or
// Paint* method.
func (p *pkg) renderingTypes() map[string]bool {
	out := map[string]bool{}
	for _, d := range p.exportedFns {
		if d.Recv != nil && (strings.HasPrefix(d.Name.Name, "Render") || strings.HasPrefix(d.Name.Name, "Paint")) {
			out[receiverTypeName(d)] = true
		}
	}
	return out
}

// ---- W13

func checkW13Callbacks(p *pkg) (out []string) {
	for _, d := range p.exportedFns {
		n := d.Name.Name
		onPrefixed := strings.HasPrefix(n, "On") && len(n) > 2 && n[2] >= 'A' && n[2] <= 'Z'
		if !(onPrefixed || strings.Contains(n, "Listener") || strings.HasPrefix(n, "WithOn")) {
			continue
		}
		// Registration takes a function; a method that merely handles an
		// event value (a bus subscriber's OnCreated(ev)) is not a callback
		// the host registers.
		if takesFunc(d) {
			out = append(out, fmt.Sprintf("%s %s", p.at(d), fnLabel(d)))
		}
	}
	return
}

func takesFunc(d *ast.FuncDecl) bool {
	for _, f := range d.Type.Params.List {
		switch t := f.Type.(type) {
		case *ast.FuncType:
			return true
		case *ast.Ident:
			if strings.HasSuffix(t.Name, "Listener") || strings.HasSuffix(t.Name, "Handler") || strings.HasSuffix(t.Name, "Fn") || strings.HasSuffix(t.Name, "Func") {
				return true
			}
		}
	}
	return false
}

// ---- W14

func checkW14Close(p *pkg) (out []string) {
	// A goroutine that its own function joins (a .Wait() in the same body)
	// is fork-join work scoped to the call and needs no Close.
	var firstGo ast.Node
	for _, f := range p.files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || firstGo != nil {
				continue
			}
			var g ast.Node
			joins := false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.GoStmt:
					if g == nil {
						g = n
					}
				case *ast.CallExpr:
					if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Wait" {
						joins = true
					}
				}
				return true
			})
			if g != nil && !joins {
				firstGo = g
			}
		}
	}
	if firstGo == nil {
		return
	}
	for _, d := range p.exportedFns {
		if d.Recv != nil && d.Name.Name == "Close" {
			return
		}
	}
	return []string{fmt.Sprintf("%s starts a goroutine and exports no Close", p.at(firstGo))}
}

// ---- walker

func (p *pkg) eachCall(fn func(call *ast.CallExpr, sel *ast.SelectorExpr)) {
	for _, f := range p.files {
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					fn(call, sel)
				}
			}
			return true
		})
	}
}

// TestWidgetsConformToADR0267 is the ratchet. Run with
// WIDGET_CONFORMANCE_DUMP=1 to print the allowlist that would make the
// current tree pass, in Go syntax, for pasting after a migration phase.
func TestWidgetsConformToADR0267(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dump := os.Getenv("WIDGET_CONFORMANCE_DUMP") != ""
	var dumped []string
	packages := loadPackages(t, root)
	seen := map[string]bool{}
	for _, p := range packages {
		seen[p.rel] = true
		if why, ok := exempt[p.rel]; ok {
			if _, listed := allowlist[p.rel]; listed {
				t.Errorf("%s is exempt (%s) and must not be on the allowlist", p.rel, why)
			}
			continue
		}
		violated := map[string][]string{}
		for _, r := range rules {
			if f := r.check(p); len(f) > 0 {
				violated[r.id] = f
			}
		}
		allowed := allowlist[p.rel]
		if dump {
			ids := make([]string, 0, len(violated))
			for id := range violated {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			if len(ids) > 0 {
				dumped = append(dumped, fmt.Sprintf("\t%q: {%s},", p.rel, `"`+strings.Join(ids, `", "`)+`"`))
			}
			for _, id := range ids {
				for _, f := range violated[id] {
					t.Logf("%-24s %s %s", p.rel, id, f)
				}
			}
			continue
		}
		for _, r := range rules {
			f, isViolated := violated[r.id]
			isAllowed := slices.Contains(allowed, r.id)
			switch {
			case isViolated && !isAllowed:
				t.Errorf("%s violates %s (%s):\n\t%s", p.rel, r.id, r.title, strings.Join(f, "\n\t"))
			case !isViolated && isAllowed:
				t.Errorf("%s no longer violates %s — remove it from the allowlist", p.rel, r.id)
			}
		}
	}
	if dump {
		t.Logf("allowlist for the current tree:\n%s", strings.Join(dumped, "\n"))
	}
	for rel := range allowlist {
		if !seen[rel] {
			t.Errorf("allowlist names %s, which does not exist", rel)
		}
	}
}
