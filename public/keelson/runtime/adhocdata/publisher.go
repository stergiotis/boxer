package adhocdata

import (
	"bytes"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Publisher is one app's side of one dataset (ADR-0240 §SD6): it publishes
// under a fixed alias, reuses the handle it was minted across republishes
// so re-captures neither leak datasets against the quotas nor break the
// consumers bound to the handle, and keeps the last outcome and a
// generation counter for the render thread to react to exactly once per
// publish. What it does not hold is the round's busy flag: an app that
// publishes two datasets per click has one round, not two.
//
// Safe for concurrent use; a publish is a blocking bus round trip and
// belongs off the render thread.
type Publisher struct {
	alias string
	keep  bool

	mu   sync.Mutex
	last PublishResult
	err  error
	gen  uint64
}

// NewPublisher builds a publisher for alias. keepAfterClose marks every
// dataset it publishes as the app's rather than the window's (ADR-0240
// §SD5) — for a process-global publisher that republishes from whichever
// window is open, or data that should outlive the window that captured it.
func NewPublisher(alias string, keepAfterClose bool) (inst *Publisher) {
	return &Publisher{alias: alias, keep: keepAfterClose}
}

// Alias is the alias every publish goes under.
func (inst *Publisher) Alias() (s string) { return inst.alias }

// Publish sends stream over bus, republishing onto the held handle when
// there is one. On success the handle is recorded before anything else,
// so a later failure in the same round cannot orphan it; the result and
// generation land for [Publisher.Last].
func (inst *Publisher) Publish(bus app.BusI, stream []byte) (res PublishResult, err error) {
	if bus == nil {
		return res, eh.Errorf("adhocdata: no bus to publish over")
	}
	inst.mu.Lock()
	handle := inst.last.Handle
	inst.mu.Unlock()
	res, err = PublishRequest(bus, PublishInput{
		Alias: inst.alias, Handle: handle, ArrowIPCStream: stream, KeepAfterClose: inst.keep,
	})
	inst.mu.Lock()
	inst.err = err
	if err == nil {
		inst.last = res
		inst.gen++
	}
	inst.mu.Unlock()
	return
}

// PublishRecord encodes rec as an Arrow IPC stream and publishes it.
func (inst *Publisher) PublishRecord(bus app.BusI, rec arrow.RecordBatch) (res PublishResult, err error) {
	stream, err := EncodeRecord(rec)
	if err != nil {
		inst.mu.Lock()
		inst.err = err
		inst.mu.Unlock()
		return
	}
	return inst.Publish(bus, stream)
}

// Handle is the held handle — empty before the first successful publish.
func (inst *Publisher) Handle() (h string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.last.Handle
}

// Last reports the last successful publish, the generation it was (which
// increments once per success, so a render thread that remembers the last
// generation it acted on reacts exactly once), and the last error.
func (inst *Publisher) Last() (res PublishResult, gen uint64, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.last, inst.gen, inst.err
}

// Retract withdraws the held dataset, if any, and forgets the handle. The
// runtime retracts a window's datasets when the window closes (ADR-0240
// §SD5), so this is for a publisher that wants its dataset gone earlier.
func (inst *Publisher) Retract(bus app.BusI) (err error) {
	inst.mu.Lock()
	handle := inst.last.Handle
	inst.last = PublishResult{}
	inst.mu.Unlock()
	if handle == "" || bus == nil {
		return nil
	}
	return RetractRequest(bus, handle)
}

// EncodeRecord writes one record batch as the Arrow IPC stream a publish
// carries. An empty batch still emits the schema, so a computation that
// produced no rows publishes an empty table rather than failing — zero
// rows is a legitimate answer.
func EncodeRecord(rec arrow.RecordBatch) (stream []byte, err error) {
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(rec.Schema()))
	if err = w.Write(rec); err != nil {
		return nil, eh.Errorf("adhocdata: write arrow record: %w", err)
	}
	if err = w.Close(); err != nil {
		return nil, eh.Errorf("adhocdata: close arrow stream: %w", err)
	}
	return buf.Bytes(), nil
}
