package deadletter

import (
	"context"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/runinfo"
	"github.com/stergiotis/boxer/public/streaming/stevedore/stevedorefacts"
)

// Kind is the kind label every dead-letter row carries.
const Kind = "stevedoreDeadLetter"

// StoreI receives what a lander or a driver gives up on. [Store] is the
// production shape over the generated store, [Log] the one for a process
// with no store at hand; a test keeps rows in memory.
type StoreI interface {
	Add(ctx context.Context, row stevedorefacts.DeadLetter) error
	Flush(ctx context.Context) error
}

// Store writes dead letters through the facts-bound generated store, which
// must have been built with the facts table's keyed id generator for
// [IdScope] as its Keys.
type Store struct {
	Store *stevedorefacts.StevedoreStore
	// Run is the run id every row's origin carries (ADR-0330 §SD3). Empty
	// takes the process's run when runinfo was initialised, else the rows
	// are unattributed. A lander has no window, so the origin names no app
	// and no instance.
	Run string
}

var _ StoreI = (*Store)(nil)

// Add buffers one row; Flush makes it durable.
func (inst *Store) Add(ctx context.Context, row stevedorefacts.DeadLetter) (err error) {
	run := inst.Run
	if run == "" {
		if ri, riErr := runinfo.Get(); riErr == nil {
			run = ri.RunId
		}
	}
	// The row's id is resolved from its natural key by the store's keyed
	// generator, whatever the row came with (ADR-0405 §SD2): a failure
	// delivered again is the same entity.
	if row.Id, err = inst.Store.MintKeyFor(ctx, row.NaturalKey); err != nil {
		return eh.Errorf("dead letter row id: %w", err)
	}
	err = inst.Store.Begin(ctx, row.Id, row.Ts, stevedorefacts.StevedoreEnvelope{NaturalKey: row.NaturalKey}).
		AddOrigin(stevedorefacts.Origin{Id: row.Id, Run: run}).
		AddDeadLetter(row).Commit()
	if err != nil {
		err = eh.Errorf("buffer dead letter: %w", err)
	}
	return
}

// Flush ships the buffered rows.
func (inst *Store) Flush(ctx context.Context) (err error) {
	_, err = inst.Store.Flush(ctx)
	if err != nil {
		err = eh.Errorf("flush dead letters: %w", err)
	}
	return
}

// Log writes one warning line per dead letter and keeps nothing.
type Log struct {
	Logger *zerolog.Logger
}

var _ StoreI = Log{}

// Add logs the row.
func (inst Log) Add(_ context.Context, row stevedorefacts.DeadLetter) error {
	l := inst.Logger
	if l == nil {
		return nil
	}
	l.Warn().Str("class", row.Class).Str("error", row.Error).Str("origin", row.Origin).
		Str("topic", row.Topic).Int32("partition", row.Partition).Int64("offset", row.Offset).Msg("stevedore dead letter")
	return nil
}

// Flush has nothing to ship.
func (inst Log) Flush(context.Context) error { return nil }

// New starts a row at ts with the kind set.
func New(ts time.Time) stevedorefacts.DeadLetter {
	return stevedorefacts.DeadLetter{Ts: ts, Kind: Kind}
}

// IdScope is the scope a dead letter's natural key is resolved in.
const IdScope = "stevedoreDeadLetter"

// NaturalKey is a row's natural key: a digest of where the message or
// request sat — a topic, a partition and an offset, or a source name and a
// position — so a failure delivered again is recognised as the same one.
// The row's id is not computed from it: the store's keyed generator
// resolves the key to an id (ADR-0405), so a redelivery restates that id.
func NaturalKey(topic string, partition int32, offset int64) (key []byte) {
	h := blake3.New(32, nil)
	_, _ = h.Write([]byte(topic))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strconv.FormatInt(int64(partition), 10)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strconv.FormatInt(offset, 10)))
	return h.Sum(nil)
}
