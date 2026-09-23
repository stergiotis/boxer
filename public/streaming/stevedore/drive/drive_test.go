package drive

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/streaming/stevedore"
	"github.com/stergiotis/boxer/public/streaming/stevedore/stevedorefacts"
)

// lineHandler: one item per line; "bad" is permanent, "panic" panics,
// "flaky" is transient while the counter lasts, "slow" outlives a deadline.
type lineHandler struct {
	flaky atomic.Int32
	calls atomic.Int32
}

func (inst *lineHandler) Handle(ctx context.Context, req stevedore.Request, emit func(stevedore.Emit) error) error {
	inst.calls.Add(1)
	body := string(req.Body)
	switch {
	case strings.HasPrefix(body, "bad"):
		return stevedore.Permanentf("bad body")
	case strings.HasPrefix(body, "panic"):
		panic("bug")
	case strings.HasPrefix(body, "slow"):
		<-ctx.Done()
		return ctx.Err()
	case strings.HasPrefix(body, "flaky"):
		if inst.flaky.Add(-1) >= 0 {
			return eh.Errorf("not yet")
		}
	}
	for i, l := range strings.Split(body, "\n") {
		if err := emit(stevedore.Emit{Payload: []byte(l), Line: uint64(i + 1)}); err != nil {
			return err
		}
	}
	return nil
}

// memSink records landed payloads in call order and the flush count; a
// payload starting with "reject" is permanent, flushFail fails n flushes.
type memSink struct {
	mu        sync.Mutex
	landed    []string
	flushes   int
	flushFail int
}

func (inst *memSink) Land(_ context.Context, item stevedore.Item) error {
	if strings.HasPrefix(string(item.Payload), "reject") {
		return stevedore.Permanentf("rejected")
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.landed = append(inst.landed, fmt.Sprintf("%s/%d:%s", item.Origin, item.Ordinal, item.Payload))
	return nil
}

func (inst *memSink) Flush(context.Context) error {
	inst.flushes++
	if inst.flushFail > 0 {
		inst.flushFail--
		return stevedore.Permanentf("store down")
	}
	return nil
}

type memDead struct{ rows []stevedorefacts.DeadLetter }

func (inst *memDead) Add(_ context.Context, row stevedorefacts.DeadLetter) error {
	inst.rows = append(inst.rows, row)
	return nil
}
func (inst *memDead) Flush(context.Context) error { return nil }

func fast() stevedore.RetryPolicy {
	return stevedore.RetryPolicy{Attempts: 2, Base: time.Millisecond, Max: time.Millisecond}
}

func reqs(bodies ...string) (out []stevedore.Request) {
	for i, b := range bodies {
		out = append(out, stevedore.Request{Origin: fmt.Sprintf("r%d", i), Body: []byte(b)})
	}
	return
}

func TestRunLandsInOrderFlushesAndCheckpoints(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			s := &memSink{}
			d := &memDead{}
			var checkpoints []uint64
			cfg := Config{Workers: workers, FlushEvery: 2, Retry: fast(),
				Checkpoint: func(_ context.Context, n uint64) error { checkpoints = append(checkpoints, n); return nil }}
			res, err := Run(context.Background(), cfg, List{Requests: reqs("a\nb", "c", "d", "e", "f")}, &lineHandler{}, s, d)
			require.NoError(t, err)
			require.Equal(t, []string{"r0/0:a", "r0/1:b", "r1/0:c", "r2/0:d", "r3/0:e", "r4/0:f"}, s.landed, "source order, whatever the worker count")
			require.Equal(t, 3, s.flushes)
			require.Equal(t, []uint64{2, 4, 5}, checkpoints)
			require.Equal(t, Result{Requests: 5, Items: 6, Done: 5}, res)
			require.Empty(t, d.rows)
		})
	}
}

