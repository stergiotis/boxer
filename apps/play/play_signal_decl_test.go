package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The signal declaration of ADR-0232 §SD9: name, type and seed in one place,
// read by the chrome's type table, the empty-default rule, a tab's Writes and
// the tab-marks expansion.

// Every name a tab declares it writes must be declared here, or the chrome
// types its row as unknown and the Run gate cannot decide whether it seeds —
// which is the drift the single declaration exists to prevent.
func TestEveryDeclaredWriteIsAReservedSignal(t *testing.T) {
	types := reservedSignalTypes()
	for _, def := range builtinTabDefs {
		spec := TabSpec{ID: def.id, Writes: def.writes}
		for _, w := range declaredWrites(&spec) {
			assert.Contains(t, types, string(w),
				"tab %q writes %q, which no reserved signal declares", spec.ID, w)
		}
	}
}

func TestReservedSignalNamesAreUnique(t *testing.T) {
	seen := make(map[SignalID]struct{}, len(reservedSignals))
	for _, s := range reservedSignals {
		_, dup := seen[s.Name]
		require.False(t, dup, "%q is declared twice", s.Name)
		seen[s.Name] = struct{}{}
	}
	require.Len(t, reservedSignalIndex, len(reservedSignals), "the index lost a row")
}

// The Map's six are the declaration's, not a second list beside it: the
// driver emits through the same names it publishes as Writes.
func TestMapViewportSignalsComeFromTheDeclaration(t *testing.T) {
	require.Equal(t, []SignalID{"vp_min_x", "vp_max_x", "vp_min_y", "vp_max_y", "vp_w", "vp_h"},
		mapViewportSignals, "the emit order is the bbox order")
	require.Equal(t, mapViewportSignals, signalsWrittenBy("map"))
	for _, s := range mapViewportSignals {
		assert.Equal(t, "UInt32", reservedSignalTypes()[string(s)])
		assert.False(t, signalHasSeed(string(s)),
			"a viewport slot has no empty literal, so it must gate the Run")
	}
}

// The seed is what lets a query reading a selection name run before the first
// click, and what keeps a numeric signal gating the Run until its panel has
// written one.
func TestSignalSeeds(t *testing.T) {
	for _, name := range []SignalID{signalSelectionKey, signalSelectionNode, signalSelectionCountry} {
		assert.True(t, signalHasSeed(string(name)),
			"%q means \"nothing selected\" when unset, which is a valid filter", name)
	}
	for _, name := range []SignalID{signalSelection, signalSelectionID, signalTimelineMin, signalTimelineMax} {
		assert.False(t, signalHasSeed(string(name)),
			"%q has no safe empty literal", name)
	}
	assert.False(t, signalHasSeed("not_reserved"),
		"an ordinary signal is not seeded — the Run gate owns it")
}

// A tab with no owned signals gets nothing, rather than every unowned one.
func TestSignalsWrittenByIsScoped(t *testing.T) {
	assert.Nil(t, signalsWrittenBy("table"), "the selection family belongs to no tab")
	assert.Nil(t, signalsWrittenBy("nonexistent"))
	assert.Equal(t, []SignalID{signalTimelineMin, signalTimelineMax}, signalsWrittenBy("timeline"))
}

// The array case of the signal encoder (ADR-0231 §SD8): a multi-selection has
// to reach the server as a ClickHouse array literal on the same param_* wire
// the scalars ride, or `{gv_selection:Array(String)}` cannot substitute.
func TestEncodeSignalValueArrays(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{nil, "[]"},
		{[]string{}, "[]"},
		{[]string{"a"}, "['a']"},
		{[]string{"a", "b"}, "['a','b']"},
		// A declared id is a database value: it may carry the quote and the
		// backslash that would otherwise end the literal early.
		{[]string{"it's"}, `['it\'s']`},
		{[]string{`back\slash`}, `['back\\slash']`},
		{[]string{`both\'`}, `['both\\\'']`},
	} {
		raw, ok := encodeSignalValue(tc.in)
		require.True(t, ok, "%v did not encode", tc.in)
		assert.Equal(t, tc.want, raw)
	}
	// A type the encoder has no literal for is still dropped rather than
	// stored lossily.
	_, ok := encodeSignalValue(map[string]int{"a": 1})
	assert.False(t, ok)
}

// Every gv_* signal is seeded, so a query referencing one runs from the first
// frame — and the seed literal has to suit the declared type, which is the
// bug a single boolean "defaults empty" would have had.
func TestGraphviewSignalsAreSeededWithTypedLiterals(t *testing.T) {
	types := reservedSignalTypes()
	names := signalsWrittenBy("graphview")
	require.NotEmpty(t, names)
	for _, n := range names {
		raw, ok := signalSeedRaw(string(n))
		require.True(t, ok, "%q is not seeded, so a query referencing it would block", n)
		switch typ := types[string(n)]; typ {
		case "String":
			assert.Equal(t, "", raw, "%q", n)
		case "Float64":
			assert.Equal(t, "0", raw, "%q", n)
		case "Array(String)":
			assert.Equal(t, "[]", raw, "%q", n)
		default:
			t.Fatalf("%q has an unhandled declared type %q", n, typ)
		}
	}
}
