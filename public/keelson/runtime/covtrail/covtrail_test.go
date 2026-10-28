package covtrail

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
)

// A one-package, one-function build with three units.
func testMeta() *covsnap.MetaProfile {
	return &covsnap.MetaProfile{
		Hash: [16]byte{0x0c}, Mode: covsnap.ModeAtomic, Granularity: covsnap.GranularityPerBlock,
		TotalUnits: 3, TotalStmts: 3,
		Pkgs: []covsnap.PkgMeta{{Path: "m/a", Name: "a", ModulePath: "m", NumUnits: 3, NumStmts: 3, Funcs: []covsnap.FuncMeta{
			{Name: "F", SrcFile: "m/a/a.go", NumStmts: 3, Units: []covsnap.UnitMeta{{StLine: 1, EnLine: 1, NxStmts: 1}, {StLine: 2, EnLine: 2, NxStmts: 1}, {StLine: 3, EnLine: 3, NxStmts: 1}}},
		}}},
	}
}

type metaSource struct{ meta *covsnap.MetaProfile }

func (m metaSource) Meta() *covsnap.MetaProfile { return m.meta }

// stubReader hands out counters that advance on every read, and counts
// the reads a bracket took.
type stubReader struct {
	reads    int
	counters []uint32
}

func (r *stubReader) read(_ *bytes.Buffer) (*covsnap.CounterSnapshot, error) {
	r.reads++
	c := append([]uint32(nil), r.counters...)
	return &covsnap.CounterSnapshot{MetaHash: [16]byte{0x0c}, Funcs: []covsnap.FuncCounters{{PkgIdx: 0, FuncIdx: 0, Counters: c}}}, nil
}

func newService(t *testing.T, r *stubReader) *Service {
	t.Helper()
	// A recorder without an executor records nothing; what is under test
	// is the bracket's reads and the service's holding of updates.
	rec := trail.NewRecorder(nil, "run-1", zerolog.Nop())
	t.Cleanup(rec.Close)
	svc := New(Options{Trail: rec, Log: zerolog.Nop(), Read: r.read, Clock: func() time.Time { return time.Unix(0, 0) }})
	return svc
}

func TestBracketReusesTheReadAfterACommandWithinAFrame(t *testing.T) {
	r := &stubReader{counters: []uint32{1, 0, 0}}
	svc := newService(t, r)
	require.NoError(t, svc.Attach(context.Background(), metaSource{testMeta()}))
	b := svc.Bracket(app.AppIdT("a"), 7)
	applied := opengine.Applied{Frame: 3, CallId: "t-1", Operation: "op", Phase: opwire.PhaseApplied,
		OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1, Call: "t-1"}}

	b.Begin(3)
	r.counters[1] = 1
	b.End(applied)
	require.Equal(t, 2, r.reads, "the first command of a frame reads before and after")

	b.Begin(3)
	r.counters[2] = 1
	b.End(applied)
	require.Equal(t, 3, r.reads, "the next command of the same frame reuses the read after the previous one")

	b.Begin(4)
	b.End(applied)
	require.Equal(t, 5, r.reads, "a new frame reads before again")
}

func TestBracketEndWithoutBeginReadsNothing(t *testing.T) {
	r := &stubReader{counters: []uint32{1, 0, 0}}
	svc := newService(t, r)
	require.NoError(t, svc.Attach(context.Background(), metaSource{testMeta()}))
	b := svc.Bracket(app.AppIdT("a"), 7)
	b.End(opengine.Applied{Frame: 1, Phase: opwire.PhaseApplied})
	require.Zero(t, r.reads)
}

func TestUpdatesBeforeAttachAreHeld(t *testing.T) {
	r := &stubReader{counters: []uint32{1, 0, 0}}
	svc := newService(t, r)
	svc.OnUpdate(&covsnap.Update{Seq: 1})
	svc.mu.Lock()
	held := len(svc.pending)
	svc.mu.Unlock()
	require.Equal(t, 1, held)
	require.NoError(t, svc.Attach(context.Background(), metaSource{testMeta()}))
	svc.mu.Lock()
	held = len(svc.pending)
	svc.mu.Unlock()
	require.Zero(t, held, "attach drains what was held")
	require.Equal(t, "0c000000000000000000000000000000", svc.Hash())
	require.Error(t, svc.Attach(context.Background(), metaSource{testMeta()}), "attach is once")
}

func TestAttachRefusesAMissingProfile(t *testing.T) {
	svc := newService(t, &stubReader{})
	require.Error(t, svc.Attach(context.Background(), metaSource{nil}))
}
