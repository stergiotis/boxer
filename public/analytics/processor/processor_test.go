package processor

import (
	"context"
	"errors"
	"iter"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// CONCRETE TYPES FOR TESTING (Generics Implementation)
// =============================================================================

type TestID int

type TestRow struct {
	ID  TestID
	Val string
}

func (r TestRow) GetEntityID() TestID {
	return r.ID
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
