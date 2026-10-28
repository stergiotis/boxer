package llm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// streamingProvider is a fakeProvider that streams: deltas are handed on
// gap apart, then the scripted response; with untilCancel it keeps writing
// until the call is stopped.
type streamingProvider struct {
	fakeProvider
	deltas      []openaichat.Delta
	gap         time.Duration
	untilCancel bool
	started     chan struct{}
}

func (f *streamingProvider) CompleteStream(ctx context.Context, req openaichat.CompletionRequest, onDelta func(openaichat.Delta)) (openaichat.CompletionResponse, error) {
	f.calls.Add(1)
	f.seen = req
	if f.started != nil {
		f.started <- struct{}{}
	}
	for i := 0; i < len(f.deltas) || f.untilCancel; i++ {
		if ctx.Err() != nil {
			return openaichat.CompletionResponse{}, ctx.Err()
		}
		if i < len(f.deltas) {
			onDelta(f.deltas[i])
		} else {
			onDelta(openaichat.Delta{Content: "x"})
		}
		time.Sleep(f.gap)
	}
	return f.resp, f.err
}

func streamCfg(p *streamingProvider) Config {
	return Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", MaxTokens: 64, Client: p}
}

// collector gathers what CompleteStreaming hands on, and notes a delta
// that arrives after the call returned.
type collector struct {
	mu        sync.Mutex
	n         int
	content   strings.Builder
	reasoning strings.Builder
	tools     []string
	returned  atomic.Bool
	late      atomic.Bool
}

func (c *collector) onDelta(d Delta) {
	if c.returned.Load() {
		c.late.Store(true)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	c.content.WriteString(d.Content)
	c.reasoning.WriteString(d.Reasoning)
	c.tools = append(c.tools, d.Tools...)
}

// The deltas a streaming provider writes reach the requester in order and
// whole, coalesced, before CompleteStreaming returns the reply — which is
// what Complete gives for the same exchange.
func TestStreamedDeltasArriveInOrderBeforeTheReply(t *testing.T) {
	resp := openaichat.CompletionResponse{Content: "hello world, at length", Reasoning: "think", FinishReason: "tool_calls",
		ToolCalls: []openaichat.ToolCall{{Id: "1", Name: "look", Arguments: "{}"}}, InputTokens: 3, OutputTokens: 7}
	var deltas []openaichat.Delta
	deltas = append(deltas, openaichat.Delta{Reasoning: "th"}, openaichat.Delta{Reasoning: "ink"})
	for _, w := range strings.SplitAfter(resp.Content, " ") {
		deltas = append(deltas, openaichat.Delta{Content: w})
	}
	deltas = append(deltas, openaichat.Delta{ToolCall: "look"})
	p := &streamingProvider{fakeProvider: fakeProvider{resp: resp}, deltas: deltas, gap: 30 * time.Millisecond}
	cli, _, _ := serve(t, streamCfg(p))

	var c collector
	res, err := cli.CompleteStreaming(context.Background(), Request{Messages: userMessage()}, c.onDelta)
	c.returned.Store(true)
	require.NoError(t, err)
	assert.Equal(t, resp.Content, c.content.String())
	assert.Equal(t, resp.Reasoning, c.reasoning.String())
	assert.Equal(t, []string{"look"}, c.tools)
	assert.Greater(t, c.n, 1, "streamed rather than all at the end")

	plain, err := cli.Complete(context.Background(), Request{Messages: userMessage()})
	require.NoError(t, err)
	assert.Equal(t, plain.Content, res.Content)
	assert.Equal(t, plain.ToolCalls, res.ToolCalls)
	assert.Equal(t, plain.OutputTokens, res.OutputTokens)
	time.Sleep(3 * deltaEvery)
	assert.False(t, c.late.Load(), "no delta after the reply")
}

// A plain call to a streaming provider, and a streaming call to one that
// does not stream, both get the answer, and no deltas.
func TestStreamingIsOptInOnBothSides(t *testing.T) {
	resp := openaichat.CompletionResponse{Content: "plain", FinishReason: "stop"}
	p := &streamingProvider{fakeProvider: fakeProvider{resp: resp}, deltas: []openaichat.Delta{{Content: "plain"}}}
	cli, _, _ := serve(t, streamCfg(p))
	res, err := cli.Complete(context.Background(), Request{Messages: userMessage()})
	require.NoError(t, err)
	assert.Equal(t, "plain", res.Content)

	plain := &fakeProvider{resp: resp}
	cli, _, _ = serve(t, localCfg(plain))
	var c collector
	res, err = cli.CompleteStreaming(context.Background(), Request{Messages: userMessage()}, c.onDelta)
	require.NoError(t, err)
	assert.Equal(t, "plain", res.Content)
	assert.Zero(t, c.n)
}

// A call the wall refuses never reaches the provider, so publishes nothing.
func TestARefusedCallStreamsNothing(t *testing.T) {
	p := &streamingProvider{fakeProvider: fakeProvider{resp: openaichat.CompletionResponse{Content: "secret"}}, deltas: []openaichat.Delta{{Content: "secret"}}}
	cli, _, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: p})
	var c collector
	_, err := cli.CompleteStreaming(context.Background(), Request{Sensitivity: queryengine.SensitivityConfined, Messages: userMessage()}, c.onDelta)
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Zero(t, c.n)
	assert.Zero(t, p.calls.Load())
}

