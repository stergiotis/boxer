package doclint

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL013 — a package-qualified identifier in a ```go fence resolves to an
// exported top-level identifier of that package in the repository.
//
// Implements DOCUMENTATION_STANDARD §4 *Claims that decay*: code in a
// document names symbols, and a symbol that was renamed or never existed
// reads with the same authority as one that does. The rule applies to every
// status, drafts included — a draft may describe a package that does not
// exist yet, but then it says so with the opt-out below rather than showing
// code a reader will try to compile.
//
// What is checked: each selector `X.Y` in a fence whose info string is `go`,
// where Y is exported, X is not declared inside the fence (a variable,
// parameter, receiver or type of the snippet itself), and X names a package
// of this repository — through the fence's own import block when it has
// one, otherwise by package name. Y must then be a top-level function, type,
// variable or constant of that package (any package of that name, when the
// name is shared). Qualifiers this repository cannot vouch for — the
// standard library, modules from go.mod, a variable the snippet assumes —
// are skipped, as is a fence that does not parse as a file, a declaration
// list or a statement list: the rule prefers a missed finding to a false
// one. Methods and fields (`X.Y.Z`) are not checked past Y.
//
// Severity is error: the repository's own documents carry no finding, so a
// new one is a regression rather than backlog.
//
// A fence that is deliberately illustrative is exempted by an HTML comment
// on the last non-blank line before it, in the directive form codelint
// uses:
//
//	<!-- boxer:lint disable=DL013 reason="sketch of a proposed API" -->
type RuleDL013 struct {
	mu      sync.Mutex
	indexes map[string]*goSymbolIndex
}

// goSymbolIndex is the exported top-level identifiers of one repository's
// non-test Go files, by package name and by import path.
type goSymbolIndex struct {
	modulePath   string
	byName       map[string]map[string]struct{}
	byImportPath map[string]map[string]struct{}
	// foreignNames are the local names under which the repository's own Go
	// files import a package from outside the module (the standard library
	// or a dependency). A fence qualifier with such a name is ambiguous
	// unless the fence imports it.
	foreignNames map[string]struct{}
}

func NewRuleDL013() (inst *RuleDL013) {
	inst = &RuleDL013{indexes: make(map[string]*goSymbolIndex)}
	return
}

func (inst *RuleDL013) Id() (id string) {
	id = "DL013"
	return
}

func (inst *RuleDL013) Check(roots []string) iter.Seq2[Finding, error] {
	return runMarkdownCheckScoped("DL013", roots, isInScopeForDL013, inst.checkOne)
}

// isInScopeForDL013 is every Markdown file outside fixtures and generated
// output: unlike the front-matter rules it reads skills, the CLAUDE.md shim
// and the landing README, since what it checks is the code they show.
func isInScopeForDL013(path string, base string) (in bool) {
	sep := string(filepath.Separator)
	in = !strings.HasSuffix(base, ".gen.md") && !strings.Contains(path, sep+"testdata"+sep) && !strings.Contains(path, sep+"changelog"+sep)
	return
}

var dl013Directive = regexp.MustCompile(`^<!--\s*boxer:lint\s+disable=([A-Z]+\d+(?:\s*,\s*[A-Z]+\d+)*)\s+reason="[^"]+"\s*-->$`)

// goFence is one ```go fenced block: its content with the fence's own
// indentation removed, and the 1-based body line of its first content line.
type goFence struct {
	Src       string
	FirstLine int32
	OptOut    bool
}

