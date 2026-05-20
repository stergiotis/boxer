package processor

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// =============================================================================
// CONCRETE TYPES FOR TESTING (Generics Implementation)
// =============================================================================

type TestID int

type TestRow struct {
	ID  TestID
	Val string
}

func (inst TestRow) GetEntityID() TestID {
	return inst.ID
}

// =============================================================================
// TEST MOCKS & HELPERS
// =============================================================================

// MockReader allows simulating complex batch sequences and errors.
// It supports "unsafe" mode where it reuses the same underlying array
// to test if the Processor correctly copies data.
type MockReader struct {
	Batches     [][]TestRow
	ReturnErr   error
	UnsafeReuse bool // If true, reuses memory to test copy semantics
}

func (inst *MockReader) StreamBatches(ctx context.Context) iter.Seq2[[]TestRow, error] {
	return func(yield func([]TestRow, error) bool) {
		var reuseBuffer []TestRow

		for _, b := range inst.Batches {
			// Check context before sending
			select {
			case <-ctx.Done():
				return
			default:
			}

			if inst.ReturnErr != nil {
				yield(nil, inst.ReturnErr)
				return
			}

			// Simulation of a DB driver reusing memory
			if inst.UnsafeReuse {
				if cap(reuseBuffer) < len(b) {
					reuseBuffer = make([]TestRow, len(b))
				}
				reuseBuffer = reuseBuffer[:len(b)]
				copy(reuseBuffer, b)
				if !yield(reuseBuffer, nil) {
					return
				}
				// Corrupt the buffer after yield returns
				for k := range reuseBuffer {
					reuseBuffer[k] = TestRow{ID: -1, Val: "CORRUPTED"}
				}
			} else {
				// Standard safe behavior
				if !yield(b, nil) {
					return
				}
			}
		}
	}
}

// ThreadSafeConsumer records results safely across goroutines.
type ThreadSafeConsumer struct {
	mu         sync.Mutex
	Processed  map[TestID][]string
	CallCounts map[TestID]int

	// Behaviors
	SleepDur      time.Duration
	PanicOnID     TestID
	ErrorOnID     TestID
	StopEarlyOnID TestID // Returns nil after 1st row
}

func NewTestConsumer() *ThreadSafeConsumer {
	return &ThreadSafeConsumer{
		Processed:  make(map[TestID][]string),
		CallCounts: make(map[TestID]int),
	}
}

func (inst *ThreadSafeConsumer) Process(ctx context.Context, id TestID, rows iter.Seq[TestRow]) error {
	inst.mu.Lock()
	inst.CallCounts[id]++
	inst.mu.Unlock()

	if id == inst.PanicOnID {
		panic("simulated panic")
	}

	for r := range rows {
		// Simulate work
		if inst.SleepDur > 0 {
			time.Sleep(inst.SleepDur)
		}

		inst.mu.Lock()
		inst.Processed[id] = append(inst.Processed[id], r.Val)
		inst.mu.Unlock()

		if id == inst.StopEarlyOnID {
			return nil
		}

		if id == inst.ErrorOnID {
			return errors.New("simulated error")
		}
	}
	return nil
}

// =============================================================================
// TEST CASES
// =============================================================================

func TestProcessor_CrossBatchLifecycle(t *testing.T) {
	// Scenario: Entity 1 spans 3 batches. Entity 2 starts in Batch 2.
	// We verify that the consumer sees a continuous stream for Entity 1.

	b1 := []TestRow{{ID: 1, Val: "1a"}, {ID: 1, Val: "1b"}}
	b2 := []TestRow{{ID: 1, Val: "1c"}, {ID: 2, Val: "2a"}}
	b3 := []TestRow{{ID: 2, Val: "2b"}, {ID: 1, Val: "1d-reappear"}}
	// Note: "1d-reappear" appearing after ID 2 should be treated as a NEW lifecycle
	// because the stream is assumed ordered.

	reader := &MockReader{Batches: [][]TestRow{b1, b2, b3}}
	consumer := NewTestConsumer()

	proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig())
	err := proc.Run(context.Background(), reader)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check Entity 1 (First lifecycle)
	expected1 := []string{"1a", "1b", "1c"}
	if !slices.Equal(consumer.Processed[1][:3], expected1) {
		t.Errorf("Entity 1 stream mismatch. Got %v", consumer.Processed[1])
	}

	// Check Entity 1 (Second lifecycle / Reappearance)
	if consumer.CallCounts[1] != 2 {
		t.Errorf("Expected Entity 1 to be processed twice (reappearance), got %d calls", consumer.CallCounts[1])
	}
}

