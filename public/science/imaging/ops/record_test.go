package ops

import (
	"context"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

var updateGoldens = flag.Bool("update", false, "rewrite the record goldens under testdata/")

func gray(t *testing.T, p imaging.Plane) imaging.Image {
	t.Helper()
	img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{SourceName: "hand-written"})
	require.NoError(t, err)
	return img
}

// must is v, panicking on err: for planes a test writes by hand.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// goldenRequests are one small request per operator over hand-written
// planes, whose records are the goldens under testdata/.
func goldenRequests(t *testing.T) map[string]Request {
	a := gray(t, must(must(imaging.NewPlaneU8(5, 4, []uint8{
		1, 2, 3, 4, 5,
		6, 255, 8, 9, 10,
		11, 12, 13, 14, 15,
		16, 17, 18, 19, 20,
	})).WithInvalid(255)))
	b := gray(t, must(must(imaging.NewPlaneU16(5, 4, []uint16{
		20, 19, 18, 17, 16,
		15, 14, 13, 12, 11,
		10, 9, 8, 7, 6,
		5, 4, 3, 2, 1,
	})).WithTransform(imaging.ValueTransform{Scale: 2, Offset: -1})))
	nan := float32(math.NaN())
	f := gray(t, must(imaging.NewPlaneF32(4, 3, []float32{
		0, 1, 2, 3,
		4, nan, 6, 7,
		8, 9, 10, 11,
	})))
	e := gray(t, must(must(imaging.NewPlaneI16(4, 4, []int16{
		-5, 3, 3, 9,
		2, -32768, 7, 1,
		4, 4, 6, 0,
		8, 2, 1, 5,
	})).WithInvalid(-32768)))
	return map[string]Request{
		"expr": {Operator: "expr", Params: Params{"expression": Text("(a-b)/(a+b)")}, Inputs: []Input{in("a", a), in("b", b)}},
		"axis-stat": {Operator: "axis-stat", Params: Params{"stat": Text("median")}, Inputs: []Input{
			{Name: "t0", Source: FromImage(a), Positions: []Position{{Axis: "time", Index: 0, Label: "day 0"}}},
			{Name: "t1", Source: FromImage(b), Positions: []Position{{Axis: "time", Index: 1, Label: "day 16"}}},
		}},
		"gaussian":      {Operator: "gaussian", Params: Params{"sigma": Number(0.8), "border": Text("nearest")}, Inputs: []Input{in("f", f)}},
		"median-filter": {Operator: "median-filter", Params: Params{"border": Text("invalid")}, Inputs: []Input{in("e", e)}},
		"standardize":   {Operator: "standardize", Inputs: []Input{in("b", b)}},
	}
}

// inexact are the operators whose samples and quantities are sums of
// products, which an architecture that fuses a multiply and an add rounds
// differently: their goldens pin the document, not the digest.
var inexact = map[string]bool{"gaussian": true, "standardize": true}

func TestRecordGoldensAndRoundTrip(t *testing.T) {
	for op, req := range goldenRequests(t) {
		res, err := Run(context.Background(), req, Options{})
		require.NoError(t, err, op)
		got, err := res.Record.Encode()
		require.NoError(t, err)
		path := filepath.Join("testdata", "record_"+strings.ReplaceAll(op, "-", "_")+".json")
		if *updateGoldens {
			require.NoError(t, os.WriteFile(path, got, 0o644))
		}
		golden, err := os.ReadFile(path)
		require.NoError(t, err, "run with -update to write the goldens")
		rec, err := DecodeRecord(golden)
		require.NoError(t, err, op)
		again, err := rec.Encode()
		require.NoError(t, err)
		require.Equal(t, string(golden), string(again), "%s: the golden decodes and encodes to itself", op)
		if inexact[op] {
			fresh := res.Record
			fresh.Output.Digest = rec.Output.Digest
			for i := range fresh.Quantities {
				require.InEpsilon(t, rec.Quantities[i].Value, fresh.Quantities[i].Value, 1e-12)
				fresh.Quantities[i].Value = rec.Quantities[i].Value
			}
			if got, err = fresh.Encode(); err != nil {
				t.Fatal(err)
			}
		}
		require.Equal(t, string(golden), string(got), "%s: the record is the golden", op)

		replayed, match, err := Replay(context.Background(), rec, req.Inputs, Options{BandRows: 1})
		require.NoError(t, err, op)
		require.Equal(t, rec.Request, replayed.Record.Request, op)
		if !inexact[op] {
			require.True(t, match, "%s: the replay reproduces the recorded digest", op)
		}
		_, match, err = Replay(context.Background(), res.Record, req.Inputs, Options{BandRows: 3})
		require.NoError(t, err, op)
		require.True(t, match, "%s: a replay on this machine reproduces the digest", op)
	}
}