// extractGoFences returns the ```go / ~~~go fenced blocks of a Markdown body.
func extractGoFences(body []byte) (fences []goFence) {
	lines := strings.Split(string(body), "\n")
	lastNonBlank := ""
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		trimmed := strings.TrimLeft(raw, " \t")
		indent := len(raw) - len(trimmed)
		marker := ""
		switch {
		case strings.HasPrefix(trimmed, "```"):
			marker = "```"
		case strings.HasPrefix(trimmed, "~~~"):
			marker = "~~~"
		}
		if marker == "" {
			if strings.TrimSpace(raw) != "" {
				lastNonBlank = strings.TrimSpace(raw)
			}
			continue
		}
		run := len(trimmed) - len(strings.TrimLeft(trimmed, marker[:1]))
		info := strings.Fields(trimmed[run:])
		isGo := len(info) > 0 && (info[0] == "go" || info[0] == "golang")
		optOut := false
		if m := dl013Directive.FindStringSubmatch(lastNonBlank); m != nil {
			for _, id := range strings.Split(m[1], ",") {
				if strings.TrimSpace(id) == "DL013" {
					optOut = true
				}
			}
		}
		closing := strings.Repeat(marker[:1], run)
		var content []string
		j := i + 1
		for ; j < len(lines); j++ {
			t := strings.TrimLeft(lines[j], " \t")
			if strings.HasPrefix(t, closing) && strings.TrimSpace(strings.TrimLeft(t, marker[:1])) == "" {
				break
			}
			l := lines[j]
			// Strip up to the fence's own indentation, so a fence inside a
			// list item parses like a top-level one.
			k := 0
			for k < indent && k < len(l) && (l[k] == ' ' || l[k] == '\t') {
				k++
			}
			content = append(content, l[k:])
		}
		if isGo {
			fences = append(fences, goFence{Src: strings.Join(content, "\n"), FirstLine: int32(i + 2), OptOut: optOut})
		}
		i = j
		lastNonBlank = ""
	}
	return
}

// parseGoFence parses src as a file, as a declaration list or as a
// statement list, in that order. lineShift is the number of synthetic lines
// prepended, to map positions back to the fence.
func parseGoFence(src string) (file *ast.File, fset *token.FileSet, lineShift int, ok bool) {
	attempts := []struct {
		prefix string
		suffix string
		shift  int
	}{
		{"", "", 0},
		{"package p\n", "", 1},
		{"package p\nfunc _() {\n", "\n}", 2},
	}
	for _, a := range attempts {
		fset = token.NewFileSet()
		f, err := parser.ParseFile(fset, "fence.go", a.prefix+src+a.suffix, parser.SkipObjectResolution)
		if err == nil {
			file, lineShift, ok = f, a.shift, true
			return
		}
	}
	return
}

// localNames collects every name the snippet declares itself: variables,
// constants, types, functions, parameters, results, receivers, range
// variables, labels and struct fields. A selector whose qualifier is one of
// these is a field or method access, not a package reference.
func localNames(file *ast.File) (names map[string]struct{}) {
	names = make(map[string]struct{})
	add := func(id *ast.Ident) {
		if id != nil {
			names[id.Name] = struct{}{}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, l := range x.Lhs {
					if id, isId := l.(*ast.Ident); isId {
						add(id)
					}
				}
			}
		case *ast.RangeStmt:
			if x.Tok == token.DEFINE {
				if id, isId := x.Key.(*ast.Ident); isId {
					add(id)
				}
				if id, isId := x.Value.(*ast.Ident); isId {
					add(id)
				}
			}
		case *ast.ValueSpec:
			for _, id := range x.Names {
				add(id)
			}
		case *ast.TypeSpec:
			add(x.Name)
		case *ast.FuncDecl:
			add(x.Name)
		case *ast.Field:
			for _, id := range x.Names {
				add(id)
			}
		case *ast.LabeledStmt:
			add(x.Label)
		}
		return true
	})
	return
}

