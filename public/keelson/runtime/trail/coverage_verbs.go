package trail

import (
	"context"
	"strconv"
	"time"
)

// The coverage verbs (ADR-0343): a build row per run, the function table
// once per meta hash, a sample row per tick with news, and an apply row per
// agent command.

// CoverageBuild buffers the run's build row; its natural key is the run and
// the hash, one row per run.
func (inst *Recorder) CoverageBuild(at time.Time, c Context, row CoverageBuild) (err error) {
	key := "covbuild|" + inst.runId() + "|" + row.MetaHash
	return inst.write(context.Background(), key, at, c, func(b *TrailEntityBuilder, id uint64) {
		row.Id, row.Kind = id, "coverageBuild"
		b.AddCoverageBuild(row)
	})
}

// CoverageFuncs buffers function-table rows; each natural key is the hash
// and the function's unit base, which is unique within a build.
func (inst *Recorder) CoverageFuncs(at time.Time, c Context, rows []CoverageFunc) (err error) {
	for i := range rows {
		row := rows[i]
		key := "covfunc|" + row.MetaHash + "|" + strconv.FormatUint(uint64(row.UnitBase), 10)
		err = inst.write(context.Background(), key, at, c, func(b *TrailEntityBuilder, id uint64) {
			row.Id, row.Kind = id, "coverageFunc"
			b.AddCoverageFunc(row)
		})
		if err != nil {
			return
		}
	}
	return
}

// CoverageSample buffers one tick's row; its natural key is the run, the
// hash and the sample sequence.
func (inst *Recorder) CoverageSample(at time.Time, c Context, row CoverageSample) (err error) {
	key := "covsample|" + inst.runId() + "|" + row.MetaHash + "|" + strconv.FormatUint(row.Seq, 10)
	return inst.write(context.Background(), key, at, c, func(b *TrailEntityBuilder, id uint64) {
		row.Id, row.Kind = id, "coverageSample"
		b.AddCoverageSample(row)
	})
}

// CoverageAgentApply buffers one agent command's row; its natural key is
// the task and call when the command carried them, and unique otherwise.
func (inst *Recorder) CoverageAgentApply(at time.Time, c Context, row CoverageAgentApply) (err error) {
	key := "covapply|"
	if c.Delegation.Has && c.Delegation.Val.Call.Has {
		key += c.Delegation.Val.Task + "|" + c.Delegation.Val.Call.Val + "|" + strconv.FormatUint(row.Frame, 10)
	} else {
		key += inst.unique(at)
	}
	return inst.write(context.Background(), key, at, c, func(b *TrailEntityBuilder, id uint64) {
		row.Id, row.Kind = id, "coverageAgentApply"
		b.AddCoverageAgentApply(row)
	})
}

// runId is the run the recorder writes for; empty on a nil recorder.
func (inst *Recorder) runId() (run string) {
	if inst == nil {
		return ""
	}
	return inst.run
}
