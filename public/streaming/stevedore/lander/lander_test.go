package lander

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/streaming/persisted/kafka"
	"github.com/stergiotis/boxer/public/streaming/stevedore"
	"github.com/stergiotis/boxer/public/streaming/stevedore/stevedorefacts"
)

// memConsumer serves scripted batches, then blocks until the context ends,
// and records the acknowledgements in order.
type memConsumer struct {
	batches [][]*kgo.Record
	next    int
	acked   []int
}

func (inst *memConsumer) Connect(context.Context) error { return nil }
func (inst *memConsumer) Close(context.Context) error   { return nil }
func (inst *memConsumer) Read(ctx context.Context) (b kafka.Batch, err error) {
	if inst.next >= len(inst.batches) {
		// The script is done: end the run the way a cancelled context does.
		return b, context.Canceled
	}
	i := inst.next
	inst.next++
	b.Records = kgo.Fetches{{Topics: []kgo.FetchTopic{{Topic: "items", Partitions: []kgo.FetchPartition{{Partition: 0, Records: inst.batches[i]}}}}}}
	b.Ack = func(context.Context, error) error {
		inst.acked = append(inst.acked, i)
		return nil
	}
	return
}

// memSink keys rows by reference and ordinal, the contract a sink keeps, so
// a redelivery is a rewrite. "bad" payloads are permanent, "flaky" ones
// transient for one attempt, and flushFail fails the next n flushes.
type memSink struct {
	rows      map[string]string
	landed    int
	flakyLeft int
	flushFail int
	flushes   int
}

