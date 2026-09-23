package lander

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/streaming/persisted/kafka"
	"github.com/stergiotis/boxer/public/streaming/stevedore"
	"github.com/stergiotis/boxer/public/streaming/stevedore/stevedorefacts"
)

// Config bounds one lander. The zero value retries with the default policy
// and logs nowhere.
type Config struct {
	// Retry is the in-place policy for a transient sink or store error; a
	// zero Attempts takes the default policy.
	Retry stevedore.RetryPolicy
	// Logger receives one line per dead letter and per stop; nil is silent.
	Logger *zerolog.Logger
}

// closeTimeout bounds the consumer's shutdown once the run ends.
const closeTimeout = 10 * time.Second

func (inst Config) retry() stevedore.RetryPolicy {
	if inst.Retry.Attempts == 0 {
		return stevedore.DefaultRetryPolicy()
	}
	return inst.Retry
}

func (inst Config) logger() *zerolog.Logger {
	if inst.Logger == nil {
		l := zerolog.Nop()
		return &l
	}
	return inst.Logger
}

// Lander is one consumer group's landing loop. Build it with [New]; it is
// not safe for concurrent use.
type Lander struct {
	cfg      Config
	consumer kafka.ConsumerI
	sink     stevedore.SinkI
	dead     DeadLettersI
	now      func() time.Time
	landed   uint64
	deadRows uint64
}

// New wires a lander over a consumer the application built — its topics,
// group and broker are the application's — a sink and a dead-letter store.
func New(cfg Config, consumer kafka.ConsumerI, sink stevedore.SinkI, dead DeadLettersI) *Lander {
	return &Lander{cfg: cfg, consumer: consumer, sink: sink, dead: dead, now: time.Now}
}

// Landed counts items handed to the sink since New.
func (inst *Lander) Landed() uint64 { return inst.landed }

// DeadLettered counts rows handed to the dead-letter store since New.
func (inst *Lander) DeadLettered() uint64 { return inst.deadRows }

// Run connects the consumer and lands batches until ctx ends, the consumer
// stops, or a failure no retry cured. It returns nil when ctx ended. A batch
// is acknowledged only after the sink and the dead-letter store flushed; a
// batch whose flush failed is not acknowledged, and Run returns, so the
// restart that follows redelivers it.
func (inst *Lander) Run(ctx context.Context) (err error) {
	if inst.consumer == nil || inst.sink == nil || inst.dead == nil {
		return eh.Errorf("a lander needs a consumer, a sink and a dead-letter store")
	}
	err = inst.consumer.Connect(ctx)
	if err != nil {
		return eh.Errorf("connect consumer: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
		defer cancel()
		cerr := inst.consumer.Close(closeCtx)
		if err == nil && cerr != nil {
			err = eh.Errorf("close consumer: %w", cerr)
		}
	}()
	for {
		batch, rerr := inst.consumer.Read(ctx)
		if rerr != nil {
			if errors.Is(rerr, context.Canceled) || errors.Is(rerr, context.DeadlineExceeded) {
				return nil
			}
			return eh.Errorf("read batch: %w", rerr)
		}
		err = inst.land(ctx, batch)
		if err != nil {
			if ctx.Err() != nil {
				// A stop mid-batch: nothing is acknowledged, the restart
				// redelivers, and that is not a failure.
				return nil
			}
			inst.cfg.logger().Error().Err(err).Msg("stevedore lander stops; the batch is not acknowledged")
			return
		}
	}
}

// land handles one batch: every record, the flushes, then the
// acknowledgement.
func (inst *Lander) land(ctx context.Context, batch kafka.Batch) (err error) {
	for rec := range batch.Records.RecordsAll() {
		err = inst.record(ctx, rec)
		if err != nil {
			return
		}
	}
	err = stevedore.Retry(ctx, inst.cfg.retry(), inst.sink.Flush)
	if err != nil {
		return eh.Errorf("flush sink: %w", err)
	}
	err = stevedore.Retry(ctx, inst.cfg.retry(), inst.dead.Flush)
	if err != nil {
		return eh.Errorf("flush dead letters: %w", err)
	}
	err = batch.Ack(ctx, nil)
	if err != nil {
		return eh.Errorf("acknowledge batch: %w", err)
	}
	return
}

// record handles one message. Only a failure no retry cured comes back as
// an error; everything else is landed or dead-lettered. A split item lands
// as it is: the sink keeps it as a durable part row, because a part held
// in memory past its batch's acknowledgement would be lost on a restart
// (ADR-0252, Updates).
func (inst *Lander) record(ctx context.Context, rec *kgo.Record) (err error) {
	item, derr := stevedore.DecodeItem(rec.Value)
	if derr != nil {
		return inst.deadLetter(ctx, rec, nil, derr)
	}
	lerr := stevedore.Retry(ctx, inst.cfg.retry(), func(actx context.Context) error {
		return inst.sink.Land(actx, item)
	})
	if lerr == nil {
		inst.landed++
		return
	}
	if stevedore.ClassOf(lerr) == stevedore.ClassPermanent {
		return inst.deadLetter(ctx, rec, &item, lerr)
	}
	return eh.Errorf("land item: %w", lerr)
}

// deadLetter records one message given up on, with its envelope's identity
// where it decoded.
func (inst *Lander) deadLetter(ctx context.Context, rec *kgo.Record, item *stevedore.Item, cause error) (err error) {
	row := newDeadLetter(inst.now())
	row.Id, row.NaturalKey = deadLetterIdentity(rec.Topic, rec.Partition, rec.Offset)
	if item != nil {
		row.Ref = item.Ref.Value()
		row.Origin = item.Origin
		row.Ordinal = item.Ordinal
	}
	row.Class = stevedore.ClassOf(cause).String()
	row.Error = cause.Error()
	row.Topic = rec.Topic
	row.Partition = rec.Partition
	row.Offset = rec.Offset
	row.Message = rec.Value
	inst.cfg.logger().Warn().Str("topic", rec.Topic).Int32("partition", rec.Partition).Int64("offset", rec.Offset).
		Str("class", row.Class).Err(cause).Msg("stevedore dead letter")
	return inst.addDeadLetter(ctx, row)
}

func (inst *Lander) addDeadLetter(ctx context.Context, row stevedorefacts.DeadLetter) (err error) {
	err = stevedore.Retry(ctx, inst.cfg.retry(), func(actx context.Context) error {
		return inst.dead.Add(actx, row)
	})
	if err != nil {
		return eb.Build().Str("class", row.Class).Errorf("record dead letter: %w", err)
	}
	inst.deadRows++
	return
}
