package runtime

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentGoroutineIdDiffersAcrossGoroutines(t *testing.T) {
	here := currentGoroutineId()
	require.NotZero(t, here)
	assert.Equal(t, here, currentGoroutineId())
	other := make(chan uint64)
	go func() { other <- currentGoroutineId() }()
	assert.NotEqual(t, here, <-other)
}

// growStackThenToken forces the stack to be copied several times before
// reading the token, which must not move with it.
//
//go:noinline
func growStackThenToken(n int) uintptr {
	var pad [256]byte
	_ = pad
	if n == 0 {
		return goroutineToken()
	}
	return growStackThenToken(n - 1)
}

func TestGoroutineTokenStableAndDistinct(t *testing.T) {
	here := goroutineToken()
	require.NotZero(t, here)
	assert.Equal(t, here, growStackThenToken(10_000))

	// Every goroutine stays alive until all tokens are compared: a token is
	// only distinct among live goroutines.
	const n = 256
	tokens := make([]uintptr, n)
	var ready, done sync.WaitGroup
	release := make(chan struct{})
	ready.Add(n)
	done.Add(n)
	for i := range n {
		go func() {
			defer done.Done()
			tokens[i] = goroutineToken()
			ready.Done()
			<-release
		}()
	}
	ready.Wait()
	seen := map[uintptr]struct{}{here: {}}
	for _, tok := range tokens {
		seen[tok] = struct{}{}
	}
	close(release)
	done.Wait()
	assert.Len(t, seen, n+1)
}

func TestBoundChannelPanicsOffItsGoroutine(t *testing.T) {
	f := NewFffi2[*Unmarshaller](nil)
	f.BindToCurrentGoroutine()
	// Captured messages never reach the nil channel, so the owner check is
	// the only thing a send exercises here.
	f.BeginCapture(nil, nil)
	f.EndCapture()

	recovered := make(chan any)
	go func() {
		defer func() { recovered <- recover() }()
		f.BeginCapture(nil, nil)
	}()
	r := <-recovered
	require.NotNil(t, r, "a capture scope opened off the bound goroutine panics")
	assert.Contains(t, r, "ADR-0261")
	assert.Contains(t, r, fmt.Sprintf("render goroutine %d", currentGoroutineId()))
}

func TestUnboundChannelDoesNotCheck(t *testing.T) {
	f := NewFffi2[*Unmarshaller](nil)
	done := make(chan any)
	go func() {
		defer func() { done <- recover() }()
		f.BeginCapture(nil, nil)
		f.EndCapture()
	}()
	assert.Nil(t, <-done)
}

func TestMessagesCountsCapturedSends(t *testing.T) {
	f := NewFffi2[*Unmarshaller](nil)
	f.BeginCapture(new(bytes.Buffer), binary.LittleEndian)
	require.NoError(t, f.SendIntermediate([]byte{1, 2}))
	require.NoError(t, f.SendIntermediate([]byte{3}))
	f.EndCapture()
	assert.EqualValues(t, 2, f.Messages())
}

// BenchmarkCheckOwnerBound is the per-message cost the check adds once a
// channel is bound (ADR-0261).
func BenchmarkCheckOwnerBound(b *testing.B) {
	f := NewFffi2[*Unmarshaller](nil)
	f.BindToCurrentGoroutine()
	for b.Loop() {
		f.checkOwner()
	}
}
