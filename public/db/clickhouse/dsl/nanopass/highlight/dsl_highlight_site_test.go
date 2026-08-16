package highlight_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// at splits a buffer on the caret marker, so the cases below read as what a
// user's screen shows.
const caret = "|"

func at(t *testing.T, buf string) (sql string, off int) {
	t.Helper()
	i := strings.Index(buf, caret)
	require.GreaterOrEqualf(t, i, 0, "case %q has no caret marker", buf)
	sql = buf[:i] + buf[i+len(caret):]
	off = i
	return
}

func siteOf(t *testing.T, buf string) highlight.CaretSite {
	t.Helper()
	sql, off := at(t, buf)
	return highlight.SiteAtIn(sql, off)
}

func calleeOf(s highlight.CaretSite) string {
	f, ok := s.InnerFrame()
	if !ok {
		return ""
	}
	return f.Callee
}

func ordinalOf(s highlight.CaretSite) int {
	f, ok := s.InnerFrame()
	if !ok {
		return -2
	}
	return f.Ordinal
}

// The driving cases, plus every row of the probes page §P3.
func TestSiteAtDrivingCases(t *testing.T) {
	cases := []struct {
		buf     string
		callee  string
		ordinal int
		partial string // PartialText
		full    string // PartialFull; "" means equal to partial
		inLit   bool
		openLit bool
	}{
		{buf: `SELECT LW_COMPONENT('|`, callee: "LW_COMPONENT", ordinal: 0, inLit: true, openLit: true},
		{buf: `SELECT LW_COMPONENT('Sys|`, callee: "LW_COMPONENT", ordinal: 0, partial: "Sys", inLit: true, openLit: true},
		{
			buf: `SELECT LW_COMPONENT('Sys|Mem')`, callee: "LW_COMPONENT", ordinal: 0,
			partial: "Sys", full: "SysMem", inLit: true,
		},
		{
			buf: `SELECT LW_COMPONENT('SysMem|')`, callee: "LW_COMPONENT", ordinal: 0,
			partial: "SysMem", inLit: true,
		},
		{
			buf:    `SELECT tupleElement(LW_COMPONENT('SysMem'), '|`,
			callee: "tupleElement", ordinal: 1, inLit: true, openLit: true,
		},
		{
			buf:    `SELECT tupleElement(LW_COMPONENT('SysMem'), 'Tot|`,
			callee: "tupleElement", ordinal: 1, partial: "Tot", inLit: true, openLit: true,
		},
		{
			buf:    `SELECT LW_COMPONENT('SysMem') AS m, tupleElement(m, '|`,
			callee: "tupleElement", ordinal: 1, inLit: true, openLit: true,
		},
		{buf: `SELECT keelson('|`, callee: "keelson", ordinal: 0, inLit: true, openLit: true},
		{
			buf:    `SELECT LW_GET(x, 'sec', 'memb', 'chan:|`,
			callee: "LW_GET", ordinal: 3, partial: "chan:", inLit: true, openLit: true,
		},
		// A comma or a space inside the open literal is text, not structure.
		{
			buf:    `SELECT LW_COMPONENT('a, b |`,
			callee: "LW_COMPONENT", ordinal: 0, partial: "a, b ", inLit: true, openLit: true,
		},
		// Nested frames: the innermost wins, and the outer is still reported.
		{buf: `SELECT concat(toHour(|`, callee: "toHour", ordinal: 0},
		{buf: `SELECT f(x + (y|`, callee: "", ordinal: 0, partial: "y"},
		// A ordinal counted after a closed sibling's own commas.
		{buf: `SELECT f(g(1, 2), |`, callee: "f", ordinal: 1},
		{buf: `SELECT f(g(1, 2), 3, |`, callee: "f", ordinal: 2},
		// Caret on the open bracket.
		{buf: `SELECT f(|)`, callee: "f", ordinal: 0},
		// Caret at EOF on a bare name.
		{buf: `SELECT LW_COMPO|`, callee: "", ordinal: -2, partial: "LW_COMPO"},
		// Keyword-syntax calls report no ordinal rather than guessing zero.
		{buf: `SELECT CAST(x AS |`, callee: "CAST", ordinal: -1},
		{buf: `SELECT EXTRACT(HOUR FROM |`, callee: "EXTRACT", ordinal: -1},
		// ... but before the keyword the comma count is still meaningful.
		{buf: `SELECT CAST(|`, callee: "CAST", ordinal: 0},
		{buf: `SELECT CAST(x, '|`, callee: "CAST", ordinal: 1, inLit: true, openLit: true},
		// Statement level.
		{buf: `SELECT |`, callee: "", ordinal: -2},
		{buf: `|`, callee: "", ordinal: -2},
	}
	for _, c := range cases {
		t.Run(c.buf, func(t *testing.T) {
			s := siteOf(t, c.buf)
			assert.Equal(t, c.callee, calleeOf(s), "callee")
			assert.Equal(t, c.ordinal, ordinalOf(s), "ordinal")
			assert.Equal(t, c.partial, s.PartialText, "partial text")
			full := c.full
			if full == "" {
				full = c.partial
			}
			assert.Equal(t, full, s.PartialFull, "partial full")
			if c.inLit {
				require.NotNil(t, s.Literal, "expected the caret inside a literal")
				assert.Equal(t, c.openLit, !s.Literal.Terminated(), "literal termination")
			} else {
				assert.Nil(t, s.Literal, "expected the caret outside any literal")
			}
		})
	}
}

