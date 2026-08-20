package ladingingest

import (
	"bytes"
	"unicode/utf8"
)

// sniffLen is how much of a file [TextRuleSniff] looks at. Enough to catch a
// binary header, small enough to be one read.
const sniffLen = 8 << 10

// block is one cut of a file's content, with the line number its first line
// carries. line0 is 1-based and 0 on a non-text block.
type block struct {
	data  []byte
	line0 uint32
}

// cut splits content into blocks of at most size bytes and reports whether the
// result is newline-aligned.
//
// # What `text` promises
//
// When cut returns text = true, **every boundary between two blocks falls
// immediately after a newline** — the last block ends where the file does,
// newline or not. That is not a hint, it is the invariant a line-oriented
// query over the block table rests on: no line straddles a boundary, so
// `splitByChar('\n', data)` per block is the whole file's line set and
// `line0 + i - 1` is the real line number.
//
// Which is why a file whose rule says text but which carries a line longer
// than one block comes back text = false, cut at fixed offsets. The
// alternative — cutting that one line mid-way and leaving the flag set — would
// make the flag mean "usually" and would lose exactly the matches that span
// the cut, silently. A caller that wants the guarantee for such a file has to
// raise the block size, and the entry row tells it which files those are.
func cut(content []byte, size uint32, rule TextRuleE) (blocks []block, text bool) {
	if len(content) == 0 {
		// Nothing was cut, so nothing was cut at newlines. Vacuous truth here
		// would put a flag on a row with no blocks to describe.
		return nil, false
	}
	if size == 0 {
		size = 1
	}
	if rule == TextRuleSniff && isText(content) {
		if b, ok := cutAtNewlines(content, int(size)); ok {
			return b, true
		}
	}
	return cutFixed(content, int(size)), false
}

// cutFixed splits at exact offsets. line0 stays 0: the block boundaries say
// nothing about lines, and a number that looked like a line number but was not
// one would be worse than none.
func cutFixed(content []byte, size int) (blocks []block) {
	if len(content) == 0 {
		return
	}
	blocks = make([]block, 0, (len(content)+size-1)/size)
	for off := 0; off < len(content); off += size {
		blocks = append(blocks, block{data: content[off:min(off+size, len(content))]})
	}
	return
}

// cutAtNewlines splits after the last newline that fits. It reports ok = false
// when some line does not fit in a block at all, which is the one case the
// newline-alignment promise cannot be kept for.
func cutAtNewlines(content []byte, size int) (blocks []block, ok bool) {
	line := uint32(1)
	for off := 0; off < len(content); {
		end := off + size
		if end >= len(content) {
			end = len(content)
		} else {
			// The last newline at or before the size limit; +1 so the block
			// keeps its terminator and the next one starts a line.
			nl := bytes.LastIndexByte(content[off:end], '\n')
			if nl < 0 {
				// One line longer than a whole block. Nothing to cut on.
				return nil, false
			}
			end = off + nl + 1
		}
		b := content[off:end]
		blocks = append(blocks, block{data: b, line0: line})
		line += uint32(bytes.Count(b, []byte{'\n'}))
		off = end
	}
	return blocks, true
}

// isText is the [TextRuleSniff] rule: the first sniffLen bytes decode as UTF-8 and
// carry no NUL.
//
// Deliberately content-based rather than extension-based — an extension says
// what a file is meant to be, and the cut has to follow what its bytes are.
// The truncation is checked against a rune boundary so a multi-byte rune
// straddling the window is not read as invalid.
func isText(content []byte) bool {
	head := content
	truncated := false
	if len(head) > sniffLen {
		head, truncated = head[:sniffLen], true
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}
	if truncated {
		// Drop a trailing partial rune rather than judging it invalid.
		//
		// The bound is how far into the *window* the trimming has gone, not
		// how far from the end of the file: a rune is at most UTFMax bytes, so
		// anything past that is genuinely invalid UTF-8 rather than a rune the
		// window cut in half. Measuring it against len(content) made the guard
		// fire on the first trimmed byte of any file over sniffLen, so every
		// text file above 8 KiB whose window happened to end mid-rune was
		// judged binary — and with it went the newline cutting the whole
		// line-oriented SQL surface rests on.
		for len(head) > 0 && sniffLen-len(head) <= utf8.UTFMax && !utf8.Valid(head) {
			if r, sz := utf8.DecodeLastRune(head); r != utf8.RuneError || sz != 1 {
				break
			}
			head = head[:len(head)-1]
		}
	}
	return utf8.Valid(head)
}