func TestProcessor_MemorySafety_CopyCheck(t *testing.T) {
	// Scenario: The Reader reuses the exact same memory array for every batch.
	// If the Processor fails to copy, the consumer (running async) will read corrupted data.

	b1 := []TestRow{{ID: 1, Val: "A"}}
	b2 := []TestRow{{ID: 1, Val: "B"}} // Reader will overwrite b1's memory with this

	reader := &MockReader{
		Batches:     [][]TestRow{b1, b2},
		UnsafeReuse: true,
	}

	consumer := NewTestConsumer()
	consumer.SleepDur = 10 * time.Millisecond // Slow consumer

	proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig())
	err := proc.Run(context.Background(), reader)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	vals := consumer.Processed[1]
	if len(vals) != 2 {
		t.Fatalf("expected 2 values, got %d", len(vals))
	}
	if vals[0] != "A" || vals[1] != "B" {
		t.Errorf("Memory corruption detected! Got %v, expected [A, B]", vals)
	}
}

func TestProcessor_EdgeCases_EmptyAndNil(t *testing.T) {
	batches := [][]TestRow{
		nil,
		{},
		{{ID: 1, Val: "A"}},
		{{ID: 2, Val: "B"}},
	}

	reader := &MockReader{Batches: batches}
	consumer := NewTestConsumer()

	proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig())
	err := proc.Run(context.Background(), reader)
	if err != nil {
		t.Errorf("failed on empty batches: %v", err)
	}

	if len(consumer.Processed) != 2 {
		t.Errorf("expected 2 entities processed, got %d", len(consumer.Processed))
	}
}

func TestProcessor_PanicRecovery(t *testing.T) {
	batches := [][]TestRow{
		{{ID: 1, Val: "OK"}},
		{{ID: 666, Val: "DOOM"}}, // Will panic
		{{ID: 2, Val: "Unreachable"}},
	}

	consumer := NewTestConsumer()
	consumer.PanicOnID = 666

	proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig())
	err := proc.Run(context.Background(), &MockReader{Batches: batches})

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("error message should mention panic, got: %v", err)
	}

	if _, ok := consumer.Processed[1]; !ok {
		t.Error("Entity 1 should have been processed")
	}
}

func TestProcessor_EarlyConsumerExit(t *testing.T) {
	manyRows := make([]TestRow, 50)
	for i := range manyRows {
		manyRows[i] = TestRow{ID: 10, Val: "x"}
	}

	consumer := NewTestConsumer()
	consumer.StopEarlyOnID = 10

	proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig())
	err := proc.Run(context.Background(), &MockReader{Batches: [][]TestRow{manyRows}})

	if err != nil {
		t.Errorf("unexpected error on early exit: %v", err)
	}

	if len(consumer.Processed[10]) != 1 {
		t.Errorf("expected 1 row processed, got %d", len(consumer.Processed[10]))
	}
}

func TestProcessor_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	streamFunc := func(ctx context.Context) iter.Seq2[[]TestRow, error] {
		return func(yield func([]TestRow, error) bool) {
			for {
				select {
				case <-ctx.Done():
					return
				default:
					if !yield([]TestRow{{ID: 1, Val: "tick"}}, nil) {
						return
					}
					time.Sleep(1 * time.Millisecond)
				}
			}
		}
	}

	wrapper := &FunctionalReader{Stream: streamFunc}

	proc := NewProcessor[TestID, TestRow](NewTestConsumer(), DefaultConfig())

	done := make(chan error)
	go func() {
		done <- proc.Run(ctx, wrapper)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context canceled error, got: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for processor to stop")
	}
}

// FunctionalReader helper for the context test
type FunctionalReader struct {
	Stream func(ctx context.Context) iter.Seq2[[]TestRow, error]
}

func (inst *FunctionalReader) StreamBatches(ctx context.Context) iter.Seq2[[]TestRow, error] {
	return inst.Stream(ctx)
}

