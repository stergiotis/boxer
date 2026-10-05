//go:build integration

package maptiles

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	_ "image/png"
	"math"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// scatterArrow is the pane's read path: ArrowStream IPC (lz4 batches) into a
// zeroed w*h 0xRRGGBBAA buffer, as packSparseRaster does per batch.
func scatterArrow(body []byte, w, h uint32) ([]uint32, error) {
	rd, err := ipc.NewReader(bytes.NewReader(body), ipc.WithAllocator(memory.NewGoAllocator()))
	if err != nil {
		return nil, err
	}
	defer rd.Release()
	n := int(w) * int(h)
	px := make([]uint32, n)
	for rd.Next() {
		rec := rd.RecordBatch()
		pos := rec.Column(0).(*array.Uint32)
		ra := rec.Column(1).(*array.Uint8)
		ga := rec.Column(2).(*array.Uint8)
		ba := rec.Column(3).(*array.Uint8)
		aa := rec.Column(4).(*array.Uint8)
		for i := range int(rec.NumRows()) {
			p := int(pos.Value(i))
			if p >= n {
				continue
			}
			px[p] = uint32(ra.Value(i))<<24 | uint32(ga.Value(i))<<16 | uint32(ba.Value(i))<<8 | uint32(aa.Value(i))
		}
	}
	return px, rd.Err()
}

// decodePNG is portolan's decodeTile (loader.go): image.Decode, a draw into
// NRGBA, and the same 0xRRGGBBAA packing.
func decodePNG(data []byte) ([]uint32, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(nrgba, nrgba.Bounds(), img, b.Min, draw.Src)
	px := make([]uint32, w*h)
	for i := range px {
		o := i * 4
		px[i] = uint32(nrgba.Pix[o])<<24 | uint32(nrgba.Pix[o+1])<<16 | uint32(nrgba.Pix[o+2])<<8 | uint32(nrgba.Pix[o+3])
	}
	return px, nil
}

func medianDecode(k int, f func() error) (time.Duration, error) {
	ds := make([]time.Duration, 0, k)
	for range k {
		s := time.Now()
		if err := f(); err != nil {
			return 0, err
		}
		ds = append(ds, time.Since(s))
	}
	sort.Slice(ds, func(a, b int) bool { return ds[a] < ds[b] })
	return ds[k/2], nil
}

// TestResultShape fetches every distinct tile of the two tile arms at every
// level as sparse Arrow and as ClickHouse's PNG output format, checks the two
// decode to the same pixels, and records bytes, server time and Go decode
// time (README §3, M5). The bbox arm's views are fetched as Arrow for the
// same columns.
func TestResultShape(t *testing.T) {
	runDir(t) // skips unless MTA_RUN_DIR is set: the trial, not the integration lane, runs these
	ctx := context.Background()
	prefix := "mta-shape-" + runTag + "-"
	type item struct {
		arm, step, tile string
		q               query
		idA, idP        string
		a, p            result
		decA, decP      time.Duration
		mismatch        int
	}
	var items []item
	seen := map[string]bool{}
	for _, name := range []string{"bbox-sd1", "tile1024", "tile512"} {
		qs, _ := simulate(armByName(name))
		for _, q := range qs {
			key := fmt.Sprintf("%s|%d|%s", name, q.lvl, q.b)
			if seen[key] {
				continue
			}
			seen[key] = true
			items = append(items, item{arm: name, step: sequence[q.step].name, tile: q.tile, q: q})
		}
	}
	for i := range items {
		it := &items[i]
		lv := ladder[it.q.lvl]
		it.idA = fmt.Sprintf("%sarrow-%04d", prefix, i)
		var err error
		it.a, err = exec(ctx, rasterTemplateSQL(lv.table, lv.sampling), it.q.b.params(), map[string]string{"use_query_cache": "0"}, "ArrowStream", it.idA)
		if err != nil {
			t.Fatal(err)
		}
		pa, err := scatterArrow(it.a.body, it.q.b.w, it.q.b.h)
		if err != nil {
			t.Fatal(err)
		}
		it.decA, _ = medianDecode(5, func() error { _, e := scatterArrow(it.a.body, it.q.b.w, it.q.b.h); return e })
		if it.arm == "bbox-sd1" {
			continue
		}
		it.idP = fmt.Sprintf("%spng-%04d", prefix, i)
		it.p, err = exec(ctx, pngTemplateSQL(lv.table, lv.sampling), it.q.b.params(), map[string]string{
			"use_query_cache":            "0",
			"output_format_image_width":  strconv.Itoa(int(it.q.b.w)),
			"output_format_image_height": strconv.Itoa(int(it.q.b.h)),
		}, "PNG", it.idP)
		if err != nil {
			t.Fatal(err)
		}
		pp, err := decodePNG(it.p.body)
		if err != nil {
			t.Fatal(err)
		}
		if len(pp) != len(pa) {
			t.Fatalf("png %d px against arrow %d", len(pp), len(pa))
		}
		for k := range pa {
			if pa[k] != pp[k] {
				it.mismatch++
			}
		}
		it.decP, _ = medianDecode(5, func() error { _, e := decodePNG(it.p.body); return e })
	}
	ql, err := queryLog(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	for _, it := range items {
		la := ql[it.idA]
		row := fmt.Sprintf("%s\t%s\t%d\t%s\t%d\t%d\t%d\t%d\t%d", it.arm, it.step, it.q.lvl, it.tile, la.resultRows, len(it.a.body), la.durUs, it.decA.Microseconds(), it.q.b.w*it.q.b.h)
		if it.idP != "" {
			lp := ql[it.idP]
			row += fmt.Sprintf("\t%d\t%d\t%d\t%d", len(it.p.body), lp.durUs, it.decP.Microseconds(), it.mismatch)
		} else {
			row += "\t\t\t\t"
		}
		rows = append(rows, row)
	}
	tsv(t, "shape.tsv", "arm\tfirst_step\tlevel\ttile\tnonempty_px\tarrow_bytes\tarrow_server_us\tarrow_decode_us\traster_px\tpng_bytes\tpng_server_us\tpng_decode_us\tpng_vs_arrow_mismatched_px", rows)
}

// density fetches the normaliser's inputs for a box: pos → (total,
// transparency).
func density(ctx context.Context, t *testing.T, b box, lv level, id string) map[uint32][2]float64 {
	t.Helper()
	r, err := exec(ctx, densitySQL(lv.table, lv.sampling), b.params(), map[string]string{"use_query_cache": "0"}, "TSV", id)
	if err != nil {
		t.Fatal(err)
	}
	m := map[uint32][2]float64{}
	for _, line := range strings.Split(strings.TrimSpace(string(r.body)), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			continue
		}
		p, _ := strconv.ParseUint(f[0], 10, 32)
		tot, _ := strconv.ParseFloat(f[1], 64)
		tr, _ := strconv.ParseFloat(f[3], 64)
		m[uint32(p)] = [2]float64{tot, tr}
	}
	return m
}

