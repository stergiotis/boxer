package deadletter

import (
	"context"
	"encoding/binary"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/observability/eh"
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

// Store writes dead letters through the facts-bound generated store.
type Store struct {
	Store *stevedorefacts.StevedoreStore
}

var _ StoreI = (*Store)(nil)

// Add buffers one row; Flush makes it durable.
func (inst *Store) Add(_ context.Context, row stevedorefacts.DeadLetter) (err error) {
	err = inst.Store.Begin(row.Id, row.Ts, stevedorefacts.StevedoreEnvelope{NaturalKey: row.NaturalKey}).
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

// Identity derives a row's id and natural key from where the message or
// request sat — a topic, a partition and an offset, or a source name and a
// position — so a redelivered failure is the same row again.
func Identity(topic string, partition int32, offset int64) (id uint64, key []byte) {
	h := blake3.New(32, nil)
	_, _ = h.Write([]byte(topic))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strconv.FormatInt(int64(partition), 10)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strconv.FormatInt(offset, 10)))
	key = h.Sum(nil)
	return binary.BigEndian.Uint64(key[:8]), key
}
