//go:build llm_generated_opus47

// Package chstats provides the ClickHouse-side counterpart to boxer's
// in-process tdigest. Where tdigest does the streaming sketch in Go,
// chstats builds the SQL fragment that pushes the same computation
// into a ClickHouse server via the native `quantilesTDigest` aggregate
// — and decodes the result back into a letterval.LVLevel ladder
// the boxenplot widget consumes unchanged.
//
// The package is intentionally pure: three small functions, no I/O.
// Callers orchestrate the actual round-trip via their own ClickHouse
// client (chclient for HTTP, chlocalpool for local subprocess, the
// official clickhouse-go driver for native protocol). This keeps
// chstats decoupled from the client choice and from any specific
// row-shape decoding the caller may prefer (FORMAT JSON, JSONEachRow,
// TabSeparated, etc.).
//
// Pushdown is the recommended path when n exceeds the ~10⁶ threshold
// where shipping raw rows to a Go-side tdigest dominates wall time;
// below that, the in-process tdigest is cheaper and avoids the network
// round-trip.
package chstats

import (
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/analytics/stats/letterval"
)

// LVQuantiles returns the deduplicated quantile points needed for the
// letter-value ladder of depths 1..maxDepth, in the order
// BuildLVSelect emits them. Depth 1 contributes the median (0.5);
// each deeper depth k adds the pair (2^-k, 1 - 2^-k).
//
// maxDepth is clamped to [0, letterval.MaxDepth]. Returns nil when
// maxDepth == 0.
func LVQuantiles(maxDepth uint8) (out []float64) {
	if maxDepth == 0 {
		return nil
	}
	if maxDepth > letterval.MaxDepth {
		maxDepth = letterval.MaxDepth
	}
	out = make([]float64, 0, int(maxDepth)*2)
	seen := make(map[float64]struct{}, int(maxDepth)*2)
	add := func(q float64) {
		if _, ok := seen[q]; ok {
			return
		}
		seen[q] = struct{}{}
		out = append(out, q)
	}
	for d := uint8(1); d <= maxDepth; d++ {
		if d == 1 {
			add(0.5)
			continue
		}
		lo := math.Ldexp(1.0, -int(d))
		hi := 1.0 - lo
		add(lo)
		add(hi)
	}
	return
}

// BuildLVSelect emits the SQL fragment
//
//	quantilesTDigest(q1, q2, ...)(valueCol)
//
// using the LVQuantiles ladder. The result is a SELECT-list
// expression, suitable for splicing into the caller's SQL — pair it
// with `count() AS n` in the same SELECT so the observation count
// comes from the same row set as the quantiles.
//
// valueCol is splatted into the SQL verbatim; the caller is
// responsible for any quoting (`"col"` for case-sensitive identifiers,
// or fully-qualified `table.col`). chstats does no SQL escaping.
//
// Returns the empty string when maxDepth == 0.
func BuildLVSelect(valueCol string, maxDepth uint8) (sql string) {
	qs := LVQuantiles(maxDepth)
	if len(qs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.Grow(32 + len(qs)*6 + len(valueCol))
	sb.WriteString("quantilesTDigest(")
	for i, q := range qs {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(strconv.FormatFloat(q, 'g', -1, 64))
	}
	sb.WriteString(")(")
	sb.WriteString(valueCol)
	sb.WriteString(")")
	sql = sb.String()
	return
}

// LevelsFromArray maps a ClickHouse `quantilesTDigest(...)` result
// array — in the same order as LVQuantiles for the same maxDepth —
// plus the source observation count `n` back into the
// []letterval.LVLevel shape the boxenplot widget consumes.
//
// arr is matched by position to LVQuantiles(maxDepth); a too-short
// arr leaves the corresponding LV bounds at zero rather than panicking.
// TailCount is populated analytically from n (Hofmann's `n · 2^-k`),
// not from arr — chstats does not need a separate count() round-trip
// to fill it, only to set the per-depth observation budget.
func LevelsFromArray(arr []float64, n int64, maxDepth uint8) (out []letterval.LVLevel) {
	if maxDepth == 0 {
		return nil
	}
	if maxDepth > letterval.MaxDepth {
		maxDepth = letterval.MaxDepth
	}
	qOrder := LVQuantiles(maxDepth)
	qValue := make(map[float64]float64, len(qOrder))
	for i, q := range qOrder {
		if i >= len(arr) {
			break
		}
		qValue[q] = arr[i]
	}
	out = make([]letterval.LVLevel, 0, maxDepth)
	nF := float64(n)
	for d := uint8(1); d <= maxDepth; d++ {
		var qLow, qHigh float64
		if d == 1 {
			qLow, qHigh = 0.5, 0.5
		} else {
			qLow = math.Ldexp(1.0, -int(d))
			qHigh = 1.0 - qLow
		}
		out = append(out, letterval.LVLevel{
			Depth:      d,
			LowerQ:     qLow,
			UpperQ:     qHigh,
			LowerValue: qValue[qLow],
			UpperValue: qValue[qHigh],
			TailCount:  int64(nF * qLow),
		})
	}
	return
}