func arrowRaster(ctx context.Context, t *testing.T, b box, lv level, id string) []uint32 {
	t.Helper()
	r, err := exec(ctx, rasterTemplateSQL(lv.table, lv.sampling), b.params(), map[string]string{"use_query_cache": "0"}, "ArrowStream", id)
	if err != nil {
		t.Fatal(err)
	}
	px, err := scatterArrow(r.body, b.w, b.h)
	if err != nil {
		t.Fatal(err)
	}
	return px
}

// TestBrightness checks the template's normaliser across addressing schemes
// (README §3, M6), at the full level:
//
//	b  two adjacent tiles against one bbox raster spanning both (2048×1024,
//	   past the pane's 1024 cap — a SQL-only check);
//	c  the s03 view's bbox raster against the tiles under it, pixel by pixel
//	   at the view pixel's centre (the grids are not aligned);
//	d  a tile against its four children (a device-resolution zoom offset of
//	   one): mean transparency over the same ground, and per coarse pixel.
func TestBrightness(t *testing.T) {
	runDir(t) // skips unless MTA_RUN_DIR is set: the trial, not the integration lane, runs these
	ctx := context.Background()
	prefix := "mta-bright-" + runTag + "-"
	full := ladder[2]
	var out []string
	var diff, nonEmpty int
	// b
	left := arrowRaster(ctx, t, tileBox(7, 66, 44, 1024), full, prefix+"b-left")
	right := arrowRaster(ctx, t, tileBox(7, 67, 44, 1024), full, prefix+"b-right")
	l, r := tileBox(7, 66, 44, 1024), tileBox(7, 67, 44, 1024)
	span := box{minX: l.minX, maxX: r.maxX, minY: l.minY, maxY: l.maxY, w: 2048, h: 1024}
	both := arrowRaster(ctx, t, span, full, prefix+"b-span")
	for y := range 1024 {
		for x := range 2048 {
			var tv uint32
			if x < 1024 {
				tv = left[y*1024+x]
			} else {
				tv = right[y*1024+x-1024]
			}
			if tv != both[y*2048+x] {
				diff++
			}
			if tv != 0 {
				nonEmpty++
			}
		}
	}
	out = append(out, fmt.Sprintf("b\ttiles 7/66/44+7/67/44 vs one bbox 2048x1024\tnonempty=%d\tdiffering_px=%d", nonEmpty, diff))
	// c
	bq, _ := simulate(armByName("bbox-sd1"))
	out = append(out, compareViewTiles(ctx, t, bq[2*3].b, 7, "c\ts03 view (z9, integer) vs its z7 1024-px tiles, at the view pixel centre", prefix+"c-"))
	for _, z := range []float64{9.4, 9.5} {
		v := portolan.NewView(portolan.ViewOptions{Size: portolan.Point{X: viewW, Y: viewH}})
		v.SetView(portolan.LL(zurich[0], zurich[1]), z)
		tz := int(math.Floor(z+0.5)) - 2 // the pyramid rounds the zoom; 1024-px tiles sit two levels up
		out = append(out, compareViewTiles(ctx, t, viewBox(v), tz,
			fmt.Sprintf("c\tview at zoom %.1f (continuous, as the pane) vs its z%d 1024-px tiles, at the view pixel centre", z, tz),
			fmt.Sprintf("%sc%d-", prefix, int(z*10))))
	}
	// d
	parent := density(ctx, t, tileBox(7, 66, 44, 1024), full, prefix+"d-parent")
	child := map[uint32][2]float64{} // keyed by parent pos; summed counts, mean transparency over non-empty children
	childN := map[uint32]int{}
	var childTrSum float64
	var childPx int
	for dy := range 2 {
		for dx := range 2 {
			cd := density(ctx, t, tileBox(8, 132+dx, 88+dy, 1024), full, fmt.Sprintf("%sd-child-%d%d", prefix, dx, dy))
			for p, v := range cd {
				cx, cy := p%1024+uint32(dx)*1024, p/1024+uint32(dy)*1024
				pp := (cy/2)*1024 + cx/2
				c := child[pp]
				c[0] += v[0]
				c[1] += v[1]
				child[pp] = c
				childN[pp]++
				childTrSum += v[1]
				childPx++
			}
		}
	}
	var parTrSum float64
	var countMismatch int
	var ratioSum float64
	var ratioN int
	for p, v := range parent {
		parTrSum += v[1]
		c := child[p]
		if c[0] != v[0] {
			countMismatch++
		}
		if childN[p] == 4 && v[1] > 0 && v[1] < 1 {
			ratioSum += (c[1] / 4) / v[1]
			ratioN++
		}
	}
	out = append(out, fmt.Sprintf("d\ttile 7/66/44 vs its four children at z8 (zoom offset one)\tparent_nonempty=%d\tchildren_nonempty=%d\tcount_mismatch_px=%d\tmean_transparency_parent=%.4f\tmean_transparency_children=%.4f\tmean_child_over_parent_where_all_four_nonempty_unsaturated=%.4f(n=%d)",
		len(parent), childPx, countMismatch, parTrSum/float64(len(parent)), childTrSum/float64(childPx), ratioSum/float64(ratioN), ratioN))
	tsv(t, "brightness.tsv", "check\twhat\tresult...", out)
}

