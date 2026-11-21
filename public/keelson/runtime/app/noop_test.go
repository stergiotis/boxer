package app

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoopBus_Publish_Errors(t *testing.T) {
	var b BusI = &NoopBus{}
	err := b.Publish("runtime.fs.dialog.read", []byte("x"))
	require.Error(t, err)
	assert.Equal(t, "runtime.fs.dialog.read", ebtest.Fields(t, err)["subject"])
}

func TestNoopBus_Subscribe_Errors(t *testing.T) {
	var b BusI = &NoopBus{}
	_, err := b.Subscribe("ch.query.boxer", func(msg *Msg) {})
	require.Error(t, err)
	assert.Equal(t, "ch.query.boxer", ebtest.Fields(t, err)["subject"])
}

func TestNoopBus_Request_Errors(t *testing.T) {
	var b BusI = &NoopBus{}
	_, err := b.Request(t.Context(), "runtime.cap.request", nil)
	require.Error(t, err)
	assert.Equal(t, "runtime.cap.request", ebtest.Fields(t, err)["subject"])
}

func TestNoopStorage_All_Error(t *testing.T) {
	var s StorageI = &NoopStorage{}
	_, _, err := s.Get(t.Context(), "foo")
	require.Error(t, err)
	err = s.Set(t.Context(), "foo", []byte("bar"))
	require.Error(t, err)
	err = s.Delete(t.Context(), "foo")
	require.Error(t, err)
}

func TestStaticMountContext_NilDepsReplaced(t *testing.T) {
	mc := NewStaticMountContext("org.test.x", zerolog.Nop(), nil, nil, nil)
	require.NotNil(t, mc.Storage())
	require.NotNil(t, mc.Bus())
	assert.Equal(t, AppIdT("org.test.x"), mc.AppId())
}

// The zero capabilities are the absent results FrameContextI documents.
func TestStaticFrameContext_AbsentCapabilities(t *testing.T) {
	mc := NewStaticMountContext("org.test.x", zerolog.Nop(), nil, nil, nil)
	fc := NewStaticFrameContext(mc)
	assert.Equal(t, AppIdT("org.test.x"), fc.AppId())
	assert.Nil(t, fc.Lifetime())
	assert.Nil(t, fc.MountValue("k"))
	assert.False(t, fc.CloseWindow("why"))
	_, err := fc.OperationGesture("op", nil)
	var refusal *OperationRefusal
	assert.ErrorAs(t, err, &refusal)
}

// A wrapper that embeds the interface forwards every capability, the
// methods added after it was written included.
func TestAWrapperForwardsCapabilities(t *testing.T) {
	mc := NewStaticMountContext("org.test.x", zerolog.Nop(), nil, nil, nil)
	mc.SetMountValue("k", "v")
	fc := NewStaticFrameContext(mc)
	fc.SetWindowFocused(false)
	type wrapper struct{ FrameContextI }
	var ctx FrameContextI = wrapper{fc}
	assert.Equal(t, "v", ctx.MountValue("k"))
	assert.False(t, ctx.WindowFocused())
}