// TestProcessor_EarlyExit_NilReturnAcrossBatches stresses the channel-full
// race that the original code mis-handled: the consumer returns nil after
// the first row, but the main loop is still trying to send more chunks. The
// original code surfaced this as "consumer stopped early: %!w(<nil>)" by
// wrapping a nil error.
func TestProcessor_EarlyExit_NilReturnAcrossBatches(t *testing.T) {
	batches := make([][]TestRow, 50)
	for i := range batches {
		batches[i] = []TestRow{{ID: 10, Val: "x"}}
	}

	consumer := NewTestConsumer()
	consumer.StopEarlyOnID = 10

	proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig())
	err := proc.Run(context.Background(), &MockReader{Batches: batches})

	if err != nil {
		t.Fatalf("consumer's nil return must not surface as error: %v", err)
	}
	if got := len(consumer.Processed[10]); got < 1 {
		t.Errorf("expected at least one row processed, got %d", got)
	}
}

// TestProcessor_EarlyExit_ErrorReturnPropagates pairs with the test above:
// when the consumer returns a non-nil error mid-stream, the error must
// propagate, not be discarded as a "legitimate early-stop".
func TestProcessor_EarlyExit_ErrorReturnPropagates(t *testing.T) {
	batches := make([][]TestRow, 50)
	for i := range batches {
		batches[i] = []TestRow{{ID: 10, Val: "x"}}
	}

	consumer := NewTestConsumer()
	consumer.ErrorOnID = 10

	proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig())
	err := proc.Run(context.Background(), &MockReader{Batches: batches})

	if err == nil {
		t.Fatal("expected consumer error to propagate")
	}
	if !strings.Contains(err.Error(), "simulated error") {
		t.Errorf("expected consumer's error message, got: %v", err)
	}
}

// TestProcessor_EarlyExit_NextEntityContinues verifies that after a consumer
// early-stops on entity 1, the processor continues with entity 2 normally
// instead of aborting the pipeline.
func TestProcessor_EarlyExit_NextEntityContinues(t *testing.T) {
	batches := make([][]TestRow, 0, 31)
	for i := 0; i < 30; i++ {
		batches = append(batches, []TestRow{{ID: 1, Val: "x"}})
	}
	batches = append(batches, []TestRow{{ID: 2, Val: "2a"}, {ID: 2, Val: "2b"}})

	consumer := NewTestConsumer()
	consumer.StopEarlyOnID = 1

	proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig())
	err := proc.Run(context.Background(), &MockReader{Batches: batches})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(consumer.Processed[2]); got != 2 {
		t.Errorf("entity 2 must process both rows: got %d, want 2", got)
	}
}

// TestProcessor_ContextCancellation_NoGoroutineLeak verifies Run joins the
// consumer goroutine on ctx cancellation. The original code abandoned the
// goroutine, leaking it.
func TestProcessor_ContextCancellation_NoGoroutineLeak(t *testing.T) {
	baseline := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())

	stream := func(ctx context.Context) iter.Seq2[[]TestRow, error] {
		return func(yield func([]TestRow, error) bool) {
			for {
				select {
				case <-ctx.Done():
					return
				default:
					if !yield([]TestRow{{ID: 1, Val: "tick"}}, nil) {
						return
					}
					time.Sleep(time.Millisecond)
				}
			}
		}
	}

	consumer := NewTestConsumer()
	consumer.SleepDur = 5 * time.Millisecond
	proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig())

	done := make(chan error, 1)
	go func() {
		done <- proc.Run(ctx, &FunctionalReader{Stream: stream})
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}

	// Allow scheduling to settle; check stabilizes.
	for i := 0; i < 20; i++ {
		runtime.Gosched()
		if runtime.NumGoroutine() <= baseline {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("goroutine leak: baseline=%d after=%d", baseline, runtime.NumGoroutine())
}

// TestProcessor_PanicIsLogged verifies a recovered consumer panic is emitted
// to the global zerolog logger, not just returned as an error. Without the
// log call, a caller that drops the returned error would silently lose the
// panic.
func TestProcessor_PanicIsLogged(t *testing.T) {
	var buf bytes.Buffer
	original := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = original })

	batches := [][]TestRow{
		{{ID: 1, Val: "OK"}},
		{{ID: 666, Val: "DOOM"}},
	}

	consumer := NewTestConsumer()
	consumer.PanicOnID = 666

	proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig())
	err := proc.Run(context.Background(), &MockReader{Batches: batches})
	if err == nil {
		t.Fatal("expected error from panic")
	}

	logged := buf.String()
	if !strings.Contains(logged, "panic recovered in consumer") {
		t.Errorf("expected panic log message, got: %q", logged)
	}
	if !strings.Contains(logged, "entity_id") {
		t.Errorf("expected entity_id field in log entry, got: %q", logged)
	}
	if !strings.Contains(logged, "666") {
		t.Errorf("expected the panicking entity ID (666) in log entry, got: %q", logged)
	}
}

