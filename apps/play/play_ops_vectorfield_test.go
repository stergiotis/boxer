package play

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/sqlfield"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/flowoverlay"
)

// describedField is a source that only describes itself.
type describedField struct{ meta vectorfield.Meta }

func (inst describedField) Describe() vectorfield.Meta { return inst.meta }
func (inst describedField) Sample(ctx context.Context, req vectorfield.Request) (vectorfield.Window, error) {
	return vectorfield.Window{}, context.Canceled
}

// vectorfieldPane gives the pane a described field of three steps, two of
// them summarised, as its last draw left it.
func vectorfieldPane(t *testing.T, p *PlayApp) time.Time {
	t.Helper()
	t0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	meta := vectorfield.Meta{Name: "wind10m", Unit: "m/s", West: -10, East: 5, South: 40, North: 55, DLon: 0.25, DLat: 0.25,
		SpeedMax: 20, Steps: []vectorfield.Step{
			{Valid: t0, Reference: t0}, {Valid: t0.Add(3 * time.Hour), Reference: t0}, {Valid: t0.Add(6 * time.Hour), Reference: t0.Add(6 * time.Hour)}}}
	d := p.vectorFieldDriver
	d.guest.layer = flowoverlay.New(describedField{meta: meta}, flowoverlay.Options{})
	t.Cleanup(d.guest.layer.Close)
	d.guest.summary = []sqlfield.StepSummary{{Mean: 4, Max: 11, Valid: 30}, {Mean: 6, Max: 15, Valid: 30}}
	d.lastOpts = vectorFieldOpts{name: "Wind at 10 m"}
	d.settledView, d.hasSettled = vectorfield.Request{West: -8, East: 2, South: 44, North: 52}, true
	drawnPane(p, vectorfieldPaneId, 0, nil)
	return t0
}

func TestGetVectorfieldReadsTheFieldAndTheSummary(t *testing.T) {
	l, h := opsLauncher(t)
	vectorfieldPane(t, l.inner)
	r := queryOp[VectorFieldReading](t, h, opGetVectorfield, GetVectorFieldArgs{})
	require.NotNil(t, r.Field)
	assert.Equal(t, "Wind at 10 m", r.Field.Name, "vector_field_opts names it")
	assert.Equal(t, "m/s", r.Field.Unit)
	assert.Equal(t, int32(3), r.Field.Steps)
	assert.Equal(t, []string{"2026-03-01 00:00:00.000", "2026-03-01 06:00:00.000"}, r.Field.Runs)
	require.NotNil(t, r.Step)
	assert.Equal(t, int32(0), *r.Step)
	require.NotNil(t, r.View)
	assert.Equal(t, 44.0, r.View.South)
	require.Len(t, r.Steps, 3)
	assert.Equal(t, 6.0, *r.Steps[1].Mean)
	assert.Equal(t, 15.0, *r.Steps[1].Max)
	assert.Nil(t, r.Steps[2].Mean, "not summarised yet")
	r = queryOp[VectorFieldReading](t, h, opGetVectorfield, GetVectorFieldArgs{Offset: 1, Limit: 1})
	require.Len(t, r.Steps, 1)
	assert.Equal(t, int32(1), r.Steps[0].Index)
	assert.Equal(t, int32(1), r.MoreSteps)
	require.Error(t, queryErr(t, h, opGetVectorfield, GetVectorFieldArgs{Limit: 501}))
}

// A task's time move shows the step at once, and the vf_t the pane writes
// once it rests carries the task as writer; then the pane writes as itself
// again.
func TestSetVectorfieldViewMovesTimeAndHandsOverTheWriter(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	t0 := vectorfieldPane(t, p)
	d := p.vectorFieldDriver
	before := h.ResourceValue(opsResVectorfield)
	step := int32(2)
	require.NoError(t, applyOp(t, h, opSetVectorfieldView, SetVectorFieldViewArgs{Step: &step}))
	assert.Equal(t, 2.0, d.pos)
	assert.Equal(t, "task:t", d.followWriter)
	assert.NotEqual(t, before, h.ResourceValue(opsResVectorfield))
	r := queryOp[VectorFieldReading](t, h, opGetVectorfield, GetVectorFieldArgs{})
	assert.Equal(t, int32(2), *r.Step)

	clock := time.Now()
	d.now = func() time.Time { return clock }
	d.posChangedAt = clock
	meta, _ := d.guest.Meta()
	emits := &recordedEmits{}
	d.emitWhenRested(meta, true, emits)
	assert.Empty(t, emits.names, "not rested yet")
	assert.Equal(t, "task:t", d.followWriter)
	clock = clock.Add(vectorFieldSettle)
	d.emitWhenRested(meta, true, emits)
	require.Equal(t, []SignalID{signalVfT}, emits.names)
	assert.Equal(t, "2026-03-01 06:00:00.000", emits.values[0])
	assert.Empty(t, d.followWriter, "published: the pane writes as itself again")

	require.NoError(t, applyOp(t, h, opSetVectorfieldView, SetVectorFieldViewArgs{Time: t0.Add(4 * time.Hour).Format(time.RFC3339)}))
	assert.InDelta(t, 4.0/3, d.pos, 1e-9, "between the second and third step")
	rate, playing := 2.0, true
	require.NoError(t, applyOp(t, h, opSetVectorfieldView, SetVectorFieldViewArgs{Rate: &rate, Playing: &playing}))
	assert.True(t, d.scrubber.Transport.Playing)
	assert.Equal(t, 2.0, d.scrubber.Transport.Rate)
}

func TestSetVectorfieldViewRefuses(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	vectorfieldPane(t, p)
	step, bad, fast := int32(3), int32(0), 9.0
	require.Error(t, applyOp(t, h, opSetVectorfieldView, SetVectorFieldViewArgs{}))
	require.Error(t, applyOp(t, h, opSetVectorfieldView, SetVectorFieldViewArgs{Step: &step}), "past the last step")
	require.Error(t, applyOp(t, h, opSetVectorfieldView, SetVectorFieldViewArgs{Step: &bad, Time: "2026-03-01"}))
	require.Error(t, applyOp(t, h, opSetVectorfieldView, SetVectorFieldViewArgs{Time: "yesterday"}))
	require.Error(t, applyOp(t, h, opSetVectorfieldView, SetVectorFieldViewArgs{Rate: &fast}))
	err := applyOp(t, h, opSetVectorfieldView, SetVectorFieldViewArgs{FitField: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "show_pane vectorfield", "the map has not laid out")
	require.Error(t, applyOp(t, h, opSetVectorfieldView, SetVectorFieldViewArgs{FitField: true,
		Bounds: &VectorFieldBox{South: 1, North: 2, West: 1, East: 2}}))
	require.Error(t, applyOp(t, h, opSetVectorfieldView, SetVectorFieldViewArgs{Step: &bad, Rate: &fast}))
	assert.Zero(t, p.vectorFieldDriver.pos, "a refused call moves nothing")
	assert.Empty(t, p.vectorFieldDriver.followWriter)
}
