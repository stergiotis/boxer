package planebus_test

import (
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/planebus"
)

type sample struct {
	Host  string
	Value int64
	Tags  []string
}

const wildcard = "testplane.>"

func clients(t *testing.T) (pub app.BusI, sub app.BusI, inst *inprocbus.Inst) {
	t.Helper()
	inst = inprocbus.NewInst(zerolog.Nop())
	pub = inst.NewClient("producer", []app.SubjectFilter{{Pattern: wildcard, Direction: app.CapDirectionPub}})
	sub = inst.NewClient("consumer", []app.SubjectFilter{{Pattern: wildcard, Direction: app.CapDirectionSub}})
	return
}

func publish(t *testing.T, bus app.BusI, subject string, v *sample) {
	t.Helper()
	b, err := planebus.CBORCodec[sample]{}.Encode(v)
	require.NoError(t, err)
	require.NoError(t, bus.Publish(subject, b))
}

func TestCBORCodecRoundTrip(t *testing.T) {
	c := planebus.CBORCodec[sample]{}
	in := &sample{Host: "h", Value: 42, Tags: []string{"a", "b"}}
	b, err := c.Encode(in)
	require.NoError(t, err)
	out, err := c.Decode(b)
	require.NoError(t, err)
	require.Equal(t, in, out)

	_, err = c.Encode(nil)
	require.Error(t, err)
	_, err = c.Decode([]byte{0xff, 0x00})
	require.Error(t, err)
}

func TestNewConsumerValidates(t *testing.T) {
	_, sub, _ := clients(t)
	h := func(string, *sample) {}
	codec := planebus.CBORCodec[sample]{}
	for _, opts := range []planebus.ConsumerOptions[sample]{
		{Subject: wildcard, Codec: codec, Handler: h},
		{Bus: sub, Codec: codec, Handler: h},
		{Bus: sub, Subject: wildcard, Handler: h},
		{Bus: sub, Subject: wildcard, Codec: codec},
	} {
		_, err := planebus.NewConsumer(opts)
		require.Error(t, err)
	}
}

func TestConsumerDeliversSubjectAndDropsCorruptFrames(t *testing.T) {
	pub, sub, _ := clients(t)
	type got struct {
		subject string
		v       *sample
	}
	var seen []got
	c, err := planebus.NewConsumer(planebus.ConsumerOptions[sample]{
		Bus: sub, Subject: wildcard, Codec: planebus.CBORCodec[sample]{},
		Handler: func(subject string, v *sample) { seen = append(seen, got{subject, v}) },
	})
	require.NoError(t, err)
	require.NoError(t, c.Start())
	t.Cleanup(func() { _ = c.Close() })

	require.NoError(t, pub.Publish("testplane.h1.x", []byte{0xff}))
	publish(t, pub, "testplane.h1.x", &sample{Host: "h1", Value: 1})

	require.Len(t, seen, 1)
	require.Equal(t, "testplane.h1.x", seen[0].subject)
	require.Equal(t, int64(1), seen[0].v.Value)

	require.NoError(t, c.Close())
	require.NoError(t, c.Close())
	publish(t, pub, "testplane.h1.x", &sample{Value: 2})
	require.Len(t, seen, 1)
}

func TestLatestHolderKeepsNewestPerKey(t *testing.T) {
	pub, sub, _ := clients(t)
	now := time.UnixMilli(1000)
	keyOf := func(subject string) (key string, ok bool) {
		parts := strings.Split(subject, ".")
		if len(parts) != 3 {
			return
		}
		key, ok = parts[1], true
		return
	}
	h, err := planebus.StartLatestHolder(planebus.LatestHolderOptions[sample]{
		Bus: sub, Subject: wildcard, Codec: planebus.CBORCodec[sample]{}, Key: keyOf,
		NowFunc: func() time.Time { return now },
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	require.Empty(t, h.Entries())
	publish(t, pub, "testplane.b.x", &sample{Value: 1})
	publish(t, pub, "testplane.a.x", &sample{Value: 2})
	now = time.UnixMilli(2000)
	publish(t, pub, "testplane.b.x", &sample{Value: 3})
	publish(t, pub, "testplane.malformed", &sample{Value: 4})

	es := h.Entries()
	require.Len(t, es, 2)
	require.Equal(t, "a", es[0].Key)
	require.Equal(t, int64(2), es[0].Value.Value)
	require.Equal(t, "b", es[1].Key)
	require.Equal(t, int64(3), es[1].Value.Value)
	require.Equal(t, int64(2000), es[1].ReceivedAtUnixMs)

	e, ok := h.Get("a")
	require.True(t, ok)
	require.Equal(t, "testplane.a.x", e.Subject)
	_, ok = h.Get("zzz")
	require.False(t, ok)
}

func TestStartLatestHolderNeedsKey(t *testing.T) {
	_, sub, _ := clients(t)
	_, err := planebus.StartLatestHolder(planebus.LatestHolderOptions[sample]{Bus: sub, Subject: wildcard, Codec: planebus.CBORCodec[sample]{}})
	require.Error(t, err)
}

func TestBridgeRelaysAcrossBuses(t *testing.T) {
	srcPub, srcSub, _ := clients(t)
	dstPub, dstSub, _ := clients(t)

	_, err := planebus.Bridge(nil, dstPub, wildcard)
	require.Error(t, err)

	stop, err := planebus.Bridge(srcSub, dstPub, wildcard)
	require.NoError(t, err)
	t.Cleanup(stop)

	got := make(chan string, 1)
	unsub, err := dstSub.Subscribe(wildcard, func(m *app.Msg) { got <- m.Subject + "=" + string(m.Payload) })
	require.NoError(t, err)
	t.Cleanup(unsub)

	require.NoError(t, srcPub.Publish("testplane.h.x", []byte("frame")))
	select {
	case s := <-got:
		require.Equal(t, "testplane.h.x=frame", s)
	case <-time.After(time.Second):
		t.Fatal("bridge did not relay")
	}
}