// The service streams only to an inbox the requester holds: a client that
// names another client's inbox — learned, say, from a request it observed
// — gets its answer, and the other inbox nothing.
func TestAStreamInboxMustBeTheSenders(t *testing.T) {
	p := &streamingProvider{fakeProvider: fakeProvider{resp: openaichat.CompletionResponse{Content: "steered", FinishReason: "stop"}},
		deltas: []openaichat.Delta{{Content: "steered"}}}
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := NewService(bus, zerolog.Nop(), streamCfg(p))
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	var hits atomic.Int32
	inbox, unsubscribe, err := bus.NewClient("victim", nil).SubscribeInbox(func(*app.Msg) { hits.Add(1) })
	require.NoError(t, err)
	defer unsubscribe()

	other := bus.NewClient(appId, ClientCaps("test: ask"))
	payload, err := encode(wireRequest{V: wireVersion, Messages: userMessage(), CancelKey: "k", StreamInbox: inbox})
	require.NoError(t, err)
	raw, err := other.RequestWithTimeout(SubjectComplete, payload, 5*time.Second)
	require.NoError(t, err)
	rep, err := decode[wireReply](raw)
	require.NoError(t, err)
	assert.True(t, rep.Ok, rep.Reason)
	assert.Equal(t, "steered", rep.Content)
	assert.EqualValues(t, 1, p.calls.Load())
	assert.Zero(t, hits.Load(), "nothing reaches an inbox the sender does not hold")
}

// Cancelling the requester stops the provider and the deltas with it:
// nothing is handed on once CompleteStreaming has returned.
func TestACancelStopsTheDeltas(t *testing.T) {
	p := &streamingProvider{untilCancel: true, gap: 5 * time.Millisecond, started: make(chan struct{}, 1)}
	cli, svc, _ := serve(t, streamCfg(p))
	ctx, cancel := context.WithCancel(context.Background())
	var c collector
	errc := make(chan error, 1)
	go func() {
		_, err := cli.CompleteStreaming(ctx, Request{Messages: userMessage()}, c.onDelta)
		c.returned.Store(true)
		errc <- err
	}()
	<-p.started
	require.Eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.n > 0 }, 5*time.Second, 5*time.Millisecond)
	cancel()
	select {
	case err := <-errc:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("CompleteStreaming did not return on cancellation")
	}
	time.Sleep(3 * deltaEvery)
	assert.False(t, c.late.Load(), "no delta after the call returned")
	require.Eventually(t, func() bool {
		calls := svc.Calls()
		return len(calls) == 1 && calls[0].Error != ""
	}, 5*time.Second, 10*time.Millisecond)
}

