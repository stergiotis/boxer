package codeview

import (
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdownhighlight"
)

func fontSectionsOf(t *testing.T, src string) []StyledSection {
	t.Helper()
	return MarkdownFontSections(src, markdownhighlight.HighlightLex([]byte(src)), nil)
}

func findSection(secs []StyledSection, start, stop int) (StyledSection, bool) {
	for _, s := range secs {
		if int(s.Start) == start && int(s.Stop) == stop {
			return s, true
		}
	}
	return StyledSection{}, false
}

func TestMarkdownFontSectionsBodyCoversTheBuffer(t *testing.T) {
	src := "plain prose\nmore prose\n"
	secs := fontSectionsOf(t, src)
	body, ok := findSection(secs, 0, len(src))
	if !ok {
		t.Fatalf("no whole-buffer section in %+v", secs)
	}
	if body.Family != FontProportional || body.Size != 1.0 || body.Flags != StyleFont {
		t.Fatalf("body section = %+v", body)
	}
	if len(secs) != 1 {
		t.Fatalf("prose alone should yield the body section only, got %+v", secs)
	}
}

func TestMarkdownFontSectionsEmptySourceYieldsNothing(t *testing.T) {
	if secs := fontSectionsOf(t, ""); len(secs) != 0 {
		t.Fatalf("got %+v", secs)
	}
}

func TestMarkdownFontSectionsHeadingLinesScaleByLevel(t *testing.T) {
	src := "# One\n\n### Three\nbody\n####### seven\n"
	secs := fontSectionsOf(t, src)
	h1, ok := findSection(secs, 0, len("# One"))
	if !ok || h1.Size != MarkdownFontScale[0] || h1.Family != FontProportional {
		t.Fatalf("h1 = %+v ok=%v", h1, ok)
	}
	start := len("# One\n\n")
	h3, ok := findSection(secs, start, start+len("### Three"))
	if !ok || h3.Size != MarkdownFontScale[2] {
		t.Fatalf("h3 = %+v ok=%v", h3, ok)
	}
	// seven hashes is not an ATX heading in CommonMark; the lexer decides,
	// and whatever it says the derivation must not index past the table.
	for _, s := range secs {
		if s.Size > MarkdownFontScale[0] || s.Size <= 0 {
			t.Fatalf("size out of range: %+v", s)
		}
	}
}

func TestMarkdownFontSectionsCodeIsMonospaceAndCoalesced(t *testing.T) {
	src := "text `code` text\n\n```go\nfunc main() {}\n```\n"
	secs := fontSectionsOf(t, src)
	// the inline span: delimiters and text as ONE monospace run
	span, ok := findSection(secs, len("text "), len("text `code`"))
	if !ok || span.Family != FontMonospace || span.Size != 1.0 {
		t.Fatalf("inline code = %+v ok=%v (all: %+v)", span, ok, secs)
	}
	// every byte of the fence lines is covered by some monospace section
	fenceStart := len("text `code` text\n\n")
	for pos := fenceStart; pos < len(src)-1; pos++ {
		if src[pos] == '\n' {
			continue
		}
		covered := false
		for _, s := range secs {
			if s.Family == FontMonospace && int(s.Start) <= pos && pos < int(s.Stop) {
				covered = true
				break
			}
		}
		if !covered {
			t.Fatalf("byte %d (%q) of the fence is not monospace: %+v", pos, src[pos], secs)
		}
	}
	// and no monospace section spans a newline
	for _, s := range secs {
		if s.Family != FontMonospace {
			continue
		}
		for _, b := range src[s.Start:s.Stop] {
			if b == '\n' {
				t.Fatalf("monospace run crosses a line: %+v", s)
			}
		}
	}
}

func TestMarkdownFontSectionsCodeInsideAHeadingTakesTheHeadingSize(t *testing.T) {
	src := "## Use `x`\n"
	secs := fontSectionsOf(t, src)
	span, ok := findSection(secs, len("## Use "), len("## Use `x`"))
	if !ok || span.Family != FontMonospace || span.Size != MarkdownFontScale[1] {
		t.Fatalf("code in heading = %+v ok=%v (all: %+v)", span, ok, secs)
	}
}

func TestMarkdownFontSectionsTableRowsAreMonospace(t *testing.T) {
	src := "| a | b |\n| - | - |\n| 1 | 2 |\n"
	secs := fontSectionsOf(t, src)
	rows := 0
	for _, s := range secs {
		if s.Family == FontMonospace {
			rows++
			if s.Stop-s.Start != uint32(len("| a | b |")) {
				t.Fatalf("a table row should be one run: %+v", s)
			}
		}
	}
	if rows != 3 {
		t.Fatalf("want 3 monospace rows, got %d in %+v", rows, secs)
	}
}

func TestMarkdownFontSectionsAppendsToDst(t *testing.T) {
	seed := []StyledSection{{Start: 0, Stop: 1, Flags: StyleUnderline}}
	secs := MarkdownFontSections("x", markdownhighlight.HighlightLex([]byte("x")), seed)
	if len(secs) < 2 || secs[0] != seed[0] {
		t.Fatalf("dst not preserved: %+v", secs)
	}
}
