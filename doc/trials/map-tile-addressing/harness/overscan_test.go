//go:build integration

package maptiles

import (
	"context"
	"fmt"
	"math"
	"testing"
)

// The overscan follow-up (README §7): the pane as built after this trial's
// first runs — a multi-entry raster memo, and a ladder that starts at the
// full table once that answered fast — with and without SD7's margin around
// the view. A settled view inside the box last requested at the same zoom
// sends nothing; otherwise the arm requests the view widened by `margin` of
// its span on every side, at full resolution ((1+2·margin)× the view's
// pixels per side, past the pane's 1024 cap).

type overscanArm struct {
	name   string
	margin float64
}

var overscanArms = []overscanArm{
	{"now-m0", 0},
	{"now-m15", 0.15},
	{"now-m25", 0.25},
	{"now-m40", 0.40},
}

// nudges is a path of small pans, the case a margin is for.
var nudges = []step{
	{name: "n01-zurich-z9", lat: zurich[0], lon: zurich[1], zoom: 9},
	{name: "n02-right-10", panX: viewW / 10},
	{name: "n03-right-10", panX: viewW / 10},
	{name: "n04-right-10", panX: viewW / 10},
	{name: "n05-down-10", panY: viewH / 10},
	{name: "n06-down-10", panY: viewH / 10},
	{name: "n07-left-15", panX: -viewW * 15 / 100},
	{name: "n08-left-15", panX: -viewW * 15 / 100},
}

var overscanPaths = map[string][]step{"trial": sequence, "nudges": nudges}

// widen is b grown by margin of its span on every side, its raster size
// grown to match.
func widen(b box, margin float64) box {
	if margin == 0 {
		return b
	}
	dx := uint64(math.Round(float64(b.maxX-b.minX) * margin))
	dy := uint64(math.Round(float64(b.maxY-b.minY) * margin))
	return box{
		minX: b.minX - min(dx, b.minX), maxX: min(b.maxX+dx, mercUnitMax),
		minY: b.minY - min(dy, b.minY), maxY: min(b.maxY+dy, mercUnitMax),
		w: uint32(math.Round(float64(b.w) * (1 + 2*margin))),
		h: uint32(math.Round(float64(b.h) * (1 + 2*margin))),
	}
}

func (b box) contains(o box) bool {
	return o.minX >= b.minX && o.maxX <= b.maxX && o.minY >= b.minY && o.maxY <= b.maxY
}

// simulateOverscan is the pane as built, over path, with the arm's margin.
func simulateOverscan(a overscanArm, path []step) (qs []query, issuedPerStep []int) {
	v := newView()
	memo := map[string]bool{} // level|box keys landed
	var covered box
	coveredZoom := math.NaN()
	fastStart := false
	for i, s := range path {
		applyStep(v, s)
		v.TakeEvents()
		view := viewBox(v)
		zoom := v.Zoom()
		issued := 0
		if a.margin > 0 && zoom == coveredZoom && covered.contains(view) {
			issuedPerStep = append(issuedPerStep, 0)
			continue
		}
		req := widen(view, a.margin)
		covered, coveredZoom = req, zoom
		start := 0
		if fastStart {
			start = len(ladder) - 1
		}
		// The memo jump: the finest level held for this box.
		for li := len(ladder) - 1; li > start; li-- {
			if memo[fmt.Sprintf("%d|%s", li, req)] {
				start = li
				break
			}
		}
		for li := start; li < len(ladder); li++ {
			key := fmt.Sprintf("%d|%s", li, req)
			if memo[key] {
				continue
			}
			memo[key] = true
			qs = append(qs, query{arm: a.name, step: i, n: len(qs), lvl: li, b: req})
			issued++
		}
		fastStart = true // the local slice's full level answers well within 300 ms
		issuedPerStep = append(issuedPerStep, issued)
	}
	return
}

// TestOverscan runs each arm's issued queries over both paths, no server
// cache, MTA_REPS times with the arms interleaved, and writes overscan.tsv.
func TestOverscan(t *testing.T) {
	runDir(t)
	ctx := context.Background()
	prefix := "mta-overscan-" + runTag + "-"
	var rows []string
	for pathName, path := range overscanPaths {
		for _, a := range overscanArms {
			_, per := simulateOverscan(a, path)
			for i, n := range per {
				rows = append(rows, fmt.Sprintf("count\t%s\t%s\t%s\t%d", pathName, a.name, path[i].name, n))
			}
		}
	}
	tsv(t, "overscan-counts.tsv", "kind\tpath\tarm\tstep\tissued", rows)

	type rec struct {
		rep        int
		path, arm  string
		q          query
		id         string
		r          result
		stepName   string
		reqW, reqH uint32
	}
	var recs []rec
	n := reps(3)
	for rep := 1; rep <= n; rep++ {
		for pathName, path := range overscanPaths {
			order := append([]overscanArm(nil), overscanArms...)
			order = append(order[rep%len(order):], order[:rep%len(order)]...)
			for _, a := range order {
				qs, _ := simulateOverscan(a, path)
				for _, q := range qs {
					id := fmt.Sprintf("%s%d-%s-%s-%03d", prefix, rep, pathName, a.name, q.n)
					r, err := run(ctx, q, map[string]string{"use_query_cache": "0"}, id)
					if err != nil {
						t.Fatalf("%s: %v", id, err)
					}
					recs = append(recs, rec{rep, pathName, a.name, q, id, r, path[q.step].name, q.b.w, q.b.h})
				}
			}
		}
	}
	ql, err := queryLog(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	rows = rows[:0]
	for _, x := range recs {
		l, ok := ql[x.id]
		if !ok {
			t.Fatalf("no query_log row for %s", x.id)
		}
		rows = append(rows, fmt.Sprintf("%d\t%s\t%s\t%s\t%d\t%dx%d\t%d\t%d\t%d\t%d",
			x.rep, x.path, x.arm, x.stepName, x.q.lvl, x.reqW, x.reqH,
			l.durUs, l.readRows, l.resultRows, len(x.r.body)))
	}
	tsv(t, "overscan-cost.tsv", "rep\tpath\tarm\tstep\tlevel\traster\tserver_us\tread_rows\tresult_rows\twire_bytes", rows)
}
