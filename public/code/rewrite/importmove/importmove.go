// Package importmove rewrites Go import paths after packages move: each
// import under a moved prefix is pointed at the new path, and what the
// rewrite cannot change safely is reported rather than guessed at.
//
// A prefix matches at path-segment boundaries only (moving a/b touches a/b
// and a/b/c, never a/bc), the longest matching prefix wins, and a keep-list
// names paths under a moved prefix that did not move. When the last segment
// changes, an unaliased import gains its old name as an alias, so the file
// compiles without renaming qualifiers; a later cleanup can drop the alias.
// It adds and removes no imports — goimports does that.
//
// Reported, not rewritten: a string literal naming a moved path (generator
// templates, embedded import lists, test fixtures), a //go:generate line
// naming one, and a template file (.tmpl, .gotmpl, .tpl) naming one.
package importmove

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Mapping moves the import path From, and everything below it, to To.
type Mapping struct {
	From string
	To   string
}

// Plan is a set of moves and the paths under them that stay.
type Plan struct {
	Mappings []Mapping
	// Keep names paths that did not move although a mapping covers them;
	// a kept path keeps everything below it too.
	Keep []string
}

// FindingKindE names what a Finding found.
type FindingKindE uint8

const (
	FindingKindStringLiteral FindingKindE = iota
	FindingKindGoGenerate
	FindingKindTemplate
)

func (inst FindingKindE) String() (s string) {
	switch inst {
	case FindingKindStringLiteral:
		return "string literal"
	case FindingKindGoGenerate:
		return "go:generate"
	case FindingKindTemplate:
		return "template"
	default:
		return "unknown"
	}
}

// Finding is a moved path the rewrite did not change.
type Finding struct {
	Path string
	Line int
	Kind FindingKindE
	// Moved is the path found, and Target where the plan moves it.
	Moved  string
	Target string
}

// FileResult is what the rewrite did to one Go file.
type FileResult struct {
	Path     string
	Rewrites int
	// Aliased names the imports that gained their old name as an alias.
	Aliased  []string
	Findings []Finding
	// Unparsed is set when the file is not valid Go; it is left as it is.
	Unparsed error
}

// Validate refuses a plan with an empty or relative-looking path, or a
// mapping that moves a path onto itself.
func (inst Plan) Validate() (err error) {
	if len(inst.Mappings) == 0 {
		return eh.Errorf("no mapping given")
	}
	for _, m := range inst.Mappings {
		if m.From == "" || m.To == "" || strings.HasSuffix(m.From, "/") || strings.HasSuffix(m.To, "/") {
			return eb.Build().Str("from", m.From).Str("to", m.To).Errorf("a mapping names two import paths without a trailing slash")
		}
		if m.From == m.To {
			return eb.Build().Str("from", m.From).Errorf("a mapping moves a path onto itself")
		}
	}
	for _, k := range inst.Keep {
		if k == "" || strings.HasSuffix(k, "/") {
			return eb.Build().Str("keep", k).Errorf("a kept path is an import path without a trailing slash")
		}
	}
	return
}

// Target returns where the plan moves path; ok is false when it stays.
func (inst Plan) Target(path string) (to string, ok bool) {
	for _, k := range inst.Keep {
		if under(path, k) {
			return
		}
	}
	best := -1
	for i, m := range inst.Mappings {
		if under(path, m.From) && (best < 0 || len(m.From) > len(inst.Mappings[best].From)) {
			best = i
		}
	}
	if best < 0 {
		return
	}
	m := inst.Mappings[best]
	return m.To + path[len(m.From):], true
}

// under reports path equal to prefix or below it at a segment boundary.
func under(path string, prefix string) (yes bool) {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// RewriteFile rewrites the imports of one Go source file. out is src when
// nothing changed.
func (inst Plan) RewriteFile(name string, src []byte) (out []byte, res FileResult, err error) {
	res.Path = name
	out = src
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		err = eb.Build().Str("file", name).Errorf("parse: %w", err)
		return
	}
	var edits []edit
	importLits := map[*ast.BasicLit]bool{}
	for _, is := range f.Imports {
		importLits[is.Path] = true
		ip, uerr := strconv.Unquote(is.Path.Value)
		if uerr != nil {
			continue
		}
		np, moved := inst.Target(ip)
		if !moved {
			continue
		}
		start := fset.Position(is.Path.Pos()).Offset
		text := strconv.Quote(np)
		if is.Name == nil && DefaultName(ip) != DefaultName(np) {
			text = DefaultName(ip) + " " + text
			res.Aliased = append(res.Aliased, np)
		}
		edits = append(edits, edit{start: start, end: fset.Position(is.Path.End()).Offset, text: text})
		res.Rewrites++
	}
	ast.Inspect(f, func(n ast.Node) (cont bool) {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || importLits[lit] {
			return true
		}
		for _, p := range inst.movedPaths(lit.Value) {
			to, _ := inst.Target(p)
			res.Findings = append(res.Findings, Finding{Path: name, Line: fset.Position(lit.Pos()).Line, Kind: FindingKindStringLiteral, Moved: p, Target: to})
		}
		return true
	})
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if !strings.HasPrefix(c.Text, "//go:generate") {
				continue
			}
			for _, p := range inst.movedPaths(c.Text) {
				to, _ := inst.Target(p)
				res.Findings = append(res.Findings, Finding{Path: name, Line: fset.Position(c.Pos()).Line, Kind: FindingKindGoGenerate, Moved: p, Target: to})
			}
		}
	}
	if len(edits) == 0 {
		return
	}
	out = applyEdits(src, edits)
	formatted, ferr := format.Source(out)
	if ferr != nil {
		err = eb.Build().Str("file", name).Errorf("format the rewritten file: %w", ferr)
		return
	}
	out = formatted
	return
}

