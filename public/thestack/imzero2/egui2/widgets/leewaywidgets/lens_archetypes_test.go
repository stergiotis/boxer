package leewaywidgets

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/lwlens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jobsModel is twelve jobs — a numeric attempt and runtime, and a state
// label — beside twelve hosts with a cpu, so clustering has kinds to find. job-00 carries the batch's only extreme runtime, job-10 lacks
// its runtime, job-11 alone is failed. Row order puts the outlier first and
// the rare label last, which is the order an unranked budget would keep.
func jobsModel() *lwlens.Model {
	m := &lwlens.Model{
		Slots: []lwlens.Slot{
			{Section: "num", Member: "attempt", NumericType: true},
			{Section: "num", Member: "runtime", NumericType: true},
			{Section: "sym", Member: "state"},
			{Section: "num", Member: "cpu", NumericType: true},
		},
		Sections: []string{"num", "sym"},
	}
	for i := range 12 {
		r := lwlens.Row{Label: fmt.Sprintf("job-%02d", i)}
		r.Cells = append(r.Cells, lwlens.Cell{Slot: 0, Text: "2", Num: 2, HasNum: true, Arity: 1})
		if i != 10 {
			v := 100 + float64(i)
			if i == 0 {
				v = 9000
			}
			r.Cells = append(r.Cells, lwlens.Cell{Slot: 1, Text: fmt.Sprint(v), Num: v, HasNum: true, Arity: 1})
		}
		state := "running"
		if i == 11 {
			state = "failed"
		}
		r.Cells = append(r.Cells, lwlens.Cell{Slot: 2, Text: state, Arity: 1})
		m.Rows = append(m.Rows, r)
	}
	for i := range 12 {
		v := 10 + float64(i)
		m.Rows = append(m.Rows, lwlens.Row{Label: fmt.Sprintf("host-%02d", i),
			Cells: []lwlens.Cell{{Slot: 3, Text: fmt.Sprint(v), Num: v, HasNum: true, Arity: 1}}})
	}
	return m
}

func TestArchetypeExceptionsRankStructureThenRareLabels(t *testing.T) {
	a, err := lwlens.Analyze(context.Background(), jobsModel(), lwlens.AnalyzeOptions{})
	require.NoError(t, err)
	p := lwlens.PlanRows(&a, lwlens.Intent{Values: 0.5, Stable: 0.5})
	lp := &lensPainter{a: &a, p: &p}
	var exc []exception
	for bi := range p.Bands {
		exc = append(exc, lp.exceptions(&p.Bands[bi], &a.Bands[p.Bands[bi].Band])...)
	}
	require.NotEmpty(t, exc)
	var lines []string
	exc = jobExceptions(a, exc)
	for _, e := range exc {
		lines = append(lines, a.Model.Rows[e.rows[0]].Label+": "+strings.Join(e.parts, " "))
	}
	for i := 1; i < len(exc); i++ {
		assert.LessOrEqual(t, exc[i-1].class, exc[i].class, "most telling first: %v", lines)
	}
	assert.True(t, strings.HasPrefix(lines[0], "job-10: −num·runtime"), "the missing slot leads: %v", lines)
	assert.Contains(t, lines, "job-11: state failed", "a rare label is an exception")
	for _, l := range lines {
		assert.NotContains(t, l, "state running", "the typical label is not")
	}
}

// jobExceptions keeps the jobs band's exceptions: ranking is per band.
func jobExceptions(a lwlens.Analysis, exc []exception) (out []exception) {
	for _, e := range exc {
		if strings.HasPrefix(a.Model.Rows[e.rows[0]].Label, "job-") {
			out = append(out, e)
		}
	}
	return out
}
