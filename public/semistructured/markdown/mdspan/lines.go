package mdspan

import (
	"sort"
	"unicode/utf8"
)

// Span is a half-open byte range [Start, End) of a source.
type Span struct {
	Start int
	End   int
}

// IsEmpty says whether the span covers no byte.
func (inst Span) IsEmpty() bool { return inst.End <= inst.Start }

// LineSpan is a closed range of 1-based lines. A zero First means no line.
type LineSpan struct {
	First int
	Last  int
}

// LineIndex converts between byte offsets and 1-based lines and columns of
// one source. Columns count runes, so a column is what an editor shows.
type LineIndex struct {
	src    []byte
	starts []int
}

// NewLineIndex indexes src. A trailing newline does not open a line.
func NewLineIndex(src []byte) (idx *LineIndex) {
	starts := make([]int, 1, len(src)/32+1)
	for i, b := range src {
		if b == '\n' && i+1 < len(src) {
			starts = append(starts, i+1)
		}
	}
	idx = &LineIndex{src: src, starts: starts}
	return
}

// Count is the number of lines; an empty source has none.
func (inst *LineIndex) Count() int {
	if len(inst.src) == 0 {
		return 0
	}
	return len(inst.starts)
}

// LineOf is the 1-based line holding off. An offset at the end of the
// source belongs to the last line.
func (inst *LineIndex) LineOf(off int) int {
	off = max(0, min(off, len(inst.src)))
	return sort.SearchInts(inst.starts, off+1)
}

// Position is the 1-based line and rune column of off.
func (inst *LineIndex) Position(off int) (line int, col int) {
	off = max(0, min(off, len(inst.src)))
	line = inst.LineOf(off)
	col = utf8.RuneCount(inst.src[inst.starts[line-1]:off]) + 1
	return
}

// LineStart is the offset line begins at, clamped to the source; a line past
// the last is the end of the source.
func (inst *LineIndex) LineStart(line int) int {
	if line < 1 {
		return 0
	}
	if line > len(inst.starts) {
		return len(inst.src)
	}
	return inst.starts[line-1]
}

// LineEnd is the offset just past line's newline, or the end of the source
// for the last line.
func (inst *LineIndex) LineEnd(line int) int {
	if line < 1 {
		return 0
	}
	if line >= len(inst.starts) {
		return len(inst.src)
	}
	return inst.starts[line]
}

// Lines is the byte span of a range of lines, newlines included.
func (inst *LineIndex) Lines(ls LineSpan) Span {
	return Span{Start: inst.LineStart(ls.First), End: inst.LineEnd(ls.Last)}
}

// LineSpanOf is the lines a byte span touches. An empty span names the line
// it sits on.
func (inst *LineIndex) LineSpanOf(s Span) (ls LineSpan) {
	ls.First = inst.LineOf(s.Start)
	ls.Last = ls.First
	if s.End > s.Start {
		ls.Last = inst.LineOf(s.End - 1)
	}
	return
}

// Offset is the byte offset of a 1-based line and rune column, clamped to
// the line.
func (inst *LineIndex) Offset(line int, col int) (off int) {
	off = inst.LineStart(line)
	end := inst.LineEnd(line)
	for c := 1; c < col && off < end; c++ {
		if inst.src[off] == '\n' {
			break
		}
		_, sz := utf8.DecodeRune(inst.src[off:])
		off += sz
	}
	return
}