func TestFailuresBecomeDeadLettersAndTheRunGoesOn(t *testing.T) {
	h := &lineHandler{}
	h.flaky.Store(1)
	s := &memSink{}
	d := &memDead{}
	cfg := Config{Retry: fast(), Deadline: 20 * time.Millisecond}
	res, err := Run(context.Background(), cfg, List{Requests: reqs("bad", "panic", "flaky", "slow", "reject me", "ok")}, h, s, d)
	require.NoError(t, err)
	require.Equal(t, []string{"r2/0:flaky", "r5/0:ok"}, s.landed)
	require.Len(t, d.rows, 4)
	classes := map[string]string{}
	for _, r := range d.rows {
		classes[r.Origin] = r.Class
		require.Equal(t, "list", r.Topic)
		require.NotZero(t, r.Id)
	}
	require.Equal(t, map[string]string{"r0": "permanent", "r1": "permanent", "r3": "transient", "r4": "permanent"}, classes)
	require.Equal(t, uint64(4), res.DeadLetters)
	require.Equal(t, uint64(6), res.Done, "a dead letter is progress")
}

func TestAFailedFlushStopsBeforeTheCheckpoint(t *testing.T) {
	s := &memSink{flushFail: 1}
	var checkpoints []uint64
	cfg := Config{FlushEvery: 2, Retry: fast(),
		Checkpoint: func(_ context.Context, n uint64) error { checkpoints = append(checkpoints, n); return nil }}
	res, err := Run(context.Background(), cfg, List{Requests: reqs("a", "b", "c")}, &lineHandler{}, s, &memDead{})
	require.Error(t, err)
	require.Empty(t, checkpoints)
	require.Equal(t, uint64(0), res.Done)
	require.Equal(t, uint64(2), res.Requests)
}

func TestSkipResumesWhereACheckpointSaid(t *testing.T) {
	s := &memSink{}
	res, err := Run(context.Background(), Config{Skip: 2, Retry: fast()}, List{Requests: reqs("a", "b", "c", "d")}, &lineHandler{}, s, &memDead{})
	require.NoError(t, err)
	require.Equal(t, []string{"r2/0:c", "r3/0:d"}, s.landed)
	require.Equal(t, uint64(4), res.Done)
	require.Equal(t, uint64(2), res.Requests)
}

func TestTreeSourceWalksRegularFilesInOrder(t *testing.T) {
	fsys := fstest.MapFS{
		"b.txt":   {Data: []byte("two")},
		"a/x.txt": {Data: []byte("one")},
		"big.bin": {Data: []byte("0123456789")},
	}
	s := &memSink{}
	d := &memDead{}
	res, err := Run(context.Background(), Config{Retry: fast()}, Tree{FS: fsys, MaxBody: 5}, &lineHandler{}, s, d)
	require.NoError(t, err)
	require.Equal(t, []string{"a/x.txt/0:one", "b.txt/0:two"}, s.landed)
	require.Len(t, d.rows, 1)
	require.Equal(t, "big.bin", d.rows[0].Origin)
	require.Equal(t, "tree:.", d.rows[0].Topic)
	require.Equal(t, uint64(3), res.Requests)
}

func TestLinesSourceNumbersLines(t *testing.T) {
	s := &memSink{}
	res, err := Run(context.Background(), Config{Retry: fast()}, Lines{R: strings.NewReader("x\ny\n\nz"), Origin: "stdin"}, &lineHandler{}, s, &memDead{})
	require.NoError(t, err)
	require.Equal(t, []string{"stdin:1/0:x", "stdin:2/0:y", "stdin:3/0:", "stdin:4/0:z"}, s.landed)
	require.Equal(t, uint64(4), res.Done)
}

func TestReferencesRepeatAcrossRuns(t *testing.T) {
	refs := func() (out []uint64) {
		sink := &refSink{}
		_, err := Run(context.Background(), Config{Retry: fast()}, List{Requests: reqs("a", "b")}, &lineHandler{}, sink, &memDead{})
		require.NoError(t, err)
		return sink.refs
	}
	require.Equal(t, refs(), refs())
}

type refSink struct{ refs []uint64 }

func (inst *refSink) Land(_ context.Context, item stevedore.Item) error {
	inst.refs = append(inst.refs, item.Ref.Value())
	return nil
}
func (inst *refSink) Flush(context.Context) error { return nil }

func TestCancelledContextStopsTheRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Run(ctx, Config{Retry: fast()}, List{Requests: reqs("a", "b")}, &lineHandler{}, &memSink{}, &memDead{})
	require.Error(t, err)
	require.Equal(t, uint64(0), res.Done)
}
