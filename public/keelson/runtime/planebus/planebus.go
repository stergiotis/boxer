package planebus

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// CodecI encodes a plane's payload to bus bytes and back.
type CodecI[T any] interface {
	Encode(v *T) (payload []byte, err error)
	Decode(payload []byte) (v *T, err error)
}

// CBORCodec encodes a payload as CBOR by reflection. A payload holding values
// CBOR cannot carry faithfully — an error interface, a bitmap — needs a codec
// of its own that maps them to a wire struct.
type CBORCodec[T any] struct{}

var _ CodecI[struct{}] = CBORCodec[struct{}]{}

func (CBORCodec[T]) Encode(v *T) (payload []byte, err error) {
	if v == nil {
		err = eh.Errorf("encode nil payload")
		return
	}
	payload, err = cbor.Marshal(v)
	if err != nil {
		err = eh.Errorf("cbor marshal: %w", err)
	}
	return
}

func (CBORCodec[T]) Decode(payload []byte) (v *T, err error) {
	var t T
	err = cbor.Unmarshal(payload, &t)
	if err != nil {
		err = eh.Errorf("cbor unmarshal: %w", err)
		return
	}
	v = &t
	return
}

// Consumer subscribes to a subject, decodes each message, and hands the
// decoded payload to its handler. It holds no state of its own.
type Consumer[T any] struct {
	bus     app.BusI
	subject string
	codec   CodecI[T]
	handler func(subject string, v *T)
	log     zerolog.Logger

	unsubscribe func()
}

// ConsumerOptions configures NewConsumer. Bus, Subject, Codec and Handler
// are required. Handler receives the concrete subject, which carries the
// tokens a wildcard subscription matched.
type ConsumerOptions[T any] struct {
	Bus     app.BusI
	Subject string
	Codec   CodecI[T]
	Handler func(subject string, v *T)
	Log     zerolog.Logger
}

// NewConsumer validates opts and returns a Consumer that is not yet
// subscribed; call Start to subscribe.
func NewConsumer[T any](opts ConsumerOptions[T]) (inst *Consumer[T], err error) {
	switch {
	case opts.Bus == nil:
		err = eh.Errorf("consumer needs a Bus")
	case opts.Subject == "":
		err = eh.Errorf("consumer needs a Subject")
	case opts.Codec == nil:
		err = eh.Errorf("consumer needs a Codec")
	case opts.Handler == nil:
		err = eh.Errorf("consumer needs a Handler")
	}
	if err != nil {
		return
	}
	inst = &Consumer[T]{
		bus:     opts.Bus,
		subject: opts.Subject,
		codec:   opts.Codec,
		handler: opts.Handler,
		log:     opts.Log,
	}
	return
}

// Start subscribes to the subject. A message that fails to decode is logged
// and dropped: one corrupt frame must not tear down the stream.
func (inst *Consumer[T]) Start() (err error) {
	unsub, err := inst.bus.Subscribe(inst.subject, func(msg *app.Msg) {
		v, derr := inst.codec.Decode(msg.Payload)
		if derr != nil {
			inst.log.Warn().Err(derr).Str("subject", msg.Subject).Msg("planebus: decode error")
			return
		}
		inst.handler(msg.Subject, v)
	})
	if err != nil {
		err = eb.Build().Str("subject", inst.subject).Errorf("consumer subscribe: %w", err)
		return
	}
	inst.unsubscribe = unsub
	return
}

// Close unsubscribes. Safe to call when never started and more than once.
func (inst *Consumer[T]) Close() (err error) {
	if inst.unsubscribe != nil {
		inst.unsubscribe()
		inst.unsubscribe = nil
	}
	return
}

// Bridge relays messages on subject from src to dst — it subscribes on one
// bus and republishes the same subject and payload on the other — and returns
// the unsubscribe func. src needs subscribe and dst publish permission for
// subject.
func Bridge(src, dst app.BusI, subject string) (stop func(), err error) {
	if src == nil || dst == nil {
		err = eh.Errorf("bridge needs both src and dst buses")
		return
	}
	stop, err = src.Subscribe(subject, func(m *app.Msg) {
		_ = dst.Publish(m.Subject, m.Payload)
	})
	if err != nil {
		err = eb.Build().Str("subject", subject).Errorf("bridge subscribe: %w", err)
		return
	}
	return
}

