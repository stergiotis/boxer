package runtime

import (
	"bytes"
	"encoding/binary"
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
