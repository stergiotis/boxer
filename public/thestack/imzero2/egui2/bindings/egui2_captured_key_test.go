package bindings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The edge byte is a wire contract (ADR-0279 §SD1): bit 0 is down, bit 1 an
// auto-repeat press. The interpreter writes these literals, so they are
// pinned here rather than derived.
func TestCapturedKeyEdges(t *testing.T) {
	press := CapturedKey{Edges: 0b01}
	assert.True(t, press.Down())
	assert.False(t, press.Up())
	assert.False(t, press.Repeat())

	repeat := CapturedKey{Edges: 0b11}
	assert.True(t, repeat.Down())
	assert.True(t, repeat.Repeat())

	release := CapturedKey{Edges: 0b00}
	assert.True(t, release.Up())
	assert.False(t, release.Down())
	assert.False(t, release.Repeat())
}