func (inst *memSink) Land(_ context.Context, item stevedore.Item) error {
	p := string(item.Payload)
	if strings.HasPrefix(p, "bad") {
		return stevedore.Permanentf("payload refused")
	}
	if strings.HasPrefix(p, "flaky") && inst.flakyLeft > 0 {
		inst.flakyLeft--
		return eh.Errorf("not yet")
	}
	if inst.rows == nil {
		inst.rows = map[string]string{}
	}
	inst.rows[fmt.Sprintf("%d/%d", item.Ref.Value(), item.Ordinal)] = p
	inst.landed++
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

type memDead struct {
	rows    []stevedorefacts.DeadLetter
	flushes int
}

func (inst *memDead) Add(_ context.Context, row stevedorefacts.DeadLetter) error {
	inst.rows = append(inst.rows, row)
	return nil
}
func (inst *memDead) Flush(context.Context) error { inst.flushes++; return nil }

func rec(t *testing.T, offset int64, item stevedore.Item) *kgo.Record {
	t.Helper()
	b, err := stevedore.EncodeItem(item)
	require.NoError(t, err)
	return &kgo.Record{Topic: "items", Partition: 0, Offset: offset, Value: b}
}

func itemOf(origin string, ordinal uint64, payload string) stevedore.Item {
	return stevedore.Item{Ref: stevedore.ReferenceOf(stevedore.Request{Origin: origin}), Origin: origin, Ordinal: ordinal, Payload: []byte(payload)}
}

func fastRetry() stevedore.RetryPolicy {
	return stevedore.RetryPolicy{Attempts: 2, Base: time.Millisecond, Max: time.Millisecond}
}

func runUntilIdle(t *testing.T, l *Lander) error {
	t.Helper()
	return l.Run(context.Background())
}

func TestLandsFlushesThenAcks(t *testing.T) {
	c := &memConsumer{batches: [][]*kgo.Record{
		{rec(t, 0, itemOf("a", 0, "x")), rec(t, 1, itemOf("a", 1, "y"))},
		{rec(t, 2, itemOf("b", 0, "z"))},
	}}
	s := &memSink{}
	d := &memDead{}
	l := New(Config{Retry: fastRetry()}, c, s, d)
	require.NoError(t, runUntilIdle(t, l))
	require.Equal(t, []int{0, 1}, c.acked)
	require.Equal(t, 3, s.landed)
	require.Equal(t, 2, s.flushes, "one flush per batch")
	require.Equal(t, uint64(3), l.Landed())
	require.Empty(t, d.rows)
}

func TestPermanentFailuresBecomeDeadLetters(t *testing.T) {
	garbage := &kgo.Record{Topic: "items", Partition: 0, Offset: 5, Value: []byte("not an envelope")}
	c := &memConsumer{batches: [][]*kgo.Record{
		{rec(t, 0, itemOf("a", 0, "bad one")), garbage, rec(t, 6, itemOf("a", 1, "fine"))},
	}}
	s := &memSink{}
	d := &memDead{}
	l := New(Config{Retry: fastRetry()}, c, s, d)
	require.NoError(t, runUntilIdle(t, l))
	require.Equal(t, []int{0}, c.acked, "the batch is acknowledged: its failures are rows")
	require.Len(t, d.rows, 2)
	require.Equal(t, "permanent", d.rows[0].Class)
	require.Equal(t, "a", d.rows[0].Origin)
	require.Equal(t, int64(0), d.rows[0].Offset)
	require.Equal(t, "permanent", d.rows[1].Class)
	require.Equal(t, int64(5), d.rows[1].Offset)
	require.Equal(t, "not an envelope", string(d.rows[1].Message))
	require.Zero(t, d.rows[1].Ref, "an envelope that did not decode has no reference")
	require.NotEqual(t, d.rows[0].Id, d.rows[1].Id)
	require.Equal(t, 1, s.landed)
	require.Equal(t, uint64(2), l.DeadLettered())
}

func TestTransientLandIsRetriedInPlace(t *testing.T) {
	c := &memConsumer{batches: [][]*kgo.Record{{rec(t, 0, itemOf("a", 0, "flaky"))}}}
	s := &memSink{flakyLeft: 1}
	d := &memDead{}
	require.NoError(t, runUntilIdle(t, New(Config{Retry: fastRetry()}, c, s, d)))
	require.Equal(t, 1, s.landed)
	require.Empty(t, d.rows)
}

func TestTransientExhaustedStopsWithoutAck(t *testing.T) {
	c := &memConsumer{batches: [][]*kgo.Record{{rec(t, 0, itemOf("a", 0, "flaky"))}}}
	s := &memSink{flakyLeft: 10}
	err := New(Config{Retry: fastRetry()}, c, s, &memDead{}).Run(context.Background())
	require.Error(t, err)
	require.Empty(t, c.acked)
}

// A crash mid-batch — here a flush that fails — leaves the batch
// unacknowledged; the run that follows lands it again, and the rows
// collapsed on reference and ordinal equal a clean run's.
func TestCrashThenRedeliveryCollapses(t *testing.T) {
	batch := func() []*kgo.Record {
		return []*kgo.Record{rec(t, 0, itemOf("a", 0, "x")), rec(t, 1, itemOf("a", 1, "y"))}
	}
	clean := &memSink{}
	require.NoError(t, runUntilIdle(t, New(Config{Retry: fastRetry()}, &memConsumer{batches: [][]*kgo.Record{batch()}}, clean, &memDead{})))

	crashed := &memSink{flushFail: 1}
	c1 := &memConsumer{batches: [][]*kgo.Record{batch()}}
	err := New(Config{Retry: fastRetry()}, c1, crashed, &memDead{}).Run(context.Background())
	require.Error(t, err)
	require.Empty(t, c1.acked, "a failed flush commits nothing")
	require.Equal(t, 2, crashed.landed, "rows were written before the flush failed")

	c2 := &memConsumer{batches: [][]*kgo.Record{batch()}}
	require.NoError(t, runUntilIdle(t, New(Config{Retry: fastRetry()}, c2, crashed, &memDead{})))
	require.Equal(t, []int{0}, c2.acked)
	require.Equal(t, 4, crashed.landed, "written twice")
	require.Equal(t, clean.rows, crashed.rows, "read once")
}

func TestReassemblyLandsWholeBodiesAndExpiresIncompleteOnes(t *testing.T) {
	ref := stevedore.ReferenceOf(stevedore.Request{Origin: "f"})
	part := func(offset int64, idx uint32, last bool, total uint32, p string) *kgo.Record {
		return rec(t, offset, stevedore.Item{Ref: ref, Origin: "f", Ordinal: 0, Split: true, Part: idx, Parts: total, Last: last, Payload: []byte(p)})
	}
	orphan := stevedore.ReferenceOf(stevedore.Request{Origin: "orphan"})
	c := &memConsumer{batches: [][]*kgo.Record{
		{part(0, 1, false, 0, "world"), part(1, 0, false, 0, "hello "),
			rec(t, 2, stevedore.Item{Ref: orphan, Origin: "orphan", Split: true, Part: 0, Payload: []byte("never finished")})},
		{part(3, 2, true, 3, "!"), part(4, 1, false, 0, "world")},
		{rec(t, 5, itemOf("plain", 0, "unsplit"))},
	}}
	s := &memSink{}
	d := &memDead{}
	now := time.Unix(1000, 0)
	l := New(Config{Retry: fastRetry(), Reassemble: true, ReassembleMaxAge: time.Minute}, c, s, d)
	// Both clocks advance two minutes after the first batch, so the orphan
	// expires at the second batch's sweep.
	clock := func() time.Time {
		if c.next > 1 {
			return now.Add(2 * time.Minute)
		}
		return now
	}
	l.now = clock
	l.re.SetClockForTest(clock)
	require.NoError(t, runUntilIdle(t, l))
	require.Equal(t, "hello world!", s.rows[fmt.Sprintf("%d/0", ref.Value())])
	require.Equal(t, "unsplit", s.rows[fmt.Sprintf("%d/0", stevedore.ReferenceOf(stevedore.Request{Origin: "plain"}).Value())])
	require.Equal(t, 2, s.landed, "a duplicate part changes nothing")
	require.Len(t, d.rows, 1)
	require.Equal(t, "incomplete", d.rows[0].Class)
	require.Equal(t, orphan.Value(), d.rows[0].Ref)
	require.Equal(t, []int{0, 1, 2}, c.acked)
}

func TestDeadLetterIdentityIsStable(t *testing.T) {
	a, ka := deadLetterIdentity("t", 3, 99)
	b, kb := deadLetterIdentity("t", 3, 99)
	require.Equal(t, a, b)
	require.Equal(t, ka, kb)
	c, _ := deadLetterIdentity("t", 3, 100)
	require.NotEqual(t, a, c)
	ids := []uint64{a, c}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	require.Len(t, ids, 2)
}
