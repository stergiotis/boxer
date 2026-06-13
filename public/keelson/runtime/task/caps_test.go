package task

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestProducerCaps_HasBothDirectionOnTaskPrefix(t *testing.T) {
	caps := ProducerCaps()
	require := assert.New(t)
	require.Len(caps, 1)
	require.Equal(PatternAll, caps[0].Pattern)
	require.Equal(app.CapDirectionBoth, caps[0].Direction)
	require.NotEmpty(caps[0].Reason)
}

func TestObserverCaps_IsSubOnly(t *testing.T) {
	caps := ObserverCaps()
	require := assert.New(t)
	require.Len(caps, 1)
	require.Equal(PatternAll, caps[0].Pattern)
	require.Equal(app.CapDirectionSub, caps[0].Direction)
}

func TestCancelerCaps_IsCancelPubOnly(t *testing.T) {
	caps := CancelerCaps()
	require := assert.New(t)
	require.Len(caps, 1)
	require.Equal(PatternCancelAll, caps[0].Pattern)
	require.Equal(app.CapDirectionPub, caps[0].Direction)
}
