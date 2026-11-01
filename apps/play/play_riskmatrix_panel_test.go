package play

import (
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/impacttolerance"
)

// The Risk matrix and Impact tolerance panes (ADR-0122's 2026-11-01 update):
// the named-column contracts, their rejections, and the folds.

// numRec builds a record of string columns then float64 columns; a NaN in a
// float column is a NULL, an empty string in a string column likewise when
// nullStr is set.
func numRec(t *testing.T, strs map[string][]string, strOrder []string, nums map[string][]float64, numOrder []string, nullStr bool) arrow.RecordBatch {
	t.Helper()
	alloc := memory.NewGoAllocator()
	var fields []arrow.Field
	var cols []arrow.Array
	n := 0
	for _, name := range strOrder {
		vs := strs[name]
		n = len(vs)
		b := array.NewStringBuilder(alloc)
		for _, v := range vs {
			if nullStr && v == "" {
				b.AppendNull()
			} else {
				b.Append(v)
			}
		}
		fields = append(fields, arrow.Field{Name: name, Type: arrow.BinaryTypes.String, Nullable: true})
		cols = append(cols, b.NewStringArray())
		b.Release()
	}
	for _, name := range numOrder {
		vs := nums[name]
		n = len(vs)
		b := array.NewFloat64Builder(alloc)
		for _, v := range vs {
			if math.IsNaN(v) {
				b.AppendNull()
			} else {
				b.Append(v)
			}
		}
		fields = append(fields, arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Float64, Nullable: true})
		cols = append(cols, b.NewFloat64Array())
		b.Release()
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(n))
}

func TestRiskMatrixAcceptContract(t *testing.T) {
	p := riskMatrixPanel{driver: NewRiskMatrixDriver(nil)}
	full := kanbanSchema(strField("id"), strField("name"),
		f64Field("p_lo"), f64Field("p_hi"), f64Field("impact_lo"), f64Field("impact_hi"))
	claim, reason := p.AcceptForChannel(chMain, full, sigNone())
	require.Empty(t, reason)
	k := claim.(riskMatrixClaim)
	assert.Equal(t, [4]int{-1, -1, -1, -1}, k.inherent, "no inherent columns is a register without them")

	_, reason = p.AcceptForChannel(chMain, kanbanSchema(strField("id"), f64Field("p_lo")), sigNone())
	assert.Contains(t, reason, "`p_hi`")
	assert.Contains(t, reason, "`impact_lo`")
	assert.NotContains(t, reason, "`name`", "name is optional")

	_, reason = p.AcceptForChannel(chMain, kanbanSchema(strField("id"),
		strField("p_lo"), f64Field("p_hi"), f64Field("impact_lo"), f64Field("impact_hi")), sigNone())
	assert.Contains(t, reason, "must be numeric")

	_, reason = p.AcceptForChannel(chMain, kanbanSchema(strField("id"),
		f64Field("p_lo"), f64Field("p_hi"), f64Field("impact_lo"), f64Field("impact_hi"),
		f64Field("inherent_p_lo")), sigNone())
	assert.Contains(t, reason, "all four columns or none")
	assert.Contains(t, reason, "`inherent_impact_hi`")

	claim, reason = p.AcceptForChannel(chMain, kanbanSchema(i32Field("id"),
		i32Field("p_lo"), f64Field("p_hi"), u64Field("impact_lo"), u64Field("impact_hi"),
		f64Field("inherent_p_lo"), f64Field("inherent_p_hi"), f64Field("inherent_impact_lo"), f64Field("inherent_impact_hi")), sigNone())
	require.Empty(t, reason, "integers are numbers, and id carries no type requirement")
	assert.GreaterOrEqual(t, claim.(riskMatrixClaim).inherent[3], 0)
}