func TestDecodeRecordIsStrict(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("testdata", "record_expr.json"))
	require.NoError(t, err)
	doc := string(golden)
	for name, bad := range map[string]string{
		"unknown member":    strings.Replace(doc, `"operator":`, `"colour": 1, "operator":`, 1),
		"repeated member":   strings.Replace(doc, `"operator": "expr",`, `"operator": "expr", "operator": "expr",`, 1),
		"another schema":    strings.Replace(doc, RecordSchema, "boxer.imaging.ops.record/2", 1),
		"digest upper case": strings.Replace(doc, `"request": "`, `"request": "A`, 1),
		"short digest":      strings.Replace(doc, `"request": "`+doc[strings.Index(doc, `"request": "`)+12:strings.Index(doc, `"request": "`)+14], `"request": "`, 1),
		"wrong type":        strings.Replace(doc, `"version": 1`, `"version": "1"`, 1),
		"object parameter":  strings.Replace(doc, `"expression": "(a - b) / (a + b)"`, `"expression": {}`, 1),
	} {
		require.NotEqual(t, doc, bad, name)
		_, err := DecodeRecord([]byte(bad))
		require.Error(t, err, name)
	}
}

func TestRequestIdentityIsKnownBeforeTheRun(t *testing.T) {
	req := goldenRequests(t)["expr"]
	id, err := RequestIdentity(req)
	require.NoError(t, err)
	res := run(t, req.Operator, req.Params, Options{}, req.Inputs...)
	require.Equal(t, id, res.Record.Request)

	// One expression in two spellings is one request.
	respelled := req
	respelled.Params = Params{"expression": Text("( a - b )/( a + b )")}
	other, err := RequestIdentity(respelled)
	require.NoError(t, err)
	require.Equal(t, id, other)

	// The value transform is not in the digest and is in the identity.
	moved := req
	b := req.Inputs[1].Source.(imageSource).img
	p, err := b.Plane(0).WithTransform(imaging.ValueTransform{Scale: 3, Offset: 0})
	require.NoError(t, err)
	moved.Inputs = []Input{req.Inputs[0], in("b", gray(t, p))}
	other, err = RequestIdentity(moved)
	require.NoError(t, err)
	require.NotEqual(t, id, other)
}

func TestReplayRefusesAnotherInput(t *testing.T) {
	req := goldenRequests(t)["expr"]
	res := run(t, req.Operator, req.Params, Options{}, req.Inputs...)
	_, _, err := Replay(context.Background(), res.Record, []Input{req.Inputs[1], req.Inputs[0]}, Options{})
	require.ErrorIs(t, err, ErrRefused)
	swapped := []Input{{Name: "a", Source: req.Inputs[1].Source}, {Name: "b", Source: req.Inputs[0].Source}}
	_, _, err = Replay(context.Background(), res.Record, swapped, Options{})
	require.ErrorIs(t, err, ErrRefused)
	stale := res.Record
	stale.Version = 7
	_, _, err = Replay(context.Background(), stale, req.Inputs, Options{})
	require.ErrorIs(t, err, ErrRefused)
}

func TestCancelledRunLeavesAFailedStoreAndNoRecord(t *testing.T) {
	img, err := samples.ByName(samples.NameElevation)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var store *tiles.Store
	res, err := Run(ctx, Request{Operator: "gaussian", Params: Params{"sigma": Number(2), "border": Text("nearest")}, Inputs: []Input{in("elevation", img)}},
		Options{BandRows: 7, OnStore: func(s *tiles.Store) { store = s },
			Progress: func(done, total int64) {
				if done >= 7 {
					cancel()
				}
			}})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, res.Store)
	require.Empty(t, res.Record.Operator)
	require.NotNil(t, store, "the store was handed out before the first band")
	require.False(t, store.Finished())
	require.ErrorIs(t, store.Err(), context.Canceled)
	require.ErrorIs(t, store.Wait(context.Background()), context.Canceled)
	_, ok := RecordOf(store)
	require.False(t, ok)
}

func TestTheStoreIsHandedOutBeforeTheFirstBand(t *testing.T) {
	img, err := samples.ByName(samples.NameDouble)
	require.NoError(t, err)
	var rowsWhenHanded int64 = -1
	var done int64
	res := run(t, "median-filter", Params{"border": Text("nearest")},
		Options{BandRows: 50, OnStore: func(s *tiles.Store) { rowsWhenHanded = done },
			Progress: func(d, total int64) { done = d }}, in("double", img))
	require.Equal(t, int64(0), rowsWhenHanded)
	require.True(t, res.Store.Finished())
}