// Entry is one key's latest value as held by a LatestHolder.
type Entry[T any] struct {
	// Key is what the holder's KeyFunc extracted from the subject.
	Key string
	// Subject is the subject the value arrived under.
	Subject string
	// ReceivedAtUnixMs stamps local arrival; staleness is judged against
	// it, not against the publisher's clock.
	ReceivedAtUnixMs int64
	// Value is the decoded payload, never nil. Shared and immutable by
	// convention: readers must not mutate it.
	Value *T
}

// KeyFunc extracts the key a value is held under from its subject, or
// reports that the subject has an unexpected shape.
type KeyFunc func(subject string) (key string, ok bool)

// LatestHolder subscribes to a (typically wildcard) subject and keeps the
// newest value per key. Safe for concurrent use.
type LatestHolder[T any] struct {
	log   zerolog.Logger
	codec CodecI[T]
	key   KeyFunc
	nowFn func() time.Time

	mu    sync.RWMutex
	byKey map[string]Entry[T]

	consumer *Consumer[T]
}

// LatestHolderOptions configures StartLatestHolder. Bus, Subject, Codec and
// Key are required.
type LatestHolderOptions[T any] struct {
	Bus     app.BusI
	Subject string
	Codec   CodecI[T]
	Key     KeyFunc
	// NowFunc overrides the arrival clock when non-nil.
	NowFunc func() time.Time
	Log     zerolog.Logger
}

// StartLatestHolder subscribes and returns a running holder.
func StartLatestHolder[T any](opts LatestHolderOptions[T]) (inst *LatestHolder[T], err error) {
	if opts.Key == nil {
		err = eh.Errorf("latest holder needs a Key func")
		return
	}
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
	h := &LatestHolder[T]{
		log:   opts.Log,
		codec: opts.Codec,
		key:   opts.Key,
		nowFn: opts.NowFunc,
		byKey: map[string]Entry[T]{},
	}
	h.consumer, err = NewConsumer(ConsumerOptions[T]{
		Bus:     opts.Bus,
		Subject: opts.Subject,
		Codec:   opts.Codec,
		Handler: h.onValue,
		Log:     opts.Log,
	})
	if err != nil {
		return
	}
	err = h.consumer.Start()
	if err != nil {
		return
	}
	inst = h
	return
}

func (inst *LatestHolder[T]) onValue(subject string, v *T) {
	key, ok := inst.key(subject)
	if !ok {
		inst.log.Warn().Str("subject", subject).Msg("planebus: latest holder: unexpected subject shape")
		return
	}
	e := Entry[T]{Key: key, Subject: subject, ReceivedAtUnixMs: inst.nowFn().UnixMilli(), Value: v}
	inst.mu.Lock()
	inst.byKey[key] = e
	inst.mu.Unlock()
}

// Entries returns every key's latest value, sorted by key. Empty until a
// first value arrives.
func (inst *LatestHolder[T]) Entries() (out []Entry[T]) {
	inst.mu.RLock()
	out = make([]Entry[T], 0, len(inst.byKey))
	for _, e := range inst.byKey {
		out = append(out, e)
	}
	inst.mu.RUnlock()
	slices.SortFunc(out, func(a, b Entry[T]) int { return strings.Compare(a.Key, b.Key) })
	return
}

// Get returns key's latest value.
func (inst *LatestHolder[T]) Get(key string) (e Entry[T], ok bool) {
	inst.mu.RLock()
	e, ok = inst.byKey[key]
	inst.mu.RUnlock()
	return
}

// Close unsubscribes. Safe to call more than once.
func (inst *LatestHolder[T]) Close() (err error) {
	err = inst.consumer.Close()
	return
}
