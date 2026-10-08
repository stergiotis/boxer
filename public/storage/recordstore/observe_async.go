package recordstore

import (
	"sync"
	"sync/atomic"
)

// DefaultAsyncObserverBuffer bounds the in-flight events of an AsyncObserver
// built with buffer <= 0.
const DefaultAsyncObserverBuffer = 4096

// AsyncObserver decouples an observer from the calls it observes (ADR-0295
// §SD5), as keelson's audit.AsyncSink does for bus audit records: ObserveCall
// enqueues into a bounded buffer and returns at once, and one goroutine
// forwards to the inner observer in order. On overflow the event is dropped
// and Dropped is bumped — the call is never blocked, so the record is
// best-effort. Close drains the buffer and stops the goroutine.
//
// An AsyncObserver cannot be [Required]: it would acknowledge an intent it has
// not recorded.
type AsyncObserver struct {
	inner   CallObserverI
	ch      chan CallEvent
	dropped atomic.Uint64
	failed  atomic.Uint64
	stop    chan struct{}
	done    chan struct{}
	// mu orders ObserveCall against Close: an enqueue holds it shared and
	// Close exclusively, so no event can enter the buffer after Close began
	// draining it — every event is either forwarded or counted as dropped.
	mu     sync.RWMutex
	closed bool
}

var _ CallObserverI = (*AsyncObserver)(nil)

// NewAsyncObserver wraps inner with a background forwarder. buffer <= 0
// selects DefaultAsyncObserverBuffer. A nil inner accepts and discards.
func NewAsyncObserver(inner CallObserverI, buffer int) (inst *AsyncObserver) {
	if buffer <= 0 {
		buffer = DefaultAsyncObserverBuffer
	}
	inst = &AsyncObserver{
		inner: inner,
		ch:    make(chan CallEvent, buffer),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	go inst.loop()
	return
}

// ObserveCall enqueues ev and never blocks. It returns nil: what becomes of
// the event is counted by Dropped and Failed instead.
func (inst *AsyncObserver) ObserveCall(ev CallEvent) error {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	if inst.closed {
		inst.dropped.Add(1)
		return nil
	}
	select {
	case inst.ch <- ev:
	default:
		inst.dropped.Add(1)
	}
	return nil
}

func (inst *AsyncObserver) forward(ev CallEvent) {
	if inst.inner == nil {
		return
	}
	if inst.inner.ObserveCall(ev) != nil {
		inst.failed.Add(1)
	}
}

func (inst *AsyncObserver) loop() {
	defer close(inst.done)
	for {
		select {
		case ev := <-inst.ch:
			inst.forward(ev)
		case <-inst.stop:
			for {
				select {
				case ev := <-inst.ch:
					inst.forward(ev)
				default:
					return
				}
			}
		}
	}
}

// Close stops accepting events, drains the buffer into the inner observer,
// and waits for the forwarder to exit. Idempotent.
func (inst *AsyncObserver) Close() {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return
	}
	inst.closed = true
	inst.mu.Unlock()
	close(inst.stop)
	<-inst.done
}

// Dropped counts events discarded because the buffer was full or the
// observer closed. A growing value means the inner observer cannot keep up.
func (inst *AsyncObserver) Dropped() uint64 { return inst.dropped.Load() }

// Failed counts events the inner observer returned an error for.
func (inst *AsyncObserver) Failed() uint64 { return inst.failed.Load() }
