//go:build llm_generated_opus47

package widgets

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sync"

	"embed"
)

// demoSourceFS embeds every .go file in this package directory at build time.
// The embed lets the source panel render demo bodies without requiring the
// source tree at runtime (containerized binaries, CI artifacts).
//
//go:embed *.go
var demoSourceFS embed.FS

type funcRange struct {
	startLine int
	endLine   int
	startByte int
	endByte   int
}

type parsedDemoFile struct {
	bytes []byte
	funcs []funcRange
}

var (
	parseMu     sync.Mutex
	parsedCache = map[string]*parsedDemoFile{}
)

// extractFunctionSource returns the embedded file source plus the 1-based
// line range covering the FuncDecl-or-FuncLit whose body most narrowly
// contains `line` in `absFile`. firstLine == 0 indicates the file is not
// in the embedded FS, can't be parsed, or no function range covers the line.
//
// Returning the full file (rather than a pre-sliced byte range) lets the
// Go highlighter parse the complete source for AST refinement; the caller
// passes the line range to codeview.BuildGoLines, which clips spans to the
// window and renders a file-relative line-number gutter.
//
// "Most narrow" handles both top-level FuncDecls and inline FuncLits (e.g.
// the closure literal `Render: func(_ *c.WidgetIdStack) { ... }` in
// registrations.go) — the auto-resolved SourceLine lands at the closure's
// `func` keyword, which sits inside the enclosing FuncDecl `init()`; we
// want the inner closure, not init.
func extractFunctionSource(absFile string, line int) (fullSrc string, firstLine int32, lastLine int32) {
	parsed := getParsedDemoFile(filepath.Base(absFile))
	if parsed == nil {
		return
	}
	best := -1
	for i, fr := range parsed.funcs {
		if line < fr.startLine || line > fr.endLine {
			continue
		}
		if best < 0 {
			best = i
			continue
		}
		bestSpan := parsed.funcs[best].endLine - parsed.funcs[best].startLine
		span := fr.endLine - fr.startLine
		if span < bestSpan {
			best = i
		}
	}
	if best < 0 {
		return
	}
	fr := parsed.funcs[best]
	fullSrc = string(parsed.bytes)
	firstLine = int32(fr.startLine)
	lastLine = int32(fr.endLine)
	return
}

func getParsedDemoFile(basename string) (parsed *parsedDemoFile) {
	parseMu.Lock()
	defer parseMu.Unlock()
	if cached, ok := parsedCache[basename]; ok {
		parsed = cached
		return
	}
	defer func() { parsedCache[basename] = parsed }()
	data, err := demoSourceFS.ReadFile(basename)
	if err != nil {
		return
	}
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, basename, data, parser.SkipObjectResolution)
	if err != nil {
		return
	}
	funcs := make([]funcRange, 0, 16)
	ast.Inspect(af, func(n ast.Node) (descend bool) {
		descend = true
		if n == nil {
			return
		}
		var startPos, endPos token.Position
		switch fn := n.(type) {
		case *ast.FuncDecl:
			startPos = fset.Position(fn.Pos())
			endPos = fset.Position(fn.End())
		case *ast.FuncLit:
			startPos = fset.Position(fn.Pos())
			endPos = fset.Position(fn.End())
		default:
			return
		}
		funcs = append(funcs, funcRange{
			startLine: startPos.Line,
			endLine:   endPos.Line,
			startByte: startPos.Offset,
			endByte:   endPos.Offset,
		})
		return
	})
	parsed = &parsedDemoFile{bytes: data, funcs: funcs}
	return
}
