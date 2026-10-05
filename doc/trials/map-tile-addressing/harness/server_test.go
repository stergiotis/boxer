//go:build integration

package maptiles

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func reps(def int) int {
	if s := os.Getenv("MTA_REPS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func run(ctx context.Context, q query, settings map[string]string, id string) (result, error) {
	lv := ladder[q.lvl]
	return exec(ctx, rasterTemplateSQL(lv.table, lv.sampling), q.b.params(), settings, "ArrowStream", id)
}

// TestServerCost runs each server-facing arm's issued queries in sequence
// order, no server cache, MTA_REPS times with the arms interleaved per
// repetition, and joins every query with its query_log row (README §3, M1).
func TestServerCost(t *testing.T) {
	runDir(t) // skips unless MTA_RUN_DIR is set: the trial, not the integration lane, runs these
	ctx := context.Background()
	prefix := "mta-cost-" + runTag + "-"
	costArms := []string{"bbox-sd1", "tile1024", "tile512"}
	type rec struct {
		rep int
		q   query
		id  string
		r   result
	}
	var recs []rec
	n := reps(3)
	for rep := 1; rep <= n; rep++ {
		order := append([]string(nil), costArms...)
		// rotate the arm order per repetition
		order = append(order[rep%len(order):], order[:rep%len(order)]...)
		for _, name := range order {
			qs, _ := simulate(armByName(name))
			for _, q := range qs {
				id := fmt.Sprintf("%s%d-%s-%03d", prefix, rep, name, q.n)
				r, err := run(ctx, q, map[string]string{"use_query_cache": "0"}, id)
				if err != nil {
					t.Fatalf("%s: %v", id, err)
				}
				recs = append(recs, rec{rep, q, id, r})
			}
		}
	}
	ql, err := queryLog(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	for _, x := range recs {
		l, ok := ql[x.id]
		if !ok {
			t.Fatalf("no query_log row for %s", x.id)
		}
		rows = append(rows, fmt.Sprintf("%d\t%s\t%s\t%d\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d",
			x.rep, x.q.arm, sequence[x.q.step].name, x.q.lvl, ladder[x.q.lvl].table, x.q.tile,
			l.durUs, l.readRows, l.readBytes, l.resultRows, len(x.r.body), x.r.total.Microseconds(), x.r.ttfb.Microseconds()))
	}
	tsv(t, "cost.tsv", "rep\tarm\tstep\tlevel\ttable\ttile\tserver_us\tread_rows\tread_bytes\tresult_rows\twire_bytes\tclient_us\tclient_ttfb_us", rows)
}

// TestQueryCache runs each arm's issued queries twice in a row with
// use_query_cache=1 under the server's own entry limits, each arm under its
// own query_cache_tag so arms cannot hit each other's entries, and reports
// per query whether it was answered from the cache, plus the entries the
// cache holds afterwards (README §3, M3).
func TestQueryCache(t *testing.T) {
	runDir(t) // skips unless MTA_RUN_DIR is set: the trial, not the integration lane, runs these
	ctx := context.Background()
	prefix := "mta-qc-" + runTag + "-"
	limits, err := plain(ctx, "SELECT name, value FROM system.server_settings WHERE name LIKE 'query_cache.%' FORMAT TSV")
	if err != nil {
		t.Fatal(err)
	}
	ttl, err := plain(ctx, "SELECT value FROM system.settings WHERE name = 'query_cache_ttl'")
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	type qid struct {
		rep int
		q   query
		id  string
		tag string
		d   time.Duration
	}
	var ids []qid
	for _, a := range arms {
		qs, _ := simulate(a)
		tag := "mta-" + runTag + "-" + a.name
		start := time.Now()
		for rep := 1; rep <= 2; rep++ {
			for _, q := range qs {
				id := fmt.Sprintf("%s%d-%s-%03d", prefix, rep, a.name, q.n)
				_, err := run(ctx, q, map[string]string{"use_query_cache": "1", "query_cache_tag": tag}, id)
				if err != nil {
					t.Fatalf("%s: %v", id, err)
				}
				ids = append(ids, qid{rep, q, id, tag, time.Since(start)})
			}
		}
	}
	ql, err := queryLog(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range ids {
		l := ql[x.id]
		rows = append(rows, fmt.Sprintf("%d\t%s\t%s\t%d\t%s\t%d\t%d\t%d\t%d\t%d",
			x.rep, x.q.arm, sequence[x.q.step].name, x.q.lvl, x.q.tile, l.cacheHits, l.cacheMisses, l.durUs, l.resultRows, x.d.Milliseconds()))
	}
	tsv(t, "querycache.tsv", "rep\tarm\tstep\tlevel\ttile\tcache_hits\tcache_misses\tserver_us\tresult_rows\tms_since_arm_start", rows)
	entries, err := plain(ctx, fmt.Sprintf("SELECT tag, result_size, compressed, query FROM system.query_cache WHERE startsWith(tag, 'mta-%s-') FORMAT TSV", runTag))
	if err != nil {
		t.Fatal(err)
	}
	var erows []string
	for _, line := range strings.Split(strings.TrimSpace(entries), "\n") {
		f := strings.SplitN(line, "\t", 4)
		if len(f) < 4 {
			continue
		}
		// The query text carries the substituted vp_* values and the table;
		// keep the table and a hash-free summary.
		tbl := "?"
		for _, lv := range ladder {
			if strings.Contains(f[3], "FROM "+lv.table+"\\n") || strings.Contains(f[3], "FROM "+lv.table+" ") {
				tbl = lv.table
			}
		}
		erows = append(erows, f[0]+"\t"+f[1]+"\t"+f[2]+"\t"+tbl)
	}
	tsv(t, "querycache-entries.tsv", "tag\tresult_size\tcompressed\ttable", erows)
	if err := os.WriteFile(runDir(t)+"/querycache-settings.txt", []byte(limits+"query_cache_ttl\t"+ttl), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestFirstPixels times one view's raster as the bbox query against the same
// view's tiles fetched one after another and six at a time, at the coarsest
// and the full level, no server cache, MTA_REPS repetitions with the modes
// interleaved (README §3, M4). Time is client wall clock from the first
// request: to the first result read whole, and to the last.
func TestFirstPixels(t *testing.T) {
	runDir(t) // skips unless MTA_RUN_DIR is set: the trial, not the integration lane, runs these
	ctx := context.Background()
	views := []int{0, 2} // s01, s03
	n := reps(7)
	var rows []string
	tileSets := map[string]map[int][]query{}
	bboxes := map[int]query{}
	for _, name := range []string{"tile1024", "tile512"} {
		qs, _ := simulate(armByName(name))
		tileSets[name] = map[int][]query{}
		for _, q := range qs {
			if q.lvl == 0 {
				tileSets[name][q.step] = append(tileSets[name][q.step], q)
			}
		}
	}
	bq, _ := simulate(armByName("bbox-sd1"))
	for _, q := range bq {
		if q.lvl == 0 {
			bboxes[q.step] = q
		}
	}
	settings := map[string]string{"use_query_cache": "0"}
	for rep := 1; rep <= n; rep++ {
		for _, st := range views {
			for _, lvl := range []int{0, 2} {
				type mode struct {
					name string
					qs   []query
					conc int
				}
				b := bboxes[st]
				b.lvl = lvl
				modes := []mode{{"bbox-sd1", []query{b}, 1}}
				for _, name := range []string{"tile1024", "tile512"} {
					var ts []query
					for _, q := range tileSets[name][st] {
						q.lvl = lvl
						ts = append(ts, q)
					}
					modes = append(modes, mode{name + "-seq", ts, 1}, mode{name + "-conc6", ts, 6})
				}
				// rotate order per repetition
				k := rep % len(modes)
				modes = append(modes[k:], modes[:k]...)
				for _, m := range modes {
					first, last, err := timeSet(ctx, m.qs, m.conc, settings, fmt.Sprintf("mta-ttfp-%s-%d-%s-", runTag, rep, m.name))
					if err != nil {
						t.Fatal(err)
					}
					rows = append(rows, fmt.Sprintf("%d\t%s\t%d\t%s\t%d\t%d\t%d", rep, sequence[st].name, lvl, m.name, len(m.qs), first.Microseconds(), last.Microseconds()))
				}
			}
		}
	}
	tsv(t, "first-pixels.tsv", "rep\tstep\tlevel\tmode\tqueries\tfirst_us\tcomplete_us", rows)
}

func timeSet(ctx context.Context, qs []query, conc int, settings map[string]string, prefix string) (first, last time.Duration, err error) {
	start := time.Now()
	var mu sync.Mutex
	var done []time.Duration
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	var firstErr error
	for i, q := range qs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, q query) {
			defer wg.Done()
			defer func() { <-sem }()
			_, e := run(ctx, q, settings, fmt.Sprintf("%s%02d", prefix, i))
			mu.Lock()
			defer mu.Unlock()
			if e != nil && firstErr == nil {
				firstErr = e
			}
			done = append(done, time.Since(start))
		}(i, q)
	}
	wg.Wait()
	if firstErr != nil {
		return 0, 0, firstErr
	}
	sort.Slice(done, func(a, b int) bool { return done[a] < done[b] })
	return done[0], done[len(done)-1], nil
}