func (inst *RuleDL013) checkOne(filePath string, ignored map[string]struct{}, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	var data []byte
	data, err = os.ReadFile(filePath)
	if err != nil {
		err = eb.Build().Str("path", filePath).Errorf("DL013 read: %w", err)
		return
	}
	_, body, ok, parseErr := parseMdFrontMatter(data)
	if !ok || parseErr != nil {
		body = data
	}
	fences := extractGoFences(body)
	if len(fences) == 0 {
		return
	}
	lineOffset := frontMatterLineOffset(data, body)
	var idx *goSymbolIndex
	for _, fence := range fences {
		if fence.OptOut {
			continue
		}
		file, fset, shift, parsed := parseGoFence(fence.Src)
		if !parsed {
			continue
		}
		if idx == nil {
			idx = inst.indexFor(filepath.Dir(filePath))
			if idx == nil {
				return
			}
		}
		imports := make(map[string]string, len(file.Imports))
		for _, spec := range file.Imports {
			p, uqErr := strconv.Unquote(spec.Path.Value)
			if uqErr != nil {
				continue
			}
			name := p[strings.LastIndex(p, "/")+1:]
			if spec.Name != nil {
				name = spec.Name.Name
			}
			imports[name] = p
		}
		locals := localNames(file)
		reported := make(map[string]struct{})
		ast.Inspect(file, func(n ast.Node) bool {
			if !cont {
				return false
			}
			sel, isSel := n.(*ast.SelectorExpr)
			if !isSel {
				return true
			}
			q, isIdent := sel.X.(*ast.Ident)
			if !isIdent || !ast.IsExported(sel.Sel.Name) {
				return true
			}
			if _, isLocal := locals[q.Name]; isLocal {
				return true
			}
			known, checked := idx.lookup(q.Name, imports[q.Name])
			if !checked {
				return true
			}
			if _, found := known[sel.Sel.Name]; found {
				return true
			}
			line := int32(fset.Position(sel.Pos()).Line-shift) + fence.FirstLine - 1 + lineOffset
			key := strconv.Itoa(int(line)) + " " + q.Name + "." + sel.Sel.Name
			if _, dup := reported[key]; dup {
				return true
			}
			reported[key] = struct{}{}
			cont = yield(Finding{
				RuleId:   "DL013",
				Severity: FindingSeverityError,
				Path:     filePath,
				Line:     line,
				Col:      1,
				Message: "`" + q.Name + "." + sel.Sel.Name + "` in a go fence does not resolve: package " + q.Name +
					" in this repository has no exported top-level " + sel.Sel.Name +
					`; fix the code, or mark a deliberate sketch with <!-- boxer:lint disable=DL013 reason="..." --> before the fence`,
			}, nil)
			return cont
		})
		if !cont {
			return
		}
	}
	return
}

// lookup returns the identifiers of the package a qualifier refers to and
// whether this repository can vouch for it. importPath is the fence's
// import for the qualifier, or "".
func (inst *goSymbolIndex) lookup(qualifier string, importPath string) (known map[string]struct{}, checked bool) {
	if importPath != "" {
		if inst.modulePath == "" || (importPath != inst.modulePath && !strings.HasPrefix(importPath, inst.modulePath+"/")) {
			return
		}
		known, checked = inst.byImportPath[importPath]
		if !checked {
			// An import of this module that names no package directory.
			known, checked = map[string]struct{}{}, true
		}
		return
	}
	if _, foreign := inst.foreignNames[qualifier]; foreign {
		return
	}
	known, checked = inst.byName[qualifier]
	return
}