// countingPool wraps another ChunkPoolI and counts Get/Put calls. Used to
// assert that the processor returns every chunk it obtains.
type countingPool[T any] struct {
	inner ChunkPoolI[T]
	gets  atomic.Int64
	puts  atomic.Int64
}

func (p *countingPool[T]) Get() []T {
	p.gets.Add(1)
	return p.inner.Get()
}

func (p *countingPool[T]) Put(s []T) {
	p.puts.Add(1)
	p.inner.Put(s)
}

// TestProcessor_PoolReclaimsChunks_OnEarlyExit verifies that every chunk
// obtained from the pool is returned to it when the consumer yield-falses
// mid-stream — the path where chunks could previously be left buffered in
// the row channel and lost to GC.
func TestProcessor_PoolReclaimsChunks_OnEarlyExit(t *testing.T) {
	batches := make([][]TestRow, 50)
	for i := range batches {
		batches[i] = []TestRow{{ID: 10, Val: "x"}}
	}

	pool := &countingPool[TestRow]{inner: NewSlicePool[TestRow](256)}

	consumer := NewTestConsumer()
	consumer.StopEarlyOnID = 10

	proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig(),
		WithPool[TestID, TestRow](pool))
	err := proc.Run(context.Background(), &MockReader{Batches: batches})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gets := pool.gets.Load()
	puts := pool.puts.Load()
	if gets == 0 {
		t.Fatal("expected at least one Get from the injected pool")
	}
	if gets != puts {
		t.Errorf("pool leak: %d gets vs %d puts (delta %d)", gets, puts, gets-puts)
	}
}

// TestPrefetcher_PassThrough verifies batches flow through the prefetcher
// unmodified and in order. The package had no prefetcher tests before this.
func TestPrefetcher_PassThrough(t *testing.T) {
	batches := [][]TestRow{
		{{ID: 1, Val: "a"}, {ID: 1, Val: "b"}},
		{{ID: 2, Val: "c"}},
		{{ID: 3, Val: "d"}, {ID: 3, Val: "e"}, {ID: 3, Val: "f"}},
	}

	source := &MockReader{Batches: batches}
	prefetched := Prefetcher[TestID, TestRow](context.Background(), source, 2)

	var got [][]TestRow
	for batch, err := range prefetched.StreamBatches(context.Background()) {
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		cp := make([]TestRow, len(batch))
		copy(cp, batch)
		got = append(got, cp)
	}

	if len(got) != len(batches) {
		t.Fatalf("expected %d batches, got %d", len(batches), len(got))
	}
	for i, b := range got {
		if !slices.Equal(b, batches[i]) {
			t.Errorf("batch %d mismatch: got %v, want %v", i, b, batches[i])
		}
	}
}

// TestPrefetcher_NoGoroutineLeakOnDownstreamStop verifies the producer
// goroutine exits when the consumer stops iterating, even if the outer ctx
// is never cancelled. The original code blocked the producer indefinitely
// on the channel send.
func TestPrefetcher_NoGoroutineLeakOnDownstreamStop(t *testing.T) {
	baseline := runtime.NumGoroutine()

	source := &FunctionalReader{
		Stream: func(ctx context.Context) iter.Seq2[[]TestRow, error] {
			return func(yield func([]TestRow, error) bool) {
				for {
					select {
					case <-ctx.Done():
						return
					default:
						if !yield([]TestRow{{ID: 1, Val: "tick"}}, nil) {
							return
						}
					}
				}
			}
		},
	}

	prefetched := Prefetcher[TestID, TestRow](context.Background(), source, 4)

	count := 0
	for batch, err := range prefetched.StreamBatches(context.Background()) {
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		_ = batch
		count++
		if count >= 3 {
			break
		}
	}

	for i := 0; i < 20; i++ {
		runtime.Gosched()
		if runtime.NumGoroutine() <= baseline {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("goroutine leak: baseline=%d after=%d", baseline, runtime.NumGoroutine())
}
