package coveragebus

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stretchr/testify/require"
)

type fakeSampler struct {
	seq    atomic.Uint64
	closed atomic.Bool
}

func (f *fakeSampler) Sample() (upd *covsnap.Update, err error) {
	seq := f.seq.Add(1)
	return &covsnap.Update{
		Seq:    seq,
		Full:   seq == 1,
		Units:  roaring.BitmapOf(uint32(seq)),
		Status: covsnap.RunStatus{CoveredUnits: uint32(seq)},
	}, nil
}

func (f *fakeSampler) Close() (err error) {
	f.closed.Store(true)
	return nil
}

// Producer -> inprocbus -> Consumer, with real capability enforcement on
// both clients: the publish leg under the service identity, the subscribe
// leg under a consumer identity, exactly the carousel/provider split.
func TestProducerConsumerEndToEnd(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	pub := bus.NewClient(ServiceAppId, []app.SubjectFilter{
		{Pattern: SubjectWildcard, Direction: app.CapDirectionPub},
	})
	sub := bus.NewClient("test.covconsumer", []app.SubjectFilter{
		{Pattern: SubjectWildcard, Direction: app.CapDirectionSub},
	})

	got := make(chan *covsnap.Update, 8)
	consumer, err := NewConsumer(ConsumerOptions{
		Bus:     sub,
		Subject: SampleSubjectWildcard(),
		Codec:   NewCBORCodec(),
		Handler: func(upd *covsnap.Update) { got <- upd },
		Log:     zerolog.Nop(),
	})
	require.NoError(t, err)
	require.NoError(t, consumer.Start())

	fs := &fakeSampler{}
	producer, err := NewProducer(ProducerOptions{
		Sampler:  fs,
		Bus:      pub,
		Subject:  SampleSubject("testhost"),
		Codec:    NewCBORCodec(),
		Interval: MinInterval,
		Log:      zerolog.Nop(),
	})
	require.NoError(t, err)
	producer.Start(context.Background())

	select {
	case upd := <-got:
		require.EqualValues(t, 1, upd.Seq)
		require.True(t, upd.Full)
		require.True(t, upd.Units.Contains(1))
		require.EqualValues(t, 1, upd.Status.CoveredUnits)
	case <-time.After(3 * time.Second):
		t.Fatal("no update delivered")
	}

	require.NoError(t, producer.Close())
	require.True(t, fs.closed.Load(), "producer owns and closes the sampler")
	require.NoError(t, consumer.Close())
}