func TestChainCarriesTheFirstRecord(t *testing.T) {
	img, err := samples.ByName(samples.NameElevation)
	require.NoError(t, err)
	first := run(t, "gaussian", Params{"sigma": Number(1.5), "border": Text("invalid")}, Options{}, in("elevation", img))
	// The second operation reads the first's store; its record is found
	// through the store.
	second := run(t, "standardize", nil, Options{}, Input{Name: "smooth", Source: first.Store})
	require.NotNil(t, second.Record.Inputs[0].Record)
	require.Equal(t, first.Record, *second.Record.Inputs[0].Record)
	require.Equal(t, "gaussian", second.Record.Inputs[0].Record.Operator)
	require.Equal(t, first.Record.Output.Digest, second.Record.Inputs[0].Digest)

	got, ok := RecordOf(second.Store)
	require.True(t, ok)
	require.Equal(t, second.Record, got)
	prov := second.Store.Provenance()
	require.Equal(t, "operation", prov.Format)
	require.Equal(t, "standardize@1", prov.Decoder)
	require.Contains(t, prov.Notes[0], "Operation standardize@1 on smooth")

	doc, err := second.Record.Encode()
	require.NoError(t, err)
	back, err := DecodeRecord(doc)
	require.NoError(t, err)
	again, err := back.Encode()
	require.NoError(t, err)
	require.Equal(t, string(doc), string(again))
	require.Equal(t, "elevation", back.Inputs[0].Record.Inputs[0].Name)

	_, match, err := Replay(context.Background(), back, []Input{{Name: "smooth", Source: first.Store}}, Options{BandRows: 33})
	require.NoError(t, err)
	require.True(t, match)

	// A record given for an input that is not its output is refused.
	_, err = Run(context.Background(), Request{Operator: "standardize", Inputs: []Input{{Name: "smooth", Source: FromImage(img), Record: &first.Record}}}, Options{})
	require.ErrorIs(t, err, ErrRefused)
}

func TestAStoreInputGivesWhatTheImageGives(t *testing.T) {
	for _, name := range []string{samples.NameElevation, samples.NameCounts, samples.NameFloat, samples.NameScaled} {
		img, err := samples.ByName(name)
		require.NoError(t, err)
		s, err := tiles.FromImage(context.Background(), img, tiles.Options{})
		require.NoError(t, err)
		params := Params{"radius": Integer(2), "border": Text("invalid")}
		fromImage := run(t, "median-filter", params, Options{BandRows: 37}, in("img", img))
		fromStore := run(t, "median-filter", params, Options{BandRows: 37}, Input{Name: "img", Source: s})
		require.Equal(t, fromImage.Record.Output.Digest, fromStore.Record.Output.Digest, name)
		require.Equal(t, fromImage.Record.Inputs[0].Digest, fromStore.Record.Inputs[0].Digest, name)
	}
}

func TestStackPositionsGoIntoTheRecord(t *testing.T) {
	stack, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	var inputs []Input
	for ti := range 3 {
		b8, err := stack.Image([]int{ti, 0}, []int{3})
		require.NoError(t, err)
		inputs = append(inputs, Input{Name: "B8_t" + string(rune('0'+ti)), Source: FromImage(b8),
			Positions: []Position{{Axis: "time", Index: ti, Label: stack.Label(0, ti)}, {Axis: "band", Index: 3, Label: "B8"}}})
	}
	res := run(t, "axis-stat", Params{"stat": Text("mean")}, Options{}, inputs...)
	require.Equal(t, samples.BandsPlacement, res.Store.Placement())
	require.Equal(t, "day 16", res.Record.Inputs[1].Positions[0].Label)
	require.NotNil(t, res.Record.Inputs[0].Invalid)
	require.Equal(t, float64(samples.BandsInvalid), *res.Record.Inputs[0].Invalid)
	// The invalid strip widens with time: column 7 is covered at day 0
	// only, column 11 at days 0 and 16, column 20 at all three. The mean is
	// of the acquisitions that cover a pixel, as a composite over time
	// leaves gaps out.
	out := readAll(t, res.Store)
	_, ok := out.Plane(0).ValueAt(0, 0)
	require.False(t, ok, "no acquisition covers column 0")
	for col, covered := range map[int]int{7: 1, 11: 2, 20: 3} {
		var sum float64
		n := 0
		for ti := range 3 {
			if v, ok := valueAt(mustImage(t, stack, ti), 0, col, 5); ok {
				sum += v
				n++
			}
		}
		require.Equal(t, covered, n, "column %d", col)
		v, ok := out.Plane(0).ValueAt(col, 5)
		require.True(t, ok)
		require.InDelta(t, sum/float64(n), v, 1e-3, "column %d", col)
	}

	b4, err := stack.Image([]int{1, 0}, []int{2})
	require.NoError(t, err)
	ndvi := run(t, "expr", Params{"expression": Text("(B8 - B4) / (B8 + B4)")}, Options{},
		Input{Name: "B8", Source: inputs[1].Source, Positions: inputs[1].Positions}, in("B4", b4))
	require.Equal(t, []string{"(B8 - B4) / (B8 + B4)"}, ndvi.Store.PlaneLabels())
	water, ok := readAll(t, ndvi.Store).Plane(0).ValueAt(20, 50)
	require.True(t, ok)
	land, ok := readAll(t, ndvi.Store).Plane(0).ValueAt(150, 50)
	require.True(t, ok)
	require.Less(t, water, 0.0)
	require.Greater(t, land, 0.5)
}

func mustImage(t *testing.T, stack imaging.Stack, ti int) imaging.Image {
	img, err := stack.Image([]int{ti, 0}, []int{3})
	require.NoError(t, err)
	return img
}
