package gloss

import "regexp"

// MediaTypeRegexp shows a stored regular expression as one: the inline face
// is the pattern with the verdict Go's RE2 engine gives it, and the block
// face — the host's — is the pattern parsed and highlighted, with an anchor
// that opens a regex explorer tethered to the cell.
//
// A pattern is text like any other text, which is the difficulty: in a grid
// a column of `^(\d{3})-(\d{4})$` reads as noise, and the one question worth
// asking about a stored pattern — does it still compile — cannot be answered
// by looking at it. A rule table, a router's match column and a redaction
// policy all hold patterns nobody re-reads until one of them stops matching.
//
// No affinity. Nothing in the ADR-0182 vocabularies says a text column
// holds a pattern — `sem:machine-readable` covers half the columns in a
// schema — so it binds by alias, by `gloss(…)` or by rule only.
//
// The engine is Go's `regexp`, not the server's. ClickHouse matches with
// libre2 and the two almost always agree; where they do not, the explorer's
// block face carries the tripwire that says so.
const MediaTypeRegexp = "gloss/regexp"

// regexpInlineMaxBytes bounds the pattern the inline face is willing to
// compile.
//
// An inline face has no cache: it runs for every visible cell of the column
// on every frame, and `regexp.Compile` of a pattern of tens of bytes is a
// few microseconds — the cost the Luhn face already set the precedent for.
// Compilation is not linear in the pattern, though, and a kilobyte of
// alternation is not a pattern anybody wrote by hand. Past the bound the
// cell shows the pattern without a verdict rather than paying for one every
// frame; the block face compiles it once.
const regexpInlineMaxBytes = 1 << 10

func regexpGloss() GlossI {
	return &simpleGloss{
		mediaType: MediaTypeRegexp,
		doc:       "an RE2 pattern, highlighted with a tethered explorer (block); the pattern and its compile verdict (inline)",
		accepts:   textLike,
		inline:    regexpFace,
	}
}

// regexpFace is the pattern itself, in the error tone with a ✗ when the
// engine refuses it — the summary widget's compile dot written out, so a
// broken pattern is findable in a column of working ones.
//
// A valid pattern is toneless on purpose. Most stored patterns compile, and
// a column painted green says nothing; the tone is spent on the exception.
//
// The verdict is on the whole pattern, the display on its first line: a
// pattern with a newline in it (an `(?x)`-style layout, or a column holding
// something that is not a pattern at all) must not be judged on the part
// that happens to fit a cell.
func regexpFace(cell CellI) Inline {
	raw := rawOrText(cell)
	if raw == "" {
		return Inline{}
	}
	if len(raw) > regexpInlineMaxBytes {
		return Inline{Text: FirstLine(raw)}
	}
	if _, err := regexp.Compile(raw); err != nil {
		return Inline{Text: FirstLine(raw) + " ✗", Tone: ToneError}
	}
	return Inline{Text: FirstLine(raw)}
}
