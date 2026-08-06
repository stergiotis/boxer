package codevol

import (
	"go/scanner"
	"go/token"
	"os"
	"regexp"
	"strings"
)

// Volume is a line tally over a set of source files.
type Volume struct {
	Files int
	// CodeLines counts lines bearing at least one non-comment token,
	// CommentLines lines bearing only comment tokens, BlankLines the rest.
	// A line with both code and a trailing comment counts as code only, so
	// the three always sum to the file's line count.
	CodeLines    int
	CommentLines int
	BlankLines   int
	// GeneratedFiles counts files carrying the conventional marker. It
	// matters more than it looks: 40% of this repository's own compiled
	// lines are generated, and a first-party total that hides that overstates
	// what anyone wrote.
	GeneratedFiles int
	GeneratedCode  int
	// OtherLangLines counts the C, C++, assembly and header files compiled
	// with a cgo package — invisible to any Go-only count, and 377k lines in
	// this module's dependency closure.
	OtherLangLines int
}

func (v *Volume) add(o Volume) {
	v.Files += o.Files
	v.CodeLines += o.CodeLines
	v.CommentLines += o.CommentLines
	v.BlankLines += o.BlankLines
	v.GeneratedFiles += o.GeneratedFiles
	v.GeneratedCode += o.GeneratedCode
	v.OtherLangLines += o.OtherLangLines
}

// generatedRe is the convention from https://go.dev/s/generatedcode: the line
// must match exactly, on its own, before the package clause.
var generatedRe = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

// CountFiles tallies goPaths (classified with go/scanner) and otherPaths
// (counted as raw lines).
//
// Callers pass file paths rather than a loader's package type on purpose:
// this keeps the package free of golang.org/x/tools, so it can be linked into
// builds that must not carry a Go toolchain's worth of analysis code.
//
// Unreadable files are skipped rather than failed on — a volume tally is
// best-effort evidence, and one unreadable file should not empty a table.
func CountFiles(goPaths []string, otherPaths []string) (v Volume) {
	for _, p := range goPaths {
		c, ok := countGoFile(p)
		if !ok {
			continue
		}
		v.add(c)
	}
	for _, p := range otherPaths {
		n, ok := countRawLines(p)
		if !ok {
			continue
		}
		v.OtherLangLines += n
	}
	return
}

// countGoFile classifies one Go file's lines.
//
// go/scanner rather than a line-oriented regex, because "//" inside a string
// literal is common in this repository (URLs, import paths, SQL) and a
// textual scan counts those lines as comments.
func countGoFile(path string) (v Volume, ok bool) {
	src, err := os.ReadFile(path)
	if err != nil {
		return
	}
	v.Files = 1
	total := strings.Count(string(src), "\n")
	if len(src) > 0 && !strings.HasSuffix(string(src), "\n") {
		total++
	}

	fset := token.NewFileSet()
	f := fset.AddFile(path, fset.Base(), len(src))
	var s scanner.Scanner
	// The error handler is a no-op: a file that does not parse still has
	// countable lines, and this is a tally, not a compiler.
	s.Init(f, src, func(token.Position, string) {}, scanner.ScanComments)

	code := make(map[int]struct{}, 256)
	comment := make(map[int]struct{}, 128)
	generated := false
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		line := f.Position(pos).Line
		if tok == token.COMMENT {
			// A block comment covers every line it spans.
			for i := 0; i <= strings.Count(lit, "\n"); i++ {
				comment[line+i] = struct{}{}
			}
			if !generated && generatedRe.MatchString(strings.TrimRight(lit, "\r")) {
				generated = true
			}
			continue
		}
		code[line] = struct{}{}
	}

	v.CodeLines = len(code)
	for l := range comment {
		if _, isCode := code[l]; !isCode {
			v.CommentLines++
		}
	}
	v.BlankLines = max(total-v.CodeLines-v.CommentLines, 0)
	if generated {
		v.GeneratedFiles = 1
		v.GeneratedCode = v.CodeLines
	}
	ok = true
	return
}

func countRawLines(path string) (n int, ok bool) {
	src, err := os.ReadFile(path)
	if err != nil {
		return
	}
	n = strings.Count(string(src), "\n")
	if len(src) > 0 && !strings.HasSuffix(string(src), "\n") {
		n++
	}
	ok = true
	return
}
