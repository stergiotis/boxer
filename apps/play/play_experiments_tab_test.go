package play

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExperimentsImplementsTheCatalogue holds the pane to vizeval's sink
// catalogue (ADR-0257 §SD2): every catalogued sink has a reading guide and is
// drawn, and every palette the card declares maps onto an emitter palette. A
// sink added to one side and not the other fails here, not in a scored run.
func TestExperimentsImplementsTheCatalogue(t *testing.T) {
	d := newExperimentsDriver(c.NewWidgetIdStack(), c.NewWidgetIdStack())
	for _, spec := range vizeval.Sinks() {
		headline, _ := sinkGuide(spec.ID)
		assert.NotEmpty(t, headline, "reading guide for %s", spec.ID)

		d.sink = spec.ID
		cand, err := d.candidate()
		require.NoError(t, err, spec.ID)
		assert.Equal(t, spec.Space.Defaults(), cand.Options, "a fresh pane draws %s at its defaults", spec.ID)
		if spec.ID == vizeval.SinkCard {
			continue // driven by prepareCard, not makeSink
		}
		sink, finish := d.makeSink(cand)
		assert.NotNil(t, sink, "makeSink builds %s", spec.ID)
		assert.NotNil(t, finish, spec.ID)
	}
	card, ok := vizeval.SinkByID(vizeval.SinkCard)
	require.True(t, ok)
	for _, o := range card.Space {
		if o.Name != vizeval.OptionPalette {
			continue
		}
		for _, ch := range o.Choices {
			_, mapped := experimentsPalettes[ch]
			assert.True(t, mapped, "palette %s maps onto an emitter palette", ch)
		}
		assert.Len(t, experimentsPalettes, len(o.Choices))
	}
	for _, ch := range vizeval.ChartMarks {
		_, ok := experimentsChartMarks[ch]
		assert.True(t, ok, "mark %s", ch)
	}
	for _, ch := range vizeval.ChartSorts {
		_, ok := experimentsChartSorts[ch]
		assert.True(t, ok, "sort %s", ch)
	}
	for _, ch := range vizeval.ChartColormaps {
		assert.NotEmpty(t, experimentsColormaps[ch], "colormap %s", ch)
	}
}

func TestExperimentsSeed(t *testing.T) {
	d := newExperimentsDriver(c.NewWidgetIdStack(), c.NewWidgetIdStack())
	require.NoError(t, d.applySeed(`{"source":"result","sink":"unicode","options":{"width":96}}`))
	assert.Equal(t, experimentsSourceResult, d.source)
	cand, err := d.candidate()
	require.NoError(t, err)
	assert.Equal(t, vizeval.SinkUnicode, cand.Sink)
	assert.Equal(t, int64(96), cand.Options[vizeval.OptionWidth])

	require.NoError(t, d.applySeed(`{"sink":"card","options":{"palette":"magma"}}`))
	assert.Equal(t, experimentsSourceFixture, d.source, "an absent source is the fixture")
	cand, err = d.candidate()
	require.NoError(t, err)
	assert.Equal(t, "magma", cand.Options[vizeval.OptionPalette])

	for name, seed := range map[string]string{
		"not json":       `{`,
		"unknown member": `{"sink":"card","extra":true}`,
		"unknown sink":   `{"sink":"pie"}`,
		"unknown option": `{"sink":"card","options":{"width":1}}`,
		"out of range":   `{"sink":"unicode","options":{"width":10}}`,
		"unknown source": `{"source":"live","sink":"card"}`,
	} {
		assert.Error(t, d.applySeed(seed), name)
	}
}

func TestExperimentsCapRowsSaysWhenItCuts(t *testing.T) {
	d := newExperimentsDriver(c.NewWidgetIdStack(), c.NewWidgetIdStack())
	spec := d.spec()
	n, notice := d.capRows(spec.RowCap)
	assert.Equal(t, spec.RowCap, n)
	assert.Empty(t, notice)
	n, notice = d.capRows(spec.RowCap + 5)
	assert.Equal(t, spec.RowCap, n)
	assert.Contains(t, notice, "row cap")
}

func TestApplyTabZones(t *testing.T) {
	reg := defaultTabs(&PlayApp{})
	require.NoError(t, reg.ApplyTabZones("*=body, editor=bottom"))
	for _, spec := range reg.all() {
		want := TabZoneBody
		if spec.ID == "editor" {
			want = TabZoneBottom
		}
		assert.Equal(t, want, spec.Zone, "later pairs win over the wildcard: %s", spec.ID)
	}
	assert.Empty(t, reg.byZone(TabZoneTools))

	for name, spec := range map[string]string{
		"unknown zone": "experiments=left",
		"unknown tab":  "nope=body",
		"not a pair":   "experiments",
		"empty id":     "=body",
	} {
		assert.Error(t, defaultTabs(&PlayApp{}).ApplyTabZones(spec), name)
	}
}
