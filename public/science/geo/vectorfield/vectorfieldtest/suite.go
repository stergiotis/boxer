// Package vectorfieldtest is the conformance suite of the vectorfield
// contract (ADR-0249 SD1): every SourceI implementation runs it against
// itself. It checks the form of what a source returns — the gutter, node
// registration, one spelling of missing, the request as the bound on size,
// wrap as the source's property — and cannot check a source's meteorology:
// whether components were rotated correctly out of a native grid is visible
// only against a fixture whose answer is known.
package vectorfieldtest

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
)

// Options says what the suite may assume of the source under test.
type Options struct {
	// Covered says the source has a valid sample everywhere inside its
	// extent, so a missing one inside it is a defect and not a coast.
	Covered bool
	// MissingStep is a step the source lists and cannot serve, or -1.
	MissingStep int
}

// Run checks src against the contract.
func Run(t *testing.T, src vectorfield.SourceI, opts Options) {
	t.Helper()
	meta := src.Describe()
	require.NotEmpty(t, meta.Steps, "a source lists at least one step")
	require.Greater(t, meta.DLon, 0.0)
	require.Greater(t, meta.DLat, 0.0)
	require.Less(t, meta.West, meta.East)
	require.Less(t, meta.South, meta.North)
	require.Greater(t, meta.ValidFraction, float32(0))

	step := 0
	if step == opts.MissingStep {
		step = 1
	}
	midLon := (meta.West + meta.East) / 2
	midLat := (meta.South + meta.North) / 2
	spanLon := meta.East - meta.West
	spanLat := meta.North - meta.South

	type namedRequest struct {
		name string
		req  vectorfield.Request
	}
	requests := []namedRequest{
		{"whole extent, coarse", vectorfield.Request{West: meta.West, East: meta.East, South: meta.South, North: meta.North, Step: step, MaxCols: 24, MaxRows: 16}},
		{"whole extent, fine", vectorfield.Request{West: meta.West, East: meta.East, South: meta.South, North: meta.North, Step: step, MaxCols: 4096, MaxRows: 4096}},
		{"interior", vectorfield.Request{West: midLon - spanLon/8, East: midLon + spanLon/8, South: midLat - spanLat/8, North: midLat + spanLat/8, Step: step, MaxCols: 64, MaxRows: 48}},
		{"interior, off the grid", vectorfield.Request{West: midLon - spanLon/7.3, East: midLon + spanLon/9.1, South: midLat - spanLat/6.7, North: midLat + spanLat/11.3, Step: step, MaxCols: 37, MaxRows: 29}},
		{"two by two", vectorfield.Request{West: midLon - spanLon/4, East: midLon + spanLon/4, South: midLat - spanLat/4, North: midLat + spanLat/4, Step: step, MaxCols: 2, MaxRows: 2}},
	}
	if meta.PeriodicLon {
		requests = append(requests,
			namedRequest{"across the antimeridian", vectorfield.Request{West: 150, East: 210, South: midLat - spanLat/8, North: midLat + spanLat/8, Step: step, MaxCols: 80, MaxRows: 40}},
			namedRequest{"across the antimeridian, westward frame", vectorfield.Request{West: -200, East: -160, South: midLat - spanLat/8, North: midLat + spanLat/8, Step: step, MaxCols: 80, MaxRows: 40}},
			namedRequest{"across the source's own seam", vectorfield.Request{West: meta.East - 20, East: meta.East + 20, South: midLat - spanLat/8, North: midLat + spanLat/8, Step: step, MaxCols: 80, MaxRows: 40}},
			namedRequest{"a full turn", vectorfield.Request{West: -180, East: 180, South: meta.South, North: meta.North, Step: step, MaxCols: 90, MaxRows: 45}},
		)
	} else {
		requests = append(requests,
			namedRequest{"past the east edge", vectorfield.Request{West: meta.East - spanLon/8, East: meta.East + spanLon/2, South: midLat - spanLat/8, North: midLat + spanLat/8, Step: step, MaxCols: 64, MaxRows: 32}},
			namedRequest{"past the north-west corner", vectorfield.Request{West: meta.West - spanLon/2, East: meta.West + spanLon/8, South: meta.North - spanLat/8, North: meta.North + spanLat/2, Step: step, MaxCols: 64, MaxRows: 32}},
		)
	}

	ctx := context.Background()
	for _, nr := range requests {
		t.Run(nr.name, func(t *testing.T) {
			win, err := src.SampleE(ctx, nr.req)
			require.NoError(t, err)
			checkWindow(t, meta, nr.req, &win, opts)

			again, err := src.SampleE(ctx, nr.req)
			require.NoError(t, err)
			require.Equal(t, win.Version, again.Version, "the same request has the same version")
			require.Equal(t, win.Cols, again.Cols)
			require.Equal(t, win.Rows, again.Rows)
		})
	}

	if !meta.PeriodicLon {
		t.Run("wholly outside a regional field", func(t *testing.T) {
			win, err := src.SampleE(ctx, vectorfield.Request{
				West: meta.East + spanLon, East: meta.East + 2*spanLon,
				South: midLat - spanLat/8, North: midLat + spanLat/8,
				Step: step, MaxCols: 32, MaxRows: 32,
			})
			require.NoError(t, err)
			for i := range win.U {
				require.True(t, isNaN(win.U[i]), "a regional field has nothing beyond its extent and never wraps")
			}
		})
	}

	t.Run("a step that is not listed", func(t *testing.T) {
		for _, bad := range []int{-1, len(meta.Steps)} {
			_, err := src.SampleE(ctx, vectorfield.Request{West: meta.West, East: meta.East, South: meta.South, North: meta.North, Step: bad, MaxCols: 8, MaxRows: 8})
			require.Error(t, err)
			require.True(t, errors.Is(err, vectorfield.ErrStepOutOfRange), "an unlisted step is ErrStepOutOfRange, got %v", err)
		}
	})

	if opts.MissingStep >= 0 {
		t.Run("a listed step that is missing", func(t *testing.T) {
			_, err := src.SampleE(ctx, vectorfield.Request{West: meta.West, East: meta.East, South: meta.South, North: meta.North, Step: opts.MissingStep, MaxCols: 8, MaxRows: 8})
			require.Error(t, err)
			require.True(t, errors.Is(err, vectorfield.ErrStepMissing), "a missing step is an error and not an empty window, got %v", err)
		})
	}

	t.Run("a malformed request", func(t *testing.T) {
		_, err := src.SampleE(ctx, vectorfield.Request{West: 10, East: 10, South: 0, North: 1, Step: step, MaxCols: 8, MaxRows: 8})
		require.Error(t, err)
		_, err = src.SampleE(ctx, vectorfield.Request{West: 0, East: 1, South: 0, North: 1, Step: step, MaxCols: 1, MaxRows: 8})
		require.Error(t, err)
	})
}

