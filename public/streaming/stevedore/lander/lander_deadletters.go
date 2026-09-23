package lander

import (
	"context"
	"encoding/binary"
	"strconv"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/streaming/stevedore/stevedorefacts"
)

// KindDeadLetter is the kind label every dead-letter row carries.
const KindDeadLetter = "stevedoreDeadLetter"

// DeadLettersI receives what a lander gives up on. The generated store is
// the production shape ([StoreDeadLetters]); a test keeps rows in memory.
type DeadLettersI interface {
	Add(ctx context.Context, row stevedorefacts.DeadLetter) error
	Flush(ctx context.Context) error
}

// StoreDeadLetters writes dead letters through the facts-bound generated
// store.
type StoreDeadLetters struct {
	Store *stevedorefacts.StevedoreStore
}

var _ DeadLettersI = (*StoreDeadLetters)(nil)

// Add buffers one row; Flush makes it durable.
func (inst *StoreDeadLetters) Add(_ context.Context, row stevedorefacts.DeadLetter) (err error) {
	err = inst.Store.Begin(row.Id, row.Ts, stevedorefacts.StevedoreEnvelope{NaturalKey: row.NaturalKey}).
		AddDeadLetter(row).Commit()
	if err != nil {
		err = eh.Errorf("buffer dead letter: %w", err)
	}
	return
}

// Flush ships the buffered rows.
func (inst *StoreDeadLetters) Flush(ctx context.Context) (err error) {
	_, err = inst.Store.Flush(ctx)
	if err != nil {
		err = eh.Errorf("flush dead letters: %w", err)
	}
	return
}

// deadLetterIdentity derives a row's id and natural key from the message's
// place on its topic, so a redelivered failure is the same row again.
func deadLetterIdentity(topic string, partition int32, offset int64) (id uint64, key []byte) {
	h := blake3.New(32, nil)
	_, _ = h.Write([]byte(topic))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strconv.FormatInt(int64(partition), 10)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strconv.FormatInt(offset, 10)))
	key = h.Sum(nil)
	return binary.BigEndian.Uint64(key[:8]), key
}

func newDeadLetter(ts time.Time) stevedorefacts.DeadLetter {
	return stevedorefacts.DeadLetter{Ts: ts, Kind: KindDeadLetter}
}