// The service's coalescing: a first delta goes at once, the next within
// deltaEvery waits for the timer unless deltaBytes are waiting, and
// nothing is published after close.
func TestDeltaStreamCoalesces(t *testing.T) {
	var mu sync.Mutex
	var got []wireDelta
	st := newDeltaStream("k", func(b []byte) error {
		d, err := decode[wireDelta](b)
		require.NoError(t, err)
		mu.Lock()
		got = append(got, d)
		mu.Unlock()
		return nil
	})
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(got) }
	st.add(openaichat.Delta{Content: "a"})
	require.Equal(t, 1, count(), "the first delta goes at once")
	st.add(openaichat.Delta{Content: "b"})
	st.add(openaichat.Delta{Reasoning: "r"})
	assert.Equal(t, 1, count(), "the next waits")
	require.Eventually(t, func() bool { return count() == 2 }, time.Second, 5*time.Millisecond)
	st.add(openaichat.Delta{Content: strings.Repeat("c", deltaBytes)})
	assert.Equal(t, 3, count(), "deltaBytes waiting go at once")
	st.add(openaichat.Delta{Content: "d"})
	st.close(true)
	assert.Equal(t, 4, count(), "close flushes")
	st.add(openaichat.Delta{Content: "e"})
	time.Sleep(2 * deltaEvery)
	assert.Equal(t, 4, count(), "nothing after close")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []uint32{0, 1, 2, 3}, []uint32{got[0].Seq, got[1].Seq, got[2].Seq, got[3].Seq})
	assert.Equal(t, wireDelta{V: wireVersion, CallKey: "k", Seq: 1, ContentOffset: 1, Content: "b", Reasoning: "r"}, got[1])
	assert.EqualValues(t, 2, got[2].ContentOffset)
	assert.EqualValues(t, 1, got[2].ReasoningOffset)
	assert.EqualValues(t, 2+deltaBytes, got[3].ContentOffset)
}

// A tick whose timer was stopped while it waited for the lock — the flush
// it was armed for already went — does not fire the timer armed after it.
func TestAStaleTickDoesNothing(t *testing.T) {
	n := 0
	st := newDeltaStream("k", func([]byte) error { n++; return nil })
	st.add(openaichat.Delta{Content: "a"})
	st.add(openaichat.Delta{Content: "b"})
	stale := st.gen
	st.add(openaichat.Delta{Content: strings.Repeat("c", deltaBytes)})
	st.add(openaichat.Delta{Content: "d"})
	require.Equal(t, 2, n)
	st.tick(stale)
	assert.Equal(t, 2, n, "the stale tick flushed nothing")
	require.NotNil(t, st.timer, "the armed timer is left in place")
	st.close(false)
}

// A cancelled call's waiting text is dropped at close.
func TestDeltaStreamDropsOnCancel(t *testing.T) {
	n := 0
	st := newDeltaStream("k", func([]byte) error { n++; return nil })
	st.add(openaichat.Delta{Content: "a"})
	st.add(openaichat.Delta{Content: "b"})
	st.close(false)
	time.Sleep(2 * deltaEvery)
	assert.Equal(t, 1, n)
}

// The receiver hands on contiguous deltas of its own call, and nothing
// past a gap.
func TestDeltaReceiverStopsAtAGap(t *testing.T) {
	var got []string
	r := newDeltaReceiver("k", func(d Delta) { got = append(got, d.Content) })
	for _, d := range []wireDelta{
		{CallKey: "k", Seq: 0, ContentOffset: 0, Content: "ab"},
		{CallKey: "other", Seq: 1, ContentOffset: 2, Content: "XX"},
		{CallKey: "k", Seq: 1, ContentOffset: 2, Content: "c"},
		{CallKey: "k", Seq: 3, ContentOffset: 9, Content: "lost"},
		{CallKey: "k", Seq: 4, ContentOffset: 3, Content: "d"},
	} {
		b, err := encode(d)
		require.NoError(t, err)
		r.handle(&app.Msg{Payload: b})
	}
	r.finish(true)
	assert.Equal(t, []string{"ab", "c"}, got)

	// A lost delta that carried only tool names leaves the offsets
	// contiguous; the sequence number still shows the gap.
	var tools []string
	r = newDeltaReceiver("k", func(d Delta) { tools = append(tools, d.Tools...) })
	for _, d := range []wireDelta{
		{CallKey: "k", Seq: 0, Tools: []string{"a"}},
		{CallKey: "k", Seq: 2, Tools: []string{"c"}},
	} {
		b, err := encode(d)
		require.NoError(t, err)
		r.handle(&app.Msg{Payload: b})
	}
	r.finish(true)
	assert.Equal(t, []string{"a"}, tools)
}