func checkWindow(t *testing.T, meta vectorfield.Meta, req vectorfield.Request, win *vectorfield.Window, opts Options) {
	t.Helper()
	if win.IsEmpty() {
		require.False(t, meta.PeriodicLon && req.North > meta.South && req.South < meta.North,
			"a periodic source has data at every longitude")
		return
	}
	n := win.Cols * win.Rows
	require.Len(t, win.U, n)
	require.Len(t, win.V, n)
	require.Len(t, win.Speed, n)
	require.Greater(t, win.DLon, 0.0)
	require.Greater(t, win.DLat, 0.0)
	require.Equal(t, req.Step, win.Step)

	// One spelling of missing, in all planes or in none; and the scalar mean
	// is never shorter than the vector mean.
	for i := range n {
		u, v, s := win.U[i], win.V[i], win.Speed[i]
		require.False(t, isInf(u) || isInf(v) || isInf(s), "no infinities, no sentinels")
		if isNaN(u) || isNaN(v) || isNaN(s) {
			require.True(t, isNaN(u) && isNaN(v) && isNaN(s), "missing is NaN in every plane together (sample %d)", i)
			continue
		}
		require.GreaterOrEqual(t, float64(s)*(1+1e-4)+1e-6, math.Hypot(float64(u), float64(v)),
			"the scalar mean is never shorter than the vector mean (sample %d)", i)
	}

	// The request bounds the size: count what falls strictly inside it.
	inCols, inRows := 0, 0
	for c := range win.Cols {
		if lon := win.LonAt(c); lon > req.West && lon < req.East {
			inCols++
		}
	}
	for r := range win.Rows {
		if lat := win.LatAt(r); lat > req.South && lat < req.North {
			inRows++
		}
	}
	require.LessOrEqual(t, inCols, req.MaxCols, "no more columns inside the bounds than asked")
	require.LessOrEqual(t, inRows, req.MaxRows, "no more rows inside the bounds than asked")

	// Node registration: a sample stands inside the field's extent, give or
	// take the area a coarse sample stands for — never half a cell outside
	// because bounds were read as pixel edges.
	require.LessOrEqual(t, win.North, meta.North+win.DLat)
	require.GreaterOrEqual(t, win.South(), meta.South-win.DLat)
	if !meta.PeriodicLon {
		require.GreaterOrEqual(t, win.West, meta.West-win.DLon)
		require.LessOrEqual(t, win.East(), meta.East+win.DLon)
	}

	// The gutter: every position inside the requested bounds, clipped to
	// where the data is, has its four neighbours in the window.
	west, east := req.West, req.East
	if !meta.PeriodicLon {
		west, east = math.Max(west, meta.West+win.DLon), math.Min(east, meta.East-win.DLon)
	}
	south, north := math.Max(req.South, meta.South+win.DLat), math.Min(req.North, meta.North-win.DLat)
	if west < east && south < north {
		for _, p := range [][2]float64{{west, north}, {east, north}, {west, south}, {east, south}, {(west + east) / 2, (south + north) / 2}} {
			require.True(t, win.Covers(p[0], p[1]), "the window covers (%v, %v) of the request with its neighbours", p[0], p[1])
			if opts.Covered {
				_, _, _, ok := win.Sample(p[0], p[1])
				require.True(t, ok, "a covered source has a value at (%v, %v)", p[0], p[1])
			}
		}
	}
	if opts.Covered {
		for i := range n {
			lon, lat := win.LonAt(i%win.Cols), win.LatAt(i/win.Cols)
			inside := lat <= meta.North && lat >= meta.South && (meta.PeriodicLon || (lon >= meta.West && lon <= meta.East))
			if inside {
				require.False(t, isNaN(win.U[i]), "a covered source has no missing sample inside its extent (%v, %v)", lon, lat)
			}
		}
	}
}

func isNaN(f float32) bool { return f != f }

func isInf(f float32) bool { return math.IsInf(float64(f), 0) }