// indexFor returns the symbol index of the repository containing dir,
// building it on first use; nil when dir is not inside a repository.
func (inst *RuleDL013) indexFor(dir string) (idx *goSymbolIndex) {
	top := findGitTop(dir)
	if top == "" {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if cached, ok := inst.indexes[top]; ok {
		idx = cached
		return
	}
	idx = buildGoSymbolIndex(top)
	inst.indexes[top] = idx
	return
}

// findGitTop walks up from dir to the nearest directory holding a '.git'
// entry (a directory, or the file a worktree carries).
func findGitTop(dir string) (top string) {
	cur, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	for {
		if _, statErr := os.Stat(filepath.Join(cur, ".git")); statErr == nil {
			top = cur
			return
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return
		}
		cur = parent
	}
}

// buildGoSymbolIndex parses the top-level declarations of every non-test Go
// file under top that git does not ignore. Package main is skipped: nothing
// imports it.
func buildGoSymbolIndex(top string) (idx *goSymbolIndex) {
	idx = &goSymbolIndex{
		modulePath:   readModulePath(filepath.Join(top, "go.mod")),
		byName:       make(map[string]map[string]struct{}),
		byImportPath: make(map[string]map[string]struct{}),
		foreignNames: make(map[string]struct{}),
	}
	ignored := gitIgnoredSet(top)
	_ = filepath.WalkDir(top, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			// testdata is walked: a package under it is importable by its
			// full path, and a fence that imports one is checked against it.
			if path != top && name != "testdata" && (shouldSkipDir(name) || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || isGitIgnored(ignored, path)) {
				return filepath.SkipDir
			}
			return nil
		}
		sep := string(filepath.Separator)
		underTestdata := strings.Contains(path, sep+"testdata"+sep)
		if !strings.HasSuffix(name, ".go") || isGitIgnored(ignored, path) {
			return nil
		}
		fset := token.NewFileSet()
		if strings.HasSuffix(name, "_test.go") {
			// Test files declare nothing a document can import, but how
			// they import still shows which names are taken from outside.
			if file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly); parseErr == nil && !underTestdata {
				idx.noteForeignImports(file)
			}
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return nil
		}
		if !underTestdata {
			idx.noteForeignImports(file)
		}
		if file.Name.Name == "main" {
			return nil
		}
		// A fixture package is reachable by import path only; its name
		// would otherwise make an unrelated qualifier look checkable.
		if !underTestdata {
			pkg := file.Name.Name
			sink := idx.byName[pkg]
			if sink == nil {
				sink = make(map[string]struct{})
				idx.byName[pkg] = sink
			}
			collectExportedSymbolsDL008(file, sink)
		}
		if idx.modulePath != "" {
			rel, relErr := filepath.Rel(top, filepath.Dir(path))
			if relErr == nil {
				ip := idx.modulePath
				if rel != "." {
					ip += "/" + filepath.ToSlash(rel)
				}
				byPath := idx.byImportPath[ip]
				if byPath == nil {
					byPath = make(map[string]struct{})
					idx.byImportPath[ip] = byPath
				}
				collectExportedSymbolsDL008(file, byPath)
			}
		}
		return nil
	})
	return
}

// noteForeignImports records the local names under which file imports
// packages from outside the module.
func (inst *goSymbolIndex) noteForeignImports(file *ast.File) {
	for _, spec := range file.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if inst.modulePath != "" && (p == inst.modulePath || strings.HasPrefix(p, inst.modulePath+"/")) {
			continue
		}
		local := importLocalName(p)
		if spec.Name != nil {
			local = spec.Name.Name
		}
		if local != "_" && local != "." {
			inst.foreignNames[local] = struct{}{}
		}
	}
}

// importLocalName guesses the name an unaliased import binds: the last path
// element, skipping a major-version suffix (`/v3`) and a gopkg.in `.vN`.
func importLocalName(importPath string) (name string) {
	elems := strings.Split(importPath, "/")
	name = elems[len(elems)-1]
	if len(elems) > 1 && len(name) > 1 && name[0] == 'v' && strings.Trim(name[1:], "0123456789") == "" {
		name = elems[len(elems)-2]
	}
	if i := strings.Index(name, ".v"); i > 0 {
		name = name[:i]
	}
	return
}

// readModulePath returns the module path a go.mod declares, or "".
func readModulePath(goMod string) (path string) {
	data, err := os.ReadFile(goMod)
	if err != nil {
		return
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if rest, found := strings.CutPrefix(line, "module "); found {
			path = strings.Trim(strings.TrimSpace(rest), `"`)
			return
		}
	}
	return
}