// compareViewTiles compares a view's bbox raster with the slippy-z 1024-px
// tiles under it: at each view pixel's mercator centre, the normaliser's
// transparency on both sides.
func compareViewTiles(ctx context.Context, t *testing.T, vb box, z int, what, prefix string) string {
	t.Helper()
	full := ladder[2]
	vd := density(ctx, t, vb, full, prefix+"view")
	side := uint64(1) << (32 - z)
	tiles := map[[2]uint64]map[uint32][2]float64{}
	for x := vb.minX / side; x <= (vb.maxX-1)/side; x++ {
		for y := vb.minY / side; y <= (vb.maxY-1)/side; y++ {
			tiles[[2]uint64{x, y}] = density(ctx, t, tileBox(z, int(x), int(y), 1024), full, fmt.Sprintf("%s%d_%d", prefix, x, y))
		}
	}
	var both, onlyView, onlyTile, equal int
	var sumAbs, sumView, sumTile float64
	var ratios []float64
	for py := range uint64(vb.h) {
		for px := range uint64(vb.w) {
			mx := vb.minX + (2*px+1)*(vb.maxX-vb.minX)/(2*uint64(vb.w))
			my := vb.minY + (2*py+1)*(vb.maxY-vb.minY)/(2*uint64(vb.h))
			tx, ty := mx/side, my/side
			tp := uint32((my-ty*side)*1024/side)*1024 + uint32((mx-tx*side)*1024/side)
			tv, tok := tiles[[2]uint64{tx, ty}][tp]
			vv, vok := vd[uint32(py)*vb.w+uint32(px)]
			switch {
			case vok && tok:
				both++
				sumAbs += math.Abs(vv[1] - tv[1])
				sumView += vv[1]
				sumTile += tv[1]
				if vv[1] == tv[1] {
					equal++
				}
				if vv[1] < 1 && tv[1] < 1 {
					ratios = append(ratios, tv[1]/vv[1])
				}
			case vok:
				onlyView++
			case tok:
				onlyTile++
			}
		}
	}
	sort.Float64s(ratios)
	med := math.NaN()
	if len(ratios) > 0 {
		med = ratios[len(ratios)/2]
	}
	return fmt.Sprintf("%s\tview_px_side=%.0f\ttile_px_side=%d\tboth_nonempty=%d\tequal_transparency=%d\tonly_view=%d\tonly_tile=%d\tmean_abs_transparency_diff=%.4f\tmean_view=%.4f\tmean_tile=%.4f\tmedian_tile_over_view_unsaturated=%.4f",
		what, float64(vb.maxX-vb.minX)/float64(vb.w), side/1024, both, equal, onlyView, onlyTile, sumAbs/float64(both), sumView/float64(both), sumTile/float64(both), med)
}
