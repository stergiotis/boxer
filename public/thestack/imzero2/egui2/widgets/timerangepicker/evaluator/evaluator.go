//go:build llm_generated_opus47

// Package evaluator runs ClickHouse SQL time-range expressions
// against a runtime-mediated clickhouse-local worker pool. Phase 4
// of ADR-0016's port (see doc/howto/imzero2-time-range-picker-port.md)
// routes through ADR-0028's chlocalbroker cap; the runtime keeps a
// warm pool of pre-spawned workers, so the typical Apply-click cost
// drops from ~50-200 ms (cold fork+exec) to ~few ms (warm hit).
//
// The Phase 2 spawn-per-Eval implementation that called exec.Command
// directly has been retired — this package no longer imports os/exec.
package evaluator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
)

// ErrEvaluatorUnavailable is returned by NewEvaluator when the bus
// client is nil. Bus-side failures (broker not subscribed, cap filter
// rejects the subject) surface from Eval rather than the constructor —
// callers should treat per-Eval errors as the authoritative signal
// that evaluation is currently impossible.
var ErrEvaluatorUnavailable = errors.New("evaluator: bus unavailable")

// Evaluator routes time-range SQL expressions through ADR-0028's
// chlocalbroker cap on ch.local.exec.<poolName>. Cheap to construct;
// safe for concurrent Eval calls (each draws an independent worker
// from the runtime-owned pool). Close is a no-op kept for API
// forward-compatibility — the pool lifecycle belongs to the runtime,
// not to this object.
type Evaluator struct {
	bus      app.BusI
	poolName string
}

// NewEvaluator returns an Evaluator bound to the supplied bus client
// and pool name. The bus client must hold a SubjectFilter with
// CapDirectionPub matching ch.local.exec.<poolName> (or a wildcard
// that covers it). Returns ErrEvaluatorUnavailable on nil bus.
func NewEvaluator(bus app.BusI, poolName string) (inst *Evaluator, err error) {
	if bus == nil {
		err = ErrEvaluatorUnavailable
		return
	}
	if poolName == "" {
		err = eh.Errorf("evaluator: poolName must be non-empty")
		return
	}
	inst = &Evaluator{bus: bus, poolName: poolName}
	return
}

// Close is a no-op.
func (inst *Evaluator) Close() (err error) {
	return
}

const queryTemplate = `WITH toDateTime64('%s', 3, '%s') AS anchor_now
SELECT
  toUnixTimestamp64Milli(CAST((%s) AS DateTime64(3, '%s'))) AS from_ms,
  toUnixTimestamp64Milli(CAST((%s) AS DateTime64(3, '%s'))) AS to_ms`

// Eval evaluates the given from/to ClickHouse SQL expressions against
// the supplied anchor instant interpreted in the picker's selected
// timezone, and returns the resolved epoch-millisecond bounds. The
// anchor is injected as anchor_now (a DateTime64(3, tzName)) into the
// query via a WITH clause so user expressions can reference it freely.
//
// tzID is resolved through the timerangepicker tz catalogue; the IANA
// name is interpolated into the template. The catalogue validates
// names via time.LoadLocation on first sight, so unknown TzID values
// are caught before the broker sees a malformed query.
//
// The broker appends FORMAT TabSeparated; to the SQL; the result is a
// single TSV row with two int64 columns (from_ms, to_ms).
func (inst *Evaluator) Eval(ctx context.Context, anchor time.Time, tzID uint16, fromExpr, toExpr string) (fromMs, toMs int64, err error) {
	tzName, tzErr := timerangepicker.IanaName(tzID)
	if tzErr != nil {
		err = eh.Errorf("evaluator: resolve tz: %w", tzErr)
		return
	}
	anchorStr := anchor.UTC().Format("2006-01-02 15:04:05.000")
	sql := fmt.Sprintf(queryTemplate, anchorStr, tzName, fromExpr, tzName, toExpr, tzName)

	rep, runErr := chlocalbroker.ExecOnPool(ctx, inst.bus, inst.poolName, chlocalbroker.ExecRequest{
		SQL:    sql,
		Format: "TabSeparated",
	})
	if runErr != nil {
		err = eh.Errorf("evaluator: chlocal request: %w", runErr)
		return
	}
	defer func() { _ = rep.Close() }()
	if execErr := rep.Err(); execErr != nil {
		err = eh.Errorf("evaluator: chlocal exec: %w", execErr)
		return
	}
	out, readErr := io.ReadAll(rep)
	if readErr != nil {
		err = eh.Errorf("evaluator: read result: %w", readErr)
		return
	}
	fromMs, toMs, err = parseTSVRow(out)
	if err != nil {
		err = eh.Errorf("evaluator: result %q: %w", string(out), err)
		return
	}
	return
}

func parseTSVRow(out []byte) (fromMs, toMs int64, err error) {
	s := strings.TrimSpace(string(out))
	if s == "" {
		err = eh.Errorf("empty result")
		return
	}
	lines := strings.Split(s, "\n")
	if len(lines) != 1 {
		err = eh.Errorf("expected 1 row, got %d", len(lines))
		return
	}
	cols := strings.Split(lines[0], "\t")
	if len(cols) != 2 {
		err = eh.Errorf("expected 2 columns, got %d", len(cols))
		return
	}
	fromMs, err = strconv.ParseInt(strings.TrimSpace(cols[0]), 10, 64)
	if err != nil {
		err = eh.Errorf("parse from_ms %q: %w", cols[0], err)
		return
	}
	toMs, err = strconv.ParseInt(strings.TrimSpace(cols[1]), 10, 64)
	if err != nil {
		err = eh.Errorf("parse to_ms %q: %w", cols[1], err)
		return
	}
	return
}
