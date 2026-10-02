package jackstay

import (
	"context"
	"slices"
	"time"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// ExecI is the one method applying DDL needs from a ClickHouse client.
type ExecI interface {
	Exec(ctx context.Context, sql string) (err error)
}

// Restate rebuilds plan's structure section from fresh inventories and lists
// every table whose verdict or DDL is no longer what the plan says. A plan is
// applied only when that list is empty: the operator confirmed the DDL they
// were shown, not whatever the servers now call for (ADR-0259 §SD1, §SD3).
//
// Progress made by the plan's own DDL is not staleness. A run that stopped
// part-way leaves tables whose remaining statements are a subset of the
// plan's, or which moved from create to identical, or from extend to narrower
// or identical. Those are accepted, and fresh carries only the statements
// still to run. fresh is therefore what [ApplyStructure] should be given.
func Restate(ops *common.TableOperations, plan *Plan, src *Inventory, dst *Inventory, now time.Time) (fresh Plan, stale []string, err error) {
	fresh, err = BuildPlan(ops, plan.Source, plan.Target, src, dst, plan.Selection, now)
	if err != nil {
		err = eh.Errorf("unable to rebuild plan: %w", err)
		return
	}
	for _, sql := range fresh.DatabaseDDL {
		if !slices.Contains(plan.DatabaseDDL, sql) {
			stale = append(stale, "database DDL not in the plan: "+sql)
		}
	}
	old := make(map[datacatalog.TableRef]*PlanTable, len(plan.Tables))
	for i := range plan.Tables {
		old[plan.Tables[i].Source] = &plan.Tables[i]
	}
	for i := range fresh.Tables {
		ft := &fresh.Tables[i]
		ot, has := old[ft.Source]
		switch {
		case !has:
			stale = append(stale, ft.Source.String()+": new on the source")
		case ot.Verdict != ft.Verdict && !isProgress(ot, ft):
			stale = append(stale, ft.Source.String()+": verdict "+ot.Verdict.String()+" is now "+ft.Verdict.String())
		case !isSubset(ft.DDL, ot.DDL):
			stale = append(stale, ft.Source.String()+": DDL differs")
		case ot.Filter != ft.Filter:
			stale = append(stale, ft.Source.String()+": filter differs from the selection's")
		}
		delete(old, ft.Source)
	}
	for ref := range old {
		stale = append(stale, ref.String()+": no longer on the source")
	}
	slices.Sort(stale)
	return
}

// isProgress reports whether the verdict moved the way the plan's own DDL
// moves it: a created table is identical to its source, and an extended one
// keeps the target's extra columns.
func isProgress(old *PlanTable, fresh *PlanTable) (ok bool) {
	switch old.Verdict {
	case VerdictCreate:
		return fresh.Verdict == VerdictIdentical
	case VerdictExtend:
		return fresh.Verdict == VerdictIdentical || fresh.Verdict == VerdictNarrower
	}
	return false
}

func isSubset(sub []string, super []string) (ok bool) {
	for _, s := range sub {
		if !slices.Contains(super, s) {
			return false
		}
	}
	return true
}

// ApplyStructure runs the plan's DDL on the target: database statements first,
// then each table's in plan order. It stops at the first failure and returns
// the statements that ran. Every statement is IF NOT EXISTS, so a run
// interrupted part-way can be repeated.
func ApplyStructure(ctx context.Context, exec ExecI, plan *Plan) (applied []string, err error) {
	applied = make([]string, 0, len(plan.DatabaseDDL)+len(plan.Tables))
	for _, sql := range plan.DatabaseDDL {
		err = exec.Exec(ctx, sql)
		if err != nil {
			err = eb.Build().Str("sql", sql).Errorf("unable to create database: %w", err)
			return
		}
		applied = append(applied, sql)
	}
	for _, t := range plan.Tables {
		for _, sql := range t.DDL {
			err = exec.Exec(ctx, sql)
			if err != nil {
				err = eb.Build().Str("table", t.Target.String()).Str("sql", sql).Errorf("unable to apply table DDL: %w", err)
				return
			}
			applied = append(applied, sql)
		}
	}
	return
}
