package llm

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// Streamed answers (ADR-0286): the service publishes coalesced deltas to
// the requester's private inbox while the provider writes the answer, and
// the reply stays the answer. A delta is advisory — what a consumer may
// show while it waits — so a lost or late one costs nothing but the view.

// The coalescing bounds (ADR-0286 §SD2): at most one delta per
// deltaEvery, or sooner once deltaBytes of text are waiting, whatever the
// provider's chunking.
const (
	deltaEvery = 100 * time.Millisecond
	deltaBytes = 2 << 10
)

// deltaStream is the service side of one streamed call: it coalesces the
// provider's deltas and publishes them in order. Publishing happens under
// mu, so close, which the reply waits for, leaves no delta behind it.
type deltaStream struct {
	mu      sync.Mutex
	publish func(payload []byte) error
	key     string
	seq     uint32
	// content and reasoning are the text not yet published; contentAt
	// and reasoningAt where it starts in the answer so far.
	content     strings.Builder
	reasoning   strings.Builder
	contentAt   uint32
	reasoningAt uint32
	tools       []string
	last        time.Time
	// timer is the armed flush and gen its number: a tick that waited for
	// mu while its timer was stopped and another armed does nothing.
	timer  *time.Timer
	gen    uint64
	closed bool
}

func newDeltaStream(key string, publish func(payload []byte) error) (inst *deltaStream) {
	inst = &deltaStream{key: key, publish: publish}
	return
}

// add takes one provider delta: published at once when the last delta is
// deltaEvery old or deltaBytes are waiting, otherwise when the timer fires.
func (inst *deltaStream) add(d openaichat.Delta) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return
	}
	inst.content.WriteString(d.Content)
	inst.reasoning.WriteString(d.Reasoning)
	if d.ToolCall != "" {
		inst.tools = append(inst.tools, d.ToolCall)
	}
	since := time.Since(inst.last)
	if since >= deltaEvery || inst.content.Len()+inst.reasoning.Len() >= deltaBytes {
		inst.flushLocked()
		return
	}
	if inst.timer == nil {
		gen := inst.gen
		inst.timer = time.AfterFunc(deltaEvery-since, func() { inst.tick(gen) })
	}
}

func (inst *deltaStream) tick(gen uint64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if gen != inst.gen || inst.closed {
		return
	}
	inst.flushLocked()
}

func (inst *deltaStream) stopTimerLocked() {
	if inst.timer != nil {
		inst.timer.Stop()
		inst.timer = nil
		inst.gen++
	}
}

// flushLocked publishes what is waiting, if anything. A publish that fails
// ends the stream: the reply still comes.
func (inst *deltaStream) flushLocked() {
	inst.stopTimerLocked()
	if inst.content.Len() == 0 && inst.reasoning.Len() == 0 && len(inst.tools) == 0 {
		return
	}
	d := wireDelta{V: wireVersion, CallKey: inst.key, Seq: inst.seq,
		ContentOffset: inst.contentAt, Content: inst.content.String(),
		ReasoningOffset: inst.reasoningAt, Reasoning: inst.reasoning.String(), Tools: inst.tools}
	inst.seq++
	inst.contentAt += uint32(len(d.Content))
	inst.reasoningAt += uint32(len(d.Reasoning))
	inst.content.Reset()
	inst.reasoning.Reset()
	inst.tools = nil
	inst.last = time.Now()
	payload, err := encode(d)
	if err == nil {
		err = inst.publish(payload)
	}
	if err != nil {
		inst.closed = true
	}
}

// close ends the stream before the reply is sent: flush publishes what is
// waiting — a completed call's last words — and a cancelled call drops it.
func (inst *deltaStream) close(flush bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return
	}
	if flush {
		inst.flushLocked()
	} else {
		inst.stopTimerLocked()
	}
	inst.closed = true
}

// Delta is one advance of a streamed answer as [Client.CompleteStreaming]
// hands it on: text to append to the answer and to the reasoning so far,
// and the names of tool calls that began. The pieces are contiguous; once
// one goes missing, no more are handed on and the reply is the answer.
type Delta struct {
	Content   string
	Reasoning string
	Tools     []string
}

// deltaReceiver is the client side of one streamed call: the bus handler
// queues what arrives, and a goroutine of its own hands it to onDelta, so
// a slow consumer does not hold the publisher — on the in-process bus,
// the service's own goroutine.
type deltaReceiver struct {
	key     string
	onDelta func(Delta)
	mu      sync.Mutex
	queue   []wireDelta
	wake    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	// seq, contentAt and reasoningAt are what was handed on; gap is set
	// once a delta did not continue it. They belong to run's goroutine;
	// dropped is finish's word that nothing more is handed on.
	seq         uint32
	contentAt   uint32
	reasoningAt uint32
	gap         bool
	dropped     atomic.Bool
}

func newDeltaReceiver(key string, onDelta func(Delta)) (inst *deltaReceiver) {
	inst = &deltaReceiver{key: key, onDelta: onDelta, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	go inst.run()
	return
}

// handle is the inbox handler: decode, check it is this call's, queue.
func (inst *deltaReceiver) handle(msg *app.Msg) {
	d, err := decode[wireDelta](msg.Payload)
	if err != nil || d.CallKey != inst.key {
		return
	}
	inst.mu.Lock()
	inst.queue = append(inst.queue, d)
	inst.mu.Unlock()
	select {
	case inst.wake <- struct{}{}:
	default:
	}
}

func (inst *deltaReceiver) run() {
	defer close(inst.done)
	for {
		select {
		case <-inst.wake:
			inst.drain()
		case <-inst.stop:
			inst.drain()
			return
		}
	}
}

func (inst *deltaReceiver) drain() {
	inst.mu.Lock()
	q := inst.queue
	inst.queue = nil
	inst.mu.Unlock()
	for _, d := range q {
		if inst.gap || inst.dropped.Load() {
			return
		}
		if d.Seq != inst.seq || d.ContentOffset != inst.contentAt || d.ReasoningOffset != inst.reasoningAt {
			inst.gap = true
			return
		}
		inst.seq++
		inst.contentAt += uint32(len(d.Content))
		inst.reasoningAt += uint32(len(d.Reasoning))
		inst.onDelta(Delta{Content: d.Content, Reasoning: d.Reasoning, Tools: d.Tools})
	}
}

// finish ends the receiver once the inbox is unsubscribed: deltas already
// queued are handed on when the call was answered, dropped when it was
// not, and onDelta is not called after finish returns.
func (inst *deltaReceiver) finish(answered bool) {
	if !answered {
		inst.dropped.Store(true)
	}
	close(inst.stop)
	<-inst.done
}
