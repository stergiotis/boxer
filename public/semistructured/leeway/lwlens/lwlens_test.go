package lwlens

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// kindsModel is three record kinds of twelve rows each, over disjoint
// sections, with one row of the first kind missing a slot its kind always
// has and one carrying a slot of another kind.
func kindsModel() *Model {
	m := &Model{}
	slot := func(sec, mem string) int32 {
		for i, s := range m.Slots {
			if s.Section == sec && s.Member == mem {
				return int32(i)
			}
		}
		m.Slots = append(m.Slots, Slot{Section: sec, Member: mem, NumericType: true})
		if !slices.Contains(m.Sections, sec) {
			m.Sections = append(m.Sections, sec)
		}
		return int32(len(m.Slots) - 1)
	}
	kinds := [][][2]string{
		{{"load", "cpu"}, {"load", "mem"}, {"load", "disk"}},
		{{"net", "rx"}, {"net", "tx"}},
		{{"job", "queue"}, {"job", "attempt"}, {"job", "state"}},
	}
	for k, slots := range kinds {
		for i := range 12 {
			r := Row{Label: fmt.Sprintf("k%d-%02d", k, i)}
			for j, sm := range slots {
				if k == 0 && i == 3 && j == 2 {
					continue
				}
				v := float64((i*7+j*3)%11) + float64(k)
				r.Cells = append(r.Cells, Cell{Slot: slot(sm[0], sm[1]), Text: fmt.Sprint(v), Num: v, HasNum: true, Arity: 1})
			}
			if k == 0 && i == 5 {
				r.Cells = append(r.Cells, Cell{Slot: slot("net", "rx"), Text: "1", Num: 1, HasNum: true, Arity: 1})
			}
			slices.SortFunc(r.Cells, func(a, b Cell) int { return int(a.Slot - b.Slot) })
			m.Rows = append(m.Rows, r)
		}
	}
	return m
}

func TestAnalyzeClustersKinds(t *testing.T) {
	a, err := Analyze(context.Background(), kindsModel(), AnalyzeOptions{})
	require.NoError(t, err)
	require.True(t, a.Clustered)
	clusters := 0
	for _, b := range a.Bands {
		if b.Cluster >= 0 {
			clusters++
			require.NotEmpty(t, b.Rule, "every cluster reads as a rule")
		}
		t.Logf("band %d: %d rows, rule %v, precision %.2f", b.Cluster, len(b.Rows), b.Rule, b.Precision)
	}
	require.Equal(t, 3, clusters)
}

func TestPlanScopes(t *testing.T) {
	a, err := Analyze(context.Background(), kindsModel(), AnalyzeOptions{})
	require.NoError(t, err)

	global := PlanRows(&a, Intent{Values: 0, Stable: 1})
	require.Equal(t, ScopeGlobal, global.Scope)
	require.Len(t, global.Frame, len(a.Model.Slots), "at full stability every varying slot is in the frame")

	band := PlanRows(&a, Intent{Values: 0, Stable: 0.4})
	require.Equal(t, ScopeBand, band.Scope)
	for _, pb := range band.Bands {
		require.NotEmpty(t, pb.Frame)
		require.Less(t, len(pb.Frame), len(a.Model.Slots))
	}

	local := PlanRows(&a, Intent{Values: 0, Stable: 0})
	var missing, unexpected int
	for _, pb := range local.Bands {
		for _, pr := range pb.Rows {
			require.Len(t, pr.Own, len(a.Model.Rows[pr.Row].Cells))
			missing += len(pr.Missing)
			unexpected += len(pr.Unexpected)
		}
	}
	require.Equal(t, 1, missing)
	require.Equal(t, 1, unexpected)
}

func TestPlanCollapsesAndHoistsConstants(t *testing.T) {
	m := kindsModel()
	// Every host carries the same os: a constant of its band.
	os := int32(len(m.Slots))
	m.Slots = append(m.Slots, Slot{Section: "sym", Member: "os"})
	m.Sections = append(m.Sections, "sym")
	for i := range 12 {
		m.Rows[i].Cells = append(m.Rows[i].Cells, Cell{Slot: os, Text: "linux", Arity: 1})
	}
	a, err := Analyze(context.Background(), m, AnalyzeOptions{})
	require.NoError(t, err)

	shape := PlanRows(&a, Intent{Values: 0, Stable: 1})
	lines := 0
	for _, pb := range shape.Bands {
		lines += len(pb.Rows)
	}
	// Three kinds, plus the host missing a slot and the host with an extra.
	require.Equal(t, 5, lines, "rows with the same slots are one line at the shape detail")

	gist := PlanRows(&a, Intent{Values: 0.6, Stable: 0.5})
	var hoisted bool
	for _, pb := range gist.Bands {
		if slices.Contains(pb.Constants, os) {
			hoisted = true
			require.NotContains(t, pb.Frame, os)
			for _, pr := range pb.Rows {
				require.NotContains(t, pr.Own, os)
			}
		}
	}
	require.True(t, hoisted, "a slot one value throughout its band is said once, in the band")
}

func TestCommonPrefixCutsAtASeparator(t *testing.T) {
	require.Equal(t, len("watchbill-worker-"), CommonPrefix([]string{"watchbill-worker-host", "watchbill-worker-run-id"}, 4))
	require.Equal(t, 0, CommonPrefix([]string{"abcdef", "abcxyz"}, 4), "no separator, no cut")
	require.Equal(t, 0, CommonPrefix([]string{"run/", "run/x"}, 4), "a text wholly the prefix keeps it")
}
