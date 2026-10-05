package chat

import "strings"

// diffOpE is a line's part in a diff.
type diffOpE uint8

const (
	diffOpSame diffOpE = iota
	diffOpAdd
	diffOpDel
	// diffOpGap stands for unchanged lines left out between two hunks.
	diffOpGap
)

type diffLine struct {
	op   diffOpE
	text string
}

// maxDiffCells bounds the LCS table; past it the diff is the old text
// removed and the new one added, which is still correct, only coarse.
const maxDiffCells = 4_000_000

// diffContext is the unchanged lines kept around a change.
const diffContext = 2

// lineDiff is the line diff of old against new, with unchanged runs longer
// than the context cut to a gap.
func lineDiff(old string, new string) (out []diffLine) {
	a, b := splitLines(old), splitLines(new)
	// Common head and tail are cheap and keep the table small.
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	full := make([]diffLine, 0, len(a)+len(b))
	for _, l := range a[:p] {
		full = append(full, diffLine{op: diffOpSame, text: l})
	}
	full = append(full, lcsDiff(a[p:len(a)-s], b[p:len(b)-s])...)
	for _, l := range a[len(a)-s:] {
		full = append(full, diffLine{op: diffOpSame, text: l})
	}
	return withContext(full)
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func lcsDiff(a []string, b []string) (out []diffLine) {
	if len(a)*len(b) > maxDiffCells {
		for _, l := range a {
			out = append(out, diffLine{op: diffOpDel, text: l})
		}
		for _, l := range b {
			out = append(out, diffLine{op: diffOpAdd, text: l})
		}
		return
	}
	// t[i][j] is the LCS length of a[i:] and b[j:], flattened.
	w := len(b) + 1
	t := make([]int32, (len(a)+1)*w)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i*w+j] = t[(i+1)*w+j+1] + 1
			} else {
				t[i*w+j] = max(t[(i+1)*w+j], t[i*w+j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, diffLine{op: diffOpSame, text: a[i]})
			i++
			j++
		case t[(i+1)*w+j] >= t[i*w+j+1]:
			out = append(out, diffLine{op: diffOpDel, text: a[i]})
			i++
		default:
			out = append(out, diffLine{op: diffOpAdd, text: b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, diffLine{op: diffOpDel, text: a[i]})
	}
	for ; j < len(b); j++ {
		out = append(out, diffLine{op: diffOpAdd, text: b[j]})
	}
	return
}

// withContext keeps diffContext unchanged lines on either side of each
// change and replaces the rest of an unchanged run with one gap.
func withContext(full []diffLine) (out []diffLine) {
	near := make([]bool, len(full))
	for i, l := range full {
		if l.op == diffOpSame {
			continue
		}
		for k := max(0, i-diffContext); k <= min(len(full)-1, i+diffContext); k++ {
			near[k] = true
		}
	}
	for i, l := range full {
		switch {
		case near[i]:
			out = append(out, l)
		case len(out) == 0 || out[len(out)-1].op != diffOpGap:
			out = append(out, diffLine{op: diffOpGap})
		}
	}
	return
}

// diffCounts are the added and removed lines of a diff.
func diffCounts(d []diffLine) (added int, removed int) {
	for _, l := range d {
		switch l.op {
		case diffOpAdd:
			added++
		case diffOpDel:
			removed++
		}
	}
	return
}
