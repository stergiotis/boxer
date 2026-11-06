---
type: reference
audience: contributor
status: draft
---

# Stress document — the constructs the renderers must agree on

This page is one of the corpus documents of the markdown-rendering-quality
trial. It carries nothing a reader needs; it carries the constructs the 2026
rendering review probed by hand, so that every arm renders the same shapes
and a line-level metric has something to measure in each.

## A long paragraph, for the line breaker

The reading column is where most of the time is spent, and the line breaker
decides what the column looks like: how even the right edge is, whether the
last line of a paragraph is a single orphaned word, and how many characters a
line carries before the eye has to travel back. A greedy first-fit breaker
takes each word as it comes and breaks when the next one would not fit, which
is fast and predictable but leaves a ragged edge whose depth depends on
whichever long word happened to arrive at the margin, such as
*incomprehensibilities* or `streamreadaccess.SinkI`, and a paragraph that ends
one word past a line break leaves that word alone on its own line. Hyphenation
narrows the rag at the cost of a hyphen, and an optimising breaker looks at
the paragraph as a whole and distributes the slack across its lines instead of
pushing it all to the last one.

## Inline mixtures

Plain text with **strong**, *emphasis*, ***both***, ~~struck~~, `inline code`,
==highlighted== text, a [short link](https://example.com), a
[link whose label is long enough to wrap across the margin of a narrow column](https://example.com/a/rather/long/path),
a bare URL https://example.com/bare, a footnote marker[^one], a #tag, and
punctuation right after a [link](https://example.com), then a comma. Numerals
in prose: 1,234,567 rows in 0.42 s on 2026-11-05 at 13:07.

[^one]: The footnote's definition, which the native widget shows as a hover
    gloss and the HTML arms render as a list at the end.

## Lists

- A bullet
- A bullet with a nested list
  - nested one
  - nested two, which runs on long enough to wrap when the column is narrow
    enough to force a second line under the marker
    - a third level
- A bullet with a `code span` and a [link](https://example.com)

1. first
2. second
3. third
4. fourth
5. fifth
6. sixth
7. seventh
8. eighth
9. ninth
10. tenth, where the marker grows a digit and the text must still align
11. eleventh

- [x] a done task
- [ ] an open task with **strong** text
- [ ] an open task whose text wraps onto a second line when the column is narrow enough to force it

## A table

| Construct | Native | HTML + CSS | Notes |
| --- | :---: | ---: | --- |
| Task list | glyph | checkbox | neither is interactive |
| Footnote | hover gloss | list at the end | ADR-0255 |
| Callout | framed group | `<div class="callout">` | five families |
| A long cell | this cell holds enough text to wrap inside its column when the column is narrow | short | — |

## Quotes and callouts

> A blockquote with a single paragraph, long enough to wrap once at the
> narrower widths, so that its left bar or frame has two lines to carry.

> [!note] A note callout
> Body line with **strong** and *emphasis*, and a second sentence to wrap.

> [!warning] A warning
> One line.

> [!tip]- A folded tip
> Hidden until unfolded.

## Code

```go
// A fenced block in Go: the monospace column, its line height and its tint.
func measure(lines []line) (cpl float64) {
	for _, l := range lines {
		cpl += float64(l.Chars)
	}
	return cpl / float64(len(lines))
}
```

    An indented code block, four spaces deep.

## Headings at every level

### Third level

Text under a third-level heading.

#### Fourth level

Text under a fourth-level heading.

##### Fifth level

Text under a fifth-level heading.

###### Sixth level

Text under a sixth-level heading, then a rule.

---

The last paragraph, after the rule.
