//go:build !(js || wasip1)

package widgets

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// rtDemoCols is the column set the demo renders, for the layout the scene's
// line counts are read from.
func rtDemoCols() []recordtable.Column {
	return []recordtable.Column{
		{Arrow: rtColVessel}, {Arrow: rtColSection}, {Arrow: rtColPath},
		{Arrow: rtColValue, Agg: recordtable.AggMean}, {Arrow: rtColUnit},
	}
}

// rtDraw renders a layout as one token per line: "v"/">" and the key for a
// group, the record row for a leaf.
func rtDraw(lay *recordtable.Layout) string {
	var sb strings.Builder
	for _, ln := range lay.Lines {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		if ln.IsGroup() {
			g := lay.Groups[ln.Group]
			if g.Open {
				sb.WriteByte('v')
			} else {
				sb.WriteByte('>')
			}
			switch {
			case g.Key.Null:
				sb.WriteString("~")
			case g.Key.Label != "":
				sb.WriteString(g.Key.Label)
			default:
				sb.WriteString(g.Key.Value)
			}
		} else {
			sb.WriteString(itoa(ln.Row))
		}
	}
	return sb.String()
}

func itoa(v int64) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = digits[v%10]
		v /= 10
	}
	return string(b[i:])
}

// The fixture groups as the demo's description says, and the line counts
// the ADR-0336 M2 scene waits for are the layout's.
func TestRecordTableDemoLayout(t *testing.T) {
	st := newRecordTableDemoState()
	n := readingsRec.NumRows()
	require.EqualValues(t, len(readingsFixture), n)

	// Section, then path: five section groups (cargo, engine, hull, note and
	// the null one, last), the path's proper prefixes nested under each and
	// the leaves named by their last segment, beside a sibling subgroup
	// where the paths differ in depth (hull's length beside draught).
	lay, err := st.groups.LayoutFor(readingsRec, nil, n, rtDemoCols())
	require.NoError(t, err)
	t.Logf("section>path open: %d lines: %s", lay.Len(), rtDraw(lay))
	require.Equal(t, "vcargo vholds v0 15 7 14 21 v1 8 vengine vengines v0 3 12 19 4 13 20 v1 5 6 vhull vdraught 1 10 0 9 17 2 11 18 vnote v~ 16 v~ vremarks 22", rtDraw(lay))
	openLines := lay.Len()

	st.groups.CollapseAll()
	lay, err = st.groups.LayoutFor(readingsRec, nil, n, rtDemoCols())
	require.NoError(t, err)
	t.Logf("section>path collapsed: %d lines: %s", lay.Len(), rtDraw(lay))
	require.Equal(t, ">cargo >engine >hull >note >~", rtDraw(lay))

	// The mean of a section merges up through the paths: hull's draughts
	// and lengths together.
	hull := int32(-1)
	for i := range lay.Groups {
		if lay.Groups[i].Key.Value == "hull" && lay.Groups[i].Depth == 0 {
			hull = int32(i)
		}
	}
	require.GreaterOrEqual(t, hull, int32(0))
	v, ok := lay.Aggregate(hull, 3)
	require.True(t, ok)
	require.EqualValues(t, 8, v.Count)
	require.InDelta(t, (6.2+6.8+142+4.1+4.4+98+2.9+61)/8, func() float64 { m, _ := v.Mean(); return m }(), 1e-9)

	// Reveal opens the ancestors of one row and names its line.
	st.groups.Reveal(st.revealTarget)
	lay, err = st.groups.LayoutFor(readingsRec, nil, n, rtDemoCols())
	require.NoError(t, err)
	t.Logf("section>path revealed: %d lines: %s", lay.Len(), rtDraw(lay))
	require.Equal(t, lay.PosOf(st.revealTarget), lay.Revealed)
	require.Equal(t, ">cargo vengine vengines v0 3 12 19 4 13 20 >1 >hull >note >~", rtDraw(lay))

	// Vessel, then path: the key is the source row, the band its name.
	st.setMode(rtByVesselThenPath)
	st.groups.ExpandAll()
	lay, err = st.groups.LayoutFor(readingsRec, nil, n, rtDemoCols())
	require.NoError(t, err)
	t.Logf("vessel>path open: %d lines: %s", lay.Len(), rtDraw(lay))
	require.True(t, strings.HasPrefix(rtDraw(lay), "vAurora "))
	require.NotEqual(t, openLines, lay.Len())
	st.groups.CollapseAll()
	lay, err = st.groups.LayoutFor(readingsRec, nil, n, rtDemoCols())
	require.NoError(t, err)
	require.Equal(t, ">Aurora >Boreas >Calypso", rtDraw(lay))

	st.setMode(rtUngrouped)
	require.False(t, st.groups.Active())
}
