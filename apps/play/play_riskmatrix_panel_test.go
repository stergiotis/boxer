package play

import (
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/impacttolerance"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/riskmatrix"
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

// The impact columns' unit is what their gloss declares: EUR through the face
// and the axis, no unit with no gloss, and a refusal when they disagree.
func TestRiskMatrixImpactUnit(t *testing.T) {
	p := riskMatrixPanel{driver: NewRiskMatrixDriver(nil)}
	accept := func(lo, hi string, inherent ...string) (riskMatrixClaim, string) {
		fields := []arrow.Field{strField("id"), f64Field("p_lo"), f64Field("p_hi"), f64Field(lo), f64Field(hi)}
		if len(inherent) > 0 {
			fields = append(fields, f64Field("inherent_p_lo"), f64Field("inherent_p_hi"), f64Field(inherent[0]), f64Field(inherent[1]))
		}
		claim, reason := p.AcceptForChannel(chMain, kanbanSchema(fields...), sigNone())
		if reason != "" {
			return riskMatrixClaim{}, reason
		}
		return claim.(riskMatrixClaim), ""
	}

	k, reason := accept("impact_lo", "impact_hi")
	require.Empty(t, reason)
	assert.False(t, k.impact.declared())
	assert.Nil(t, k.impact.format(), "no gloss: the widget's plain numbers, and no currency")

	eur := "@gloss/money;unit=EUR"
	k, reason = accept("impact_lo"+eur, "impact_hi"+eur, "inherent_impact_lo"+eur, "inherent_impact_hi"+eur)
	require.Empty(t, reason)
	assert.Equal(t, "EUR", k.impact.unit)
	assert.Equal(t, "€2.5m", k.impact.format()(2.5e6))

	k, reason = accept("impact_lo@gloss/duration;unit=h", "impact_hi@gloss/duration;unit=h")
	require.Empty(t, reason, "any quantity with a unit")
	assert.Equal(t, "1d 1h 00m", k.impact.format()(25))
	k, reason = accept("impact_lo@gloss/length;unit=km", "impact_hi@gloss/length;unit=km;show=km")
	require.Empty(t, reason, "show= is how it reads, not what it holds")
	assert.Equal(t, "km", k.impact.unit)

	_, reason = accept("impact_lo"+eur, "impact_hi@gloss/money;unit=USD")
	assert.Contains(t, reason, "`impact_lo` has gloss/money;unit=EUR")
	assert.Contains(t, reason, "`impact_hi` has gloss/money;unit=USD")
	_, reason = accept("impact_lo"+eur, "impact_hi")
	assert.Contains(t, reason, "`impact_hi` has no unit", "one column naming a unit does not name it for the rest")
	_, reason = accept("impact_lo"+eur, "impact_hi"+eur, "inherent_impact_lo@gloss/money;unit=GBP", "inherent_impact_hi@gloss/money;unit=GBP")
	assert.Contains(t, reason, "`inherent_impact_lo` has gloss/money;unit=GBP")

	_, reason = accept("impact_lo@gloss/money;unit=eur", "impact_hi@gloss/money;unit=eur")
	assert.Contains(t, reason, "does not bind")
	assert.Contains(t, reason, "upper case: EUR", "the catalog's own reason comes through")
	_, reason = accept("impact_lo@gloss/epoch", "impact_hi@gloss/epoch")
	assert.Contains(t, reason, "a moment")

	k, reason = accept("impact_lo@gloss/raw", "impact_hi@gloss/bytes")
	require.Empty(t, reason, "a gloss with no unit parameter says nothing about a unit")
	assert.False(t, k.impact.declared())
}

// The gloss's compact figure and the widget's are separate code, since neither
// package may know the other; a pane showing one beside the other must not
// disagree.
func TestCompactFormattersAgree(t *testing.T) {
	for _, v := range []float64{0, 0.1234, 1, 950, 1250, 999.6e3, 20e3, 2.5e6, 12.5e6, 3e9, -12e3} {
		assert.Equal(t, riskmatrix.FormatCompact(v), gloss.FormatCompactAmount(v), "%v", v)
	}
}

func TestRiskMatrixStatusNamesTheUnit(t *testing.T) {
	d := NewRiskMatrixDriver(nil)
	d.impact, _ = columnUnitOf(f64Field("impact_lo@gloss/money;unit=EUR"))
	assert.Contains(t, d.statusLine(), "impact in EUR")
	d.impact = columnUnit{}
	assert.NotContains(t, d.statusLine(), "impact in")
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

// tolerance and recovery take any gloss/duration unit, each its own, and the
// fold converts both to the widget's hours; what cannot be read as a span is
// refused rather than read as hours.
func TestToleranceTimeColumns(t *testing.T) {
	p := tolerancePanel{driver: NewToleranceDriver(nil)}
	accept := func(tol, rec string) (toleranceClaim, string) {
		claim, reason := p.AcceptForChannel(chMain, kanbanSchema(strField("service"), f64Field(tol), strField("scenario"), f64Field(rec)), sigNone())
		if reason != "" {
			return toleranceClaim{}, reason
		}
		return claim.(toleranceClaim), ""
	}
	k, reason := accept("tolerance@gloss/duration;unit=h", "recovery@gloss/duration;unit=min")
	require.Empty(t, reason, "two units of one quantity are converted")
	assert.Equal(t, 1.0, k.toleranceToHours)
	assert.InDelta(t, 1.0/60, k.recoveryToHours, 1e-12)
	k, reason = accept("tolerance_hours", "recovery@gloss/duration;unit=s")
	require.Empty(t, reason, "the shorthand beside the gloss form")
	assert.Equal(t, 1.0, k.toleranceToHours)
	assert.InDelta(t, 1.0/3600, k.recoveryToHours, 1e-12)
	k, reason = accept("tolerance_hours@gloss/duration;unit=h", "recovery_hours")
	require.Empty(t, reason, "a shorthand that also declares hours")
	assert.Equal(t, 1.0, k.recoveryToHours)
	_, reason = accept("tolerance_hours@gloss/raw", "recovery_hours")
	assert.Empty(t, reason, "a gloss with no unit parameter does not contradict the name")

	_, reason = accept("tolerance", "recovery@gloss/duration;unit=h")
	assert.Contains(t, reason, "`tolerance` has no unit")
	assert.Contains(t, reason, "`tolerance_hours`")
	_, reason = accept("tolerance@gloss/duration;unit=h", "recovery")
	assert.Contains(t, reason, "`recovery` has no unit")
	_, reason = accept("tolerance@gloss/money;unit=EUR", "recovery@gloss/duration;unit=h")
	assert.Contains(t, reason, "must be a span of time")
	assert.Contains(t, reason, "gloss/money;unit=EUR")
	_, reason = accept("tolerance@gloss/duration", "recovery@gloss/duration;unit=h")
	assert.Contains(t, reason, "does not bind")
	_, reason = accept("tolerance_hours@gloss/duration;unit=min", "recovery_hours")
	assert.Contains(t, reason, "says hours in its name")

	_, reason = p.AcceptForChannel(chMain, kanbanSchema(strField("service"), f64Field("tolerance_hours"),
		f64Field("tolerance@gloss/duration;unit=h"), strField("scenario"), f64Field("recovery_hours")), sigNone())
	assert.Contains(t, reason, "Both `tolerance` and `tolerance_hours`")
}

func TestToleranceFoldConvertsUnits(t *testing.T) {
	rec := numRec(t,
		map[string][]string{"service": {"Payroll", "Payroll"}, "scenario": {"Outage", "Fire"}}, []string{"service", "scenario"},
		map[string][]float64{
			"tolerance@gloss/duration;unit=h":  {24, 24},
			"recovery@gloss/duration;unit=min": {90, 1800},
		}, []string{"tolerance@gloss/duration;unit=h", "recovery@gloss/duration;unit=min"},
		false)
	defer rec.Release()
	d := NewToleranceDriver(nil)
	claim, reason := tolerancePanel{driver: d}.AcceptForChannel(chMain, rec.Schema(), sigNone())
	require.Empty(t, reason)
	d.rebuild(rec, rec.Schema(), claim.(toleranceClaim))
	m := d.model
	require.NoError(t, m.Validate())
	assert.Equal(t, []float64{24}, m.ToleranceHours)
	assert.Equal(t, 1.5, m.Recovery(0, 0))
	assert.Equal(t, 30.0, m.Recovery(0, 1))
	assert.Contains(t, d.statusLine(), "1 breach under at least one scenario", "30 h is past 24 h though 1800 is a large number and 24 a small one")
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
