package cqrsexample

import (
	"context"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// This file is the CQRS write model over the generated LedgerStore — the
// ADR-0100 event-sourcing mapping made concrete. It is deliberately
// small: an aggregate (Account), a fold, snapshot-aware rehydration
// (Load) and guarded commands (Open / Deposit / Withdraw / CloseAccount).
// There is no framework here and none intended — the point is that the
// store primitives suffice.

// Account is the write-model aggregate: the state folded from the
// account's event stream, plus the cursor the single writer needs.
type Account struct {
	ID      string
	Owner   string
	Balance uint64
	Closed  bool

	exists bool
	// nextSeq is the next event sequence — the envelope Order, assigned
	// by the caller (ADR-0100: single-writer per aggregate; optimistic
	// concurrency is deferred, a serializing layer above owns it).
	nextSeq uint64
	// replayed counts the events folded during Load — observable evidence
	// that a snapshot short-circuits replay (see the lifecycle test).
	replayed int
}

// Replayed reports how many events the last Load folded.
func (inst *Account) Replayed() int { return inst.replayed }

// snapKey is the aggregate's sibling snapshot key: snapshots live outside
// the event stream so Replay never sees them and Latest finds the newest
// one directly.
func snapKey(id string) string { return id + "/snap" }

// fold applies one event row to the state. The row's archetype IS the
// event type: exactly one event component is populated. Command-side
// guards keep the stream consistent; fold still refuses impossible
// transitions so a corrupt stream surfaces instead of wrapping around.
func (inst *Account) fold(row *LedgerEntity) (err error) {
	switch {
	case row.Opened.Has:
		inst.exists = true
		inst.Owner = row.Opened.Val.Owner
	case row.Deposited.Has:
		inst.Balance += row.Deposited.Val.Amount
	case row.Withdrawn.Has:
		if row.Withdrawn.Val.Amount > inst.Balance {
			err = eh.Errorf("event stream violates the balance invariant at seq %d", recordstore.SeqOf(row.Ts))
			return
		}
		inst.Balance -= row.Withdrawn.Val.Amount
	case row.Closed.Has:
		inst.Closed = true
	default:
		err = eh.Errorf("ledger row at seq %d carries no known event component", recordstore.SeqOf(row.Ts))
		return
	}
	inst.nextSeq = recordstore.SeqOf(row.Ts) + 1
	inst.replayed++
	return
}

// Service is the command side: rehydrate, guard the invariant, append.
// Like the store it wraps, a Service is single-goroutine.
type Service struct {
	st *LedgerStore
}

func NewService(st *LedgerStore) *Service { return &Service{st: st} }

// Load rehydrates the aggregate: restore the newest snapshot (Latest on
// the sibling key), then Replay only the events strictly after it and
// fold. A fresh aggregate replays everything from sequence 1.
func (inst *Service) Load(ctx context.Context, id string) (acct *Account, err error) {
	acct = &Account{ID: id, nextSeq: 1}
	from := recordstore.SeqTs(0)
	snap, found, err := inst.st.Latest(ctx, snapKey(id))
	if err != nil {
		err = eh.Errorf("load snapshot for %s: %w", id, err)
		return
	}
	if found && snap.AccountState.Has {
		state := snap.AccountState.Val
		acct.exists = true
		acct.Owner = state.Owner
		acct.Balance = state.Balance
		acct.Closed = state.Closed
		acct.nextSeq = state.AsOf + 1
		from = recordstore.SeqTs(state.AsOf + 1)
	}
	for ev, rerr := range inst.st.Replay(ctx, id, from, recordstore.ReplayOpts{}) {
		if rerr != nil {
			err = eh.Errorf("replay %s: %w", id, rerr)
			return
		}
		err = acct.fold(ev)
		if err != nil {
			return
		}
	}
	return
}

// append writes one event row durably: Begin at the aggregate's next
// sequence, contribute the event component, Commit, Flush (the rows are
// durable when Flush returns — one command, one Arrow insert). A failed
// command discards its buffered row: it must not ship behind the next
// command's flush.
func (inst *Service) append(ctx context.Context, acct *Account, add func(b *LedgerEntityBuilder)) (err error) {
	defer func() {
		if err != nil {
			inst.st.DiscardPending()
		}
	}()
	b := inst.st.Begin(acct.ID, recordstore.SeqTs(acct.nextSeq))
	add(b)
	err = b.Commit()
	if err != nil {
		err = eh.Errorf("append event to %s: %w", acct.ID, err)
		return
	}
	_, err = inst.st.Flush(ctx)
	if err != nil {
		err = eh.Errorf("flush event to %s: %w", acct.ID, err)
		return
	}
	acct.nextSeq++
	return
}

// Open creates the account — rejected when it already exists.
func (inst *Service) Open(ctx context.Context, id string, owner string) (err error) {
	acct, err := inst.Load(ctx, id)
	if err != nil {
		return
	}
	if acct.exists {
		err = eh.Errorf("account %s is already open", id)
		return
	}
	return inst.append(ctx, acct, func(b *LedgerEntityBuilder) {
		b.AddOpened(Opened{ID: id, Owner: owner})
	})
}

// Deposit adds funds — the account must exist and be open.
func (inst *Service) Deposit(ctx context.Context, id string, amount uint64) (err error) {
	acct, err := inst.Load(ctx, id)
	if err != nil {
		return
	}
	err = acct.mustBeActive()
	if err != nil {
		return
	}
	if amount == 0 {
		err = eh.Errorf("deposit to %s: amount must be positive", id)
		return
	}
	return inst.append(ctx, acct, func(b *LedgerEntityBuilder) {
		b.AddDeposited(Deposited{ID: id, Amount: amount})
	})
}

// Withdraw removes funds — the write-model invariant: never overdraw.
func (inst *Service) Withdraw(ctx context.Context, id string, amount uint64) (err error) {
	acct, err := inst.Load(ctx, id)
	if err != nil {
		return
	}
	err = acct.mustBeActive()
	if err != nil {
		return
	}
	if amount > acct.Balance {
		err = eh.Errorf("withdraw %d from %s: balance is %d", amount, id, acct.Balance)
		return
	}
	return inst.append(ctx, acct, func(b *LedgerEntityBuilder) {
		b.AddWithdrawn(Withdrawn{ID: id, Amount: amount})
	})
}

// CloseAccount terminates the account — a domain event, not a storage
// tombstone (the ledger keeps the full history).
func (inst *Service) CloseAccount(ctx context.Context, id string, reason string) (err error) {
	acct, err := inst.Load(ctx, id)
	if err != nil {
		return
	}
	err = acct.mustBeActive()
	if err != nil {
		return
	}
	return inst.append(ctx, acct, func(b *LedgerEntityBuilder) {
		b.AddClosed(Closed{ID: id, Reason: reason})
	})
}

// Snapshot persists the folded state under the sibling key so the next
// Load replays only what follows. Any state may be snapshotted, closed
// accounts included.
func (inst *Service) Snapshot(ctx context.Context, id string) (err error) {
	defer func() {
		if err != nil {
			inst.st.DiscardPending()
		}
	}()
	acct, err := inst.Load(ctx, id)
	if err != nil {
		return
	}
	if !acct.exists {
		err = eh.Errorf("snapshot %s: account does not exist", id)
		return
	}
	asOf := acct.nextSeq - 1
	b := inst.st.Begin(snapKey(id), recordstore.SeqTs(asOf))
	b.AddAccountState(AccountState{
		ID:      snapKey(id),
		Owner:   acct.Owner,
		Balance: acct.Balance,
		Closed:  acct.Closed,
		AsOf:    asOf,
	})
	err = b.Commit()
	if err != nil {
		err = eh.Errorf("snapshot %s commit: %w", id, err)
		return
	}
	_, err = inst.st.Flush(ctx)
	if err != nil {
		err = eh.Errorf("snapshot %s flush: %w", id, err)
	}
	return
}

func (inst *Account) mustBeActive() (err error) {
	if !inst.exists {
		err = eh.Errorf("account %s does not exist", inst.ID)
		return
	}
	if inst.Closed {
		err = eh.Errorf("account %s is closed", inst.ID)
	}
	return
}
