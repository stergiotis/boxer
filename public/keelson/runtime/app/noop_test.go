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
	err := b.Publish("fs.dialog.read", []byte("x"))
	require.Error(t, err)
	assert.Equal(t, "fs.dialog.read", ebtest.Fields(t, err)["subject"])
}

func TestNoopBus_Subscribe_Errors(t *testing.T) {
	var b BusI = &NoopBus{}
	_, err := b.Subscribe("ch.query.boxer", func(msg *Msg) {})
	require.Error(t, err)
	assert.Equal(t, "ch.query.boxer", ebtest.Fields(t, err)["subject"])
}

func TestNoopBus_Request_Errors(t *testing.T) {
	var b BusI = &NoopBus{}
	_, err := b.Request("runtime.cap.request", nil)
	require.Error(t, err)
	assert.Equal(t, "runtime.cap.request", ebtest.Fields(t, err)["subject"])
}

func TestNoopStorage_All_Error(t *testing.T) {
	var s StorageI = &NoopStorage{}
	_, _, err := s.Get("foo")
	require.Error(t, err)
	err = s.Set("foo", []byte("bar"))
	require.Error(t, err)
	err = s.Delete("foo")
	require.Error(t, err)
}

func TestStaticMountContext_NilDepsReplaced(t *testing.T) {
	mc := NewStaticMountContext("org.test.x", zerolog.Nop(), nil, nil, nil)
	require.NotNil(t, mc.Storage())
	require.NotNil(t, mc.Bus())
	assert.Equal(t, AppIdT("org.test.x"), mc.AppId())
}

func TestStaticFrameContext_EguiScope(t *testing.T) {
	mc := NewStaticMountContext("org.test.x", zerolog.Nop(), nil, nil, nil)
	fc := NewStaticFrameContext(mc, "scope-value")
	assert.Equal(t, "scope-value", fc.EguiScope())
	assert.Equal(t, AppIdT("org.test.x"), fc.AppId())
}