func TestSiteAtFrameStack(t *testing.T) {
	s := siteOf(t, `SELECT concat(toHour(ts), lower(|`)
	require.Len(t, s.Frames, 2)
	assert.Equal(t, "lower", s.Frames[0].Callee)
	assert.Equal(t, 0, s.Frames[0].Ordinal)
	assert.Equal(t, "concat", s.Frames[1].Callee)
	assert.Equal(t, 1, s.Frames[1].Ordinal, "the outer frame is on its second argument")
	assert.Equal(t, []byte{'(', '('}, s.Open, "both brackets are open, outermost first")
}

// Args index by ordinal and cover both sides of the caret, because a domain
// may read a sibling either way.
func TestSiteAtArguments(t *testing.T) {
	sql, off := at(t, `SELECT tupleElement(LW_COMPONENT('SysMem'), '|`)
	s := highlight.SiteAtIn(sql, off)
	f, ok := s.InnerFrame()
	require.True(t, ok)
	require.GreaterOrEqual(t, len(f.Args), 1)
	assert.Equal(t, "LW_COMPONENT('SysMem')", sql[f.Args[0].Start:f.Args[0].Stop])

	sql, off = at(t, `SELECT tupleElement(|, 'TotalBytes') FROM t`)
	s = highlight.SiteAtIn(sql, off)
	f, ok = s.InnerFrame()
	require.True(t, ok)
	require.Len(t, f.Args, 2, "the argument after the caret is collected too")
	assert.Equal(t, 0, f.Ordinal)
	assert.Equal(t, "'TotalBytes'", sql[f.Args[1].Start:f.Args[1].Stop])
}

func TestSiteAtMemberAccess(t *testing.T) {
	t.Run("identifier chain", func(t *testing.T) {
		s := siteOf(t, `SELECT m.|  FROM t`)
		require.NotNil(t, s.Member)
		assert.Equal(t, highlight.ReceiverIdent, s.Member.Kind)
		assert.Equal(t, []string{"m"}, s.Member.Chain)
		assert.Empty(t, s.PartialText)
	})
	t.Run("partly typed member", func(t *testing.T) {
		s := siteOf(t, `SELECT m.Tot| FROM t`)
		require.NotNil(t, s.Member)
		assert.Equal(t, []string{"m"}, s.Member.Chain)
		assert.Equal(t, "Tot", s.PartialText)
	})
	t.Run("longer chain", func(t *testing.T) {
		s := siteOf(t, `SELECT db.tbl.| FROM t`)
		require.NotNil(t, s.Member)
		assert.Equal(t, []string{"db", "tbl"}, s.Member.Chain)
	})
	t.Run("a close paren before the dot", func(t *testing.T) {
		sql, off := at(t, `SELECT LW_COMPONENT('SysMem').Tot|`)
		s := highlight.SiteAtIn(sql, off)
		require.NotNil(t, s.Member)
		assert.Equal(t, highlight.ReceiverCall, s.Member.Kind)
		assert.Equal(t, "LW_COMPONENT", s.Member.Callee)
		assert.Equal(t, `LW_COMPONENT('SysMem')`, sql[s.Member.Receiver.Start:s.Member.Receiver.Stop])
		assert.Equal(t, "Tot", s.PartialText)
	})
	t.Run("a parenthesised receiver", func(t *testing.T) {
		s := siteOf(t, `SELECT (a + b).|`)
		require.NotNil(t, s.Member)
		assert.Equal(t, highlight.ReceiverParen, s.Member.Kind)
	})
	t.Run("a dot inside a literal is text", func(t *testing.T) {
		s := siteOf(t, `SELECT f('a.b|`)
		assert.Nil(t, s.Member)
	})
	t.Run("no dot", func(t *testing.T) {
		s := siteOf(t, `SELECT m |`)
		assert.Nil(t, s.Member)
	})
}

func TestSiteAtClause(t *testing.T) {
	cases := []struct{ buf, clause string }{
		{`SELECT a FROM |`, "FROM"},
		{`SELECT |`, "SELECT"},
		{`SELECT a FROM t WHERE |`, "WHERE"},
		{`SELECT a FROM t SETTINGS |`, "SETTINGS"},
		{`SELECT a FROM t JOIN u ON |`, "ON"},
		{`|`, ""},
	}
	for _, c := range cases {
		t.Run(c.buf, func(t *testing.T) {
			assert.Equal(t, c.clause, siteOf(t, c.buf).Clause)
		})
	}
}

// The caret-at-partial-end rule ADR-0190 §SD10's suffix insert depends on.
func TestCaretAtPartialEnd(t *testing.T) {
	assert.True(t, siteOf(t, `SELECT LW_COMPONENT('Sys|`).CaretAtPartialEnd())
	assert.True(t, siteOf(t, `SELECT LW_COMPONENT('SysMem|')`).CaretAtPartialEnd())
	assert.False(t, siteOf(t, `SELECT LW_COMPONENT('Sys|Mem')`).CaretAtPartialEnd())
	assert.False(t, siteOf(t, `SELECT LW_COMPO|NENT`).CaretAtPartialEnd())
}

// SiteAt embeds EntityAt's answer rather than replacing it, so the Docs pane's
// consumer keeps working off the same walk.
func TestSiteEmbedsTheEntity(t *testing.T) {
	s := siteOf(t, `SELECT toHour|(x)`)
	assert.Equal(t, "toHour", s.Entity.Name)
	assert.True(t, s.Entity.Call)
}

func TestSiteAtEmptyAndOutOfRange(t *testing.T) {
	assert.Empty(t, highlight.SiteAtIn("", 0).Frames)
	s := highlight.SiteAtIn("SELECT 1", 999)
	assert.Empty(t, s.Frames)
	assert.Nil(t, s.Literal)
	s = highlight.SiteAtIn("SELECT 1", -5)
	assert.Empty(t, s.Frames)
}