func TestRiskMatrixFold(t *testing.T) {
	nan := math.NaN()
	rec := numRec(t,
		map[string][]string{"id": {"R1", "R2", "R3"}, "name": {"Ransomware", "Bad row", "Fire"}}, []string{"id", "name"},
		map[string][]float64{
			"p_lo": {0.03, 0, 0.01}, "p_hi": {0.15, 0.1, 0.002},
			"impact_lo": {800e3, 1, 2e6}, "impact_hi": {12e6, 2, 40e6},
			"inherent_p_lo": {0.1, nan, nan}, "inherent_p_hi": {0.35, nan, nan},
			"inherent_impact_lo": {1.5e6, nan, nan}, "inherent_impact_hi": {25e6, nan, nan},
		},
		[]string{"p_lo", "p_hi", "impact_lo", "impact_hi", "inherent_p_lo", "inherent_p_hi", "inherent_impact_lo", "inherent_impact_hi"},
		false)
	defer rec.Release()
	d := NewRiskMatrixDriver(nil)
	claim, reason := riskMatrixPanel{driver: d}.AcceptForChannel(chMain, rec.Schema(), sigNone())
	require.Empty(t, reason)
	d.rebuild(rec, rec.Schema(), claim.(riskMatrixClaim))

	m := d.model
	require.NoError(t, m.Validate())
	assert.Equal(t, []string{"R1", "R3"}, m.Ids, "a zero likelihood cannot sit on a log axis")
	assert.EqualValues(t, 1, d.skipped)
	assert.Equal(t, []int64{0, 2}, d.rows, "model index → result row")
	assert.Equal(t, 0.002, m.PLo[1], "a range written high to low is read as a range")
	assert.Equal(t, 0.01, m.PHi[1])
	assert.True(t, m.HasInherent())
	assert.True(t, math.IsNaN(m.InherentPLo[1]), "a NULL inherent rating is none")
	assert.Contains(t, d.statusLine(), "1 rows skipped")
}

func TestToleranceAcceptContract(t *testing.T) {
	p := tolerancePanel{driver: NewToleranceDriver(nil)}
	_, reason := p.AcceptForChannel(chMain, kanbanSchema(strField("service"), f64Field("tolerance_hours"),
		strField("scenario"), f64Field("recovery_hours")), sigNone())
	require.Empty(t, reason)
	_, reason = p.AcceptForChannel(chMain, kanbanSchema(strField("service"), f64Field("tolerance_hours")), sigNone())
	assert.Contains(t, reason, "`scenario`, `recovery_hours`")
	_, reason = p.AcceptForChannel(chMain, kanbanSchema(strField("service"), strField("tolerance_hours"),
		strField("scenario"), f64Field("recovery_hours")), sigNone())
	assert.Contains(t, reason, "number of hours")
}

func TestToleranceFold(t *testing.T) {
	nan := math.NaN()
	rec := numRec(t,
		map[string][]string{
			"service":  {"Order intake", "Order intake", "Payroll", "Order intake", "Payroll", "Support", "Payroll"},
			"scenario": {"Ransomware", "Outage", "Outage", "Ransomware", "Ransomware", "", "Outage"},
		}, []string{"service", "scenario"},
		map[string][]float64{
			"tolerance_hours": {24, 24, 72, 24, 48, 12, 72},
			"recovery_hours":  {72, 10, 8, 5, nan, nan, 9},
		}, []string{"tolerance_hours", "recovery_hours"},
		true)
	defer rec.Release()
	d := NewToleranceDriver(nil)
	claim, reason := tolerancePanel{driver: d}.AcceptForChannel(chMain, rec.Schema(), sigNone())
	require.Empty(t, reason)
	d.rebuild(rec, rec.Schema(), claim.(toleranceClaim))

	m := d.model
	require.NoError(t, m.Validate())
	assert.Equal(t, []string{"Order intake", "Payroll", "Support"}, m.Services, "first-seen order")
	assert.Equal(t, []string{"Ransomware", "Outage"}, m.Scenarios, "a NULL scenario adds a service, not a scenario")
	assert.Equal(t, []float64{24, 72, 12}, m.ToleranceHours, "the first row's tolerance wins")
	assert.Equal(t, 1, d.conflicts, "Payroll carries two tolerances")
	assert.Equal(t, 2, d.duplicates, "repeated pairs: Order intake/Ransomware and Payroll/Outage")
	assert.Equal(t, 72.0, m.Recovery(0, 0))
	assert.True(t, math.IsNaN(m.Recovery(1, 0)), "a NULL recovery is no estimate")
	assert.True(t, math.IsNaN(m.Recovery(2, 1)))

	p := impacttolerance.Point{Service: 1, Scenario: 1}
	assert.EqualValues(t, 2, d.rowAt[p], "an estimate maps back to its row")
	assert.Equal(t, p, d.pointAt[2])
	assert.Contains(t, d.statusLine(), "1 breach under at least one scenario")
}

// The panes' boxes agree with themselves, like every panel's.
func TestRiskPaneFillsAreCoherent(t *testing.T) {
	for _, f := range []paneFill{riskMatrixPaneFill, tolerancePaneFill} {
		w, h := f.box(0, 0)
		assert.GreaterOrEqual(t, w, f.minW)
		assert.LessOrEqual(t, w, f.maxW)
		assert.GreaterOrEqual(t, h, f.minH)
	}
}
