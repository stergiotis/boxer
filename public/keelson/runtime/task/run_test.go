package task

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
)

// runFixture is a window's task API on an in-proc bus, with the lifecycle
// verbs an observer saw.
type runFixture struct {
	api   TaskApiI
	stop  chan struct{}
	scope *lifetime.Scope
	verbs chan string
}

func newRunFixture(t *testing.T) (f *runFixture) {
	busInst := inprocbus.NewInst(zerolog.Nop())
	f = &runFixture{stop: make(chan struct{}), scope: lifetime.New(nil), verbs: make(chan string, 8)}
	mc := app.NewStaticMountContext("test.app", zerolog.Nop(), nil, busInst.NewClient("test.app", ProducerCaps()), f.stop)
	mc.SetLifetime(f.scope)
	f.api = ForApp(mc)
	unsub, err := busInst.NewClient("test.observer", ObserverCaps()).Subscribe(PatternAll, func(m *app.Msg) {
		if _, verb, ok := ParseSubject(m.Subject); ok {
			f.verbs <- verb
		}
	})
	require.NoError(t, err)
	t.Cleanup(unsub)
	return
}

// until collects verbs until the terminal one arrives.
func (inst *runFixture) until(t *testing.T, terminal string) (got []string) {
	for !slices.Contains(got, terminal) {
		select {
		case v := <-inst.verbs:
			got = append(got, v)
		case <-time.After(2 * time.Second):
			t.Fatalf("no %s; verbs so far: %v", terminal, got)
		}
	}
	return
}

func TestRunWithoutApiRunsUntracked(t *testing.T) {
	parent := context.WithValue(context.Background(), struct{}{}, 1)
	boom := errors.New("boom")
	err := Run(nil, parent, SpawnOpts{Kind: "k"}, func(ctx context.Context) error {
		assert.Equal(t, parent, ctx)
		return boom
	})
	assert.ErrorIs(t, err, boom)
	assert.ErrorIs(t, RunToCompletion(nil, SpawnOpts{Kind: "k"}, func() error { return boom }), boom)
}

func TestRunRefusedSpawnRunsUntracked(t *testing.T) {
	busInst := inprocbus.NewInst(zerolog.Nop())
	// No ProducerCaps: the bus refuses the task's subjects.
	mc := app.NewStaticMountContext("test.app", zerolog.Nop(), nil, busInst.NewClient("test.app", nil), nil)
	parent := context.Background()
	ran := false
	require.NoError(t, Run(ForApp(mc), parent, SpawnOpts{Kind: "k"}, func(ctx context.Context) error {
		assert.Equal(t, parent, ctx)
		ran = true
		return nil
	}))
	assert.True(t, ran)
}

func TestRunPublishesDoneOrError(t *testing.T) {
	f := newRunFixture(t)
	require.NoError(t, Run(f.api, context.Background(), SpawnOpts{Kind: "k"}, func(context.Context) error { return nil }))
	assert.Equal(t, []string{VerbCreated, VerbDone}, f.until(t, VerbDone))

	boom := errors.New("boom")
	assert.ErrorIs(t, Run(f.api, context.Background(), SpawnOpts{Kind: "k"}, func(context.Context) error { return boom }), boom)
	assert.Equal(t, []string{VerbCreated, VerbError}, f.until(t, VerbError))
}

// A run whose context ended is not a failed one, whatever error the
// cancelled work surfaced.
func TestRunEndsACancelledRunAsDone(t *testing.T) {
	f := newRunFixture(t)
	parent, cancel := context.WithCancel(context.Background())
	err := Run(f.api, parent, SpawnOpts{Kind: "k"}, func(ctx context.Context) error {
		cancel()
		<-ctx.Done()
		return io.ErrUnexpectedEOF
	})
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF, "the caller still gets the work's error")
	assert.NotContains(t, f.until(t, VerbDone), VerbError)
}

// The window's close does not stop work run to completion, a failure after
// it is still a failure, and the drain waits for the work.
func TestRunToCompletionOutlivesTheClose(t *testing.T) {
	f := newRunFixture(t)
	release := make(chan struct{})
	returned := make(chan error, 1)
	go func() {
		returned <- RunToCompletion(f.api, SpawnOpts{Kind: "k", Cancellable: true}, func() error {
			<-release
			return io.ErrUnexpectedEOF
		})
	}()
	require.Equal(t, VerbCreated, <-f.verbs)
	close(f.stop)
	f.scope.Retire()
	require.Equal(t, VerbCancel, <-f.verbs, "the close is announced")
	select {
	case <-f.scope.Done():
		t.Fatal("the scope drained while the work still ran")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	assert.ErrorIs(t, <-returned, io.ErrUnexpectedEOF)
	assert.Equal(t, []string{VerbError}, f.until(t, VerbError))
	require.Eventually(t, func() bool {
		select {
		case <-f.scope.Done():
			return true
		default:
			return false
		}
	}, 2*time.Second, time.Millisecond)
}

func TestRunPublishesAPanicAndReraisesIt(t *testing.T) {
	f := newRunFixture(t)
	assert.PanicsWithValue(t, "boom", func() {
		_ = Run(f.api, context.Background(), SpawnOpts{Kind: "k"}, func(context.Context) error { panic("boom") })
	})
	assert.Equal(t, []string{VerbCreated, VerbError}, f.until(t, VerbError))
}