// ScanTemplate reports the moved paths a template file names, by line.
func (inst Plan) ScanTemplate(name string, src []byte) (findings []Finding) {
	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	line := 0
	for sc.Scan() {
		line++
		for _, p := range inst.movedPaths(sc.Text()) {
			to, _ := inst.Target(p)
			findings = append(findings, Finding{Path: name, Line: line, Kind: FindingKindTemplate, Moved: p, Target: to})
		}
	}
	return
}

// movedPaths are the import paths in text that the plan moves: each
// occurrence of a mapping's From that starts and ends at a path boundary,
// extended to the whole path it begins.
func (inst Plan) movedPaths(text string) (paths []string) {
	for _, m := range inst.Mappings {
		for at := 0; ; {
			i := strings.Index(text[at:], m.From)
			if i < 0 {
				break
			}
			i += at
			end := i + len(m.From)
			at = end
			if i > 0 && isPathByte(text[i-1]) {
				continue
			}
			if end < len(text) && text[end] != '/' && isPathByte(text[end]) {
				continue
			}
			for end < len(text) && (isPathByte(text[end]) || text[end] == '/') {
				end++
			}
			p := strings.TrimRight(text[i:end], "/.")
			if _, moved := inst.Target(p); moved && !slices.Contains(paths, p) {
				paths = append(paths, p)
			}
		}
	}
	sort.Strings(paths)
	return
}

func isPathByte(b byte) (yes bool) {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '.' || b == '_' || b == '-' || b == '~'
}

// DefaultName is the package name an import path implies: its last
// segment, or the one before a major-version suffix (v2, v18).
func DefaultName(path string) (name string) {
	name = path[strings.LastIndex(path, "/")+1:]
	if len(name) > 1 && name[0] == 'v' && strings.Trim(name[1:], "0123456789") == "" && strings.Contains(path, "/") {
		p := path[:strings.LastIndex(path, "/")]
		name = p[strings.LastIndex(p, "/")+1:]
	}
	return
}

type edit struct {
	start int
	end   int
	text  string
}

func applyEdits(src []byte, edits []edit) (out []byte) {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	out = make([]byte, 0, len(src)+64)
	pos := 0
	for _, e := range edits {
		out = append(out, src[pos:e.start]...)
		out = append(out, e.text...)
		pos = e.end
	}
	out = append(out, src[pos:]...)
	return
}

// mentionsAny reports src containing any mapping's From.
func (inst Plan) mentionsAny(src []byte) (yes bool) {
	for _, m := range inst.Mappings {
		if bytes.Contains(src, []byte(m.From)) {
			return true
		}
	}
	return
}

// templateExts are the file extensions scanned as templates.
var templateExts = []string{".tmpl", ".gotmpl", ".tpl"}

// Run walks root, rewrites every Go file (writing it back when write is
// true) and scans template files. Directories whose name starts with a
// dot, vendor, node_modules and target are skipped. A Go file that does
// not parse (a deliberately broken test fixture, say) is left as it is and
// reported with Unparsed set.
func (inst Plan) Run(root string, write bool) (results []FileResult, err error) {
	if err = inst.Validate(); err != nil {
		return
	}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) (err error) {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			n := d.Name()
			if p != root && (strings.HasPrefix(n, ".") || n == "vendor" || n == "node_modules" || n == "target") {
				return filepath.SkipDir
			}
			return
		}
		ext := filepath.Ext(p)
		if ext != ".go" && !slices.Contains(templateExts, ext) {
			return
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return
		}
		if ext != ".go" {
			if found := inst.ScanTemplate(p, src); len(found) > 0 {
				results = append(results, FileResult{Path: p, Findings: found})
			}
			return
		}
		out, res, rerr := inst.RewriteFile(p, src)
		if rerr != nil {
			if inst.mentionsAny(src) {
				results = append(results, FileResult{Path: p, Unparsed: rerr})
			}
			return
		}
		if res.Rewrites == 0 && len(res.Findings) == 0 {
			return
		}
		if write && res.Rewrites > 0 {
			var fi fs.FileInfo
			fi, err = d.Info()
			if err != nil {
				return
			}
			if err = os.WriteFile(p, out, fi.Mode().Perm()); err != nil {
				return eb.Build().Str("file", p).Errorf("write: %w", err)
			}
		}
		results = append(results, res)
		return
	})
	return
}
