// Package schemacheck is the one schema check a process runs over the
// stored tables it uses (ADR-0387): each table's live columns and stamp are
// judged against the columns the binary was generated for, the verdicts
// are kept on a board an operator reads as keelson('schema_tables'), and
// every table whose verdict is not ok gets one log line.
//
// The check reports; it does not decide which backend a process uses. A
// subsystem that falls back to memory when its table cannot be opened
// still does, and the verdict says why. BOXER_SCHEMA_STRICT turns any
// verdict other than ok into a boot failure instead.
package schemacheck

import (
	"context"
	"slices"
	"sync"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Strict makes a verdict other than ok a boot failure.
var Strict = env.NewBool(env.Spec{
	Name:        "BOXER_SCHEMA_STRICT",
	Description: "when true, a stored table whose schema verdict is not ok (missing columns, an incompatible type, an absent or unreadable table) fails the process's boot; unset, the verdict is logged and shown in keelson('schema_tables') and the process starts as before",
	Category:    env.CategoryDatabase,
})

// Board holds the latest verdict per table. Safe for concurrent use; the
// zero value is ready.
type Board struct {
	mu       sync.Mutex
	verdicts []recordstore.SchemaVerdict
}

// NewBoard is an empty board.
func NewBoard() *Board { return &Board{} }

// Record keeps vs, each replacing an earlier verdict on the same table
// judged as the same kind. Several kinds may share one table — the
// facts-bound stores all write boxer.facts — and each keeps its row.
func (inst *Board) Record(vs ...recordstore.SchemaVerdict) {
	if inst == nil {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, v := range vs {
		i := slices.IndexFunc(inst.verdicts, func(o recordstore.SchemaVerdict) bool { return o.Table == v.Table && o.Kind == v.Kind })
		if i >= 0 {
			inst.verdicts[i] = v
			continue
		}
		inst.verdicts = append(inst.verdicts, v)
	}
}

// Verdicts is a copy of the board, in the order tables were first recorded.
func (inst *Board) Verdicts() (vs []recordstore.SchemaVerdict) {
	if inst == nil {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return slices.Clone(inst.verdicts)
}

// Check judges every contract through exec, records the verdicts on board
// (nil records nothing), and logs one line per table whose verdict is not
// ok: an error for incompatible or unreadable, a warning otherwise. With
// strict set, any such table is returned as an error naming them all.
func Check(ctx context.Context, exec recordstore.ExecutorI, board *Board, log zerolog.Logger, strict bool, contracts ...recordstore.TableContract) (vs []recordstore.SchemaVerdict, err error) {
	vs = make([]recordstore.SchemaVerdict, 0, len(contracts))
	for _, c := range contracts {
		vs = append(vs, recordstore.JudgeTable(ctx, exec, c))
	}
	board.Record(vs...)
	err = Report(vs, log, strict)
	return
}

// Report logs the verdicts that are not ok and, under strict, returns them
// as one error. It is Check's second half, for a verdict reached elsewhere.
func Report(vs []recordstore.SchemaVerdict, log zerolog.Logger, strict bool) (err error) {
	var bad []string
	for _, v := range vs {
		if v.IsOk() {
			if len(v.Extra) > 0 {
				log.Debug().Str("table", v.Table).Strs("extra", v.Extra).Msg("schema: " + v.Summary())
			}
			continue
		}
		ev := log.Warn()
		if v.Verdict == recordstore.SchemaVerdictIncompatible || v.Verdict == recordstore.SchemaVerdictUnchecked {
			ev = log.Error()
		}
		ev = ev.Str("table", v.Table).Stringer("verdict", v.Verdict).Stringer("stamp", v.Age)
		if len(v.Missing) > 0 {
			ev = ev.Strs("missing", v.Missing)
		}
		ev.Msg("schema: " + v.Summary())
		bad = append(bad, v.Table+" ("+v.Verdict.String()+")")
	}
	if strict && len(bad) > 0 {
		err = eb.Build().Strs("tables", bad).Errorf("schema: stored tables are not ok and BOXER_SCHEMA_STRICT is set")
	}
	return
}
