package chatview

import (
	"strings"
	"time"
	"unicode"
)

// rowKindE is what one virtual row of the transcript draws.
type rowKindE uint8

const (
	rowDay rowKindE = iota
	rowSystem
	rowMessage
)

// row is one entry of the frame's row plan. msg is the message ordinal for a
// system or message row and the day ordinal (counted from the first drawn
// day) for a day row; dayMS is the day row's time.
type row struct {
	kind  rowKindE
	msg   int32
	dayMS int64
	// first / last mark a message cluster's edges: consecutive messages of
	// one sender within clusterGapMS on one day. The name shows on first,
	// the tail corner on last.
	first, last bool
}

const (
	// clusterGapMS is how far apart two messages of one sender may be and
	// still read as one cluster.
	clusterGapMS  int64 = 5 * 60 * 1000
	defaultWindow       = 300
	olderStep           = 300
)

// plan derives the rows for messages [from, Len). Pure.
func plan(m *Model, loc *time.Location, from int) (rows []row) {
	n := m.Len()
	if from < 0 {
		from = 0
	}
	if from >= n {
		return
	}
	rows = make([]row, 0, n-from+8)
	var (
		lastY, lastD int = -1, -1
		days         int32
		prevMsg      = -1 // ordinal of the previous message row, -1 none
	)
	for i := from; i < n; i++ {
		y, d := dayOf(m.TimeMS[i], loc)
		if y != lastY || d != lastD {
			rows = append(rows, row{kind: rowDay, msg: days, dayMS: m.TimeMS[i]})
			days++
			lastY, lastD = y, d
			prevMsg = -1
		}
		if m.Flags[i]&FlagSystem != 0 || m.Sender[i] < 0 {
			rows = append(rows, row{kind: rowSystem, msg: int32(i)})
			prevMsg = -1
			continue
		}
		r := row{kind: rowMessage, msg: int32(i), first: true, last: true}
		if prevMsg >= 0 && continues(m, prevMsg, i) {
			r.first = false
			rows[len(rows)-1].last = false
		}
		rows = append(rows, r)
		prevMsg = i
	}
	return
}

// continues reports whether message i extends the cluster message p began:
// same sender, within the gap. Callers guarantee p and i are message rows
// on one day.
func continues(m *Model, p, i int) bool {
	return m.Sender[p] == m.Sender[i] && m.TimeMS[i]-m.TimeMS[p] <= clusterGapMS
}

func dayOf(ms int64, loc *time.Location) (year, yday int) {
	t := time.UnixMilli(ms).In(loc)
	return t.Year(), t.YearDay()
}

// chooseLayout resolves LayoutAuto: a dialogue when exactly two participants
// speak and the viewer is one of them, a group otherwise.
func chooseLayout(m *Model, viewer int32, want LayoutE) LayoutE {
	if want != LayoutAuto {
		return want
	}
	if viewer < 0 {
		return LayoutGroup
	}
	var seen [3]int32
	count := 0
	viewerSpeaks := false
	for i := range m.Len() {
		s := m.Sender[i]
		if s < 0 || m.Flags[i]&FlagSystem != 0 {
			continue
		}
		if s == viewer {
			viewerSpeaks = true
		}
		dup := false
		for k := range count {
			if seen[k] == s {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		if count == 2 {
			return LayoutGroup
		}
		seen[count] = s
		count++
	}
	if count == 2 && viewerSpeaks {
		return LayoutDialogue
	}
	return LayoutGroup
}

// initials is the avatar disc's text: the first letter of the first two
// words, upper-cased; "?" for an empty name.
func initials(name string) string {
	var b strings.Builder
	count := 0
	for _, w := range strings.Fields(name) {
		for _, r := range w {
			b.WriteRune(unicode.ToUpper(r))
			count++
			break
		}
		if count == 2 {
			break
		}
	}
	if count == 0 {
		return "?"
	}
	return b.String()
}

// firstLine is the quote strip's excerpt: the body's first line, cut at
// quoteMaxRunes.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	n := 0
	for i := range s {
		if n == quoteMaxRunes {
			return s[:i] + "…"
		}
		n++
	}
	return s
}

const quoteMaxRunes = 80
