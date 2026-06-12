// Package storagetest is the executable conformance contract for
// [repo.StorageI] implementations. Run drives the full suite; each
// requirement is also a Check* function returning an error so suites
// can be meta-tested against deliberately broken stores.
//
// Implementors: the factory you pass to Run must open a store over the
// given location string, and OPENING THE SAME LOCATION AGAIN after
// Close must observe everything previously written — the suite checks
// durability across reopen, which is the property the engine's
// crash-recovery depends on.
package storagetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
)

// OpenFunc opens (creating on first use) a store at location.
type OpenFunc func(location string) (repo.StorageI, error)

func h(b byte) (out t.PatchHash) {
	for i := range out {
		out[i] = b
	}
	return
}

// CheckEnvelopes: put/get/has round-trip, idempotent re-put,
// first-write-wins immutability, ErrEnvelopeNotFound on miss.
func CheckEnvelopes(ctx context.Context, open OpenFunc, location string) (err error) {
	st, err := open(location)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer st.Close()

	data := []byte("PXE1\x05json1{\"x\":1}")
	if err = st.PutEnvelope(ctx, h(1), data); err != nil {
		return fmt.Errorf("put: %w", err)
	}
	if err = st.PutEnvelope(ctx, h(1), data); err != nil {
		return fmt.Errorf("idempotent re-put: %w", err)
	}
	got, err := st.GetEnvelope(ctx, h(1))
	if err != nil {
		return fmt.Errorf("get: %w", err)
	}
	if !bytes.Equal(got, data) {
		return fmt.Errorf("get returned %q, want %q", got, data)
	}
	ok, err := st.HasEnvelope(ctx, h(1))
	if err != nil || !ok {
		return fmt.Errorf("has(present) = %v, %v", ok, err)
	}
	ok, err = st.HasEnvelope(ctx, h(2))
	if err != nil || ok {
		return fmt.Errorf("has(absent) = %v, %v", ok, err)
	}
	if _, err2 := st.GetEnvelope(ctx, h(2)); !errors.Is(err2, repo.ErrEnvelopeNotFound) {
		return fmt.Errorf("get(absent): want ErrEnvelopeNotFound, got %v", err2)
	}
	// First write wins: a different payload for the same hash must not
	// replace the original (envelopes are immutable).
	if err = st.PutEnvelope(ctx, h(1), []byte("OTHER")); err != nil {
		return fmt.Errorf("re-put different bytes: %w", err)
	}
	got, err = st.GetEnvelope(ctx, h(1))
	if err != nil || !bytes.Equal(got, data) {
		return fmt.Errorf("envelope mutated by re-put: %q, %v", got, err)
	}
	return
}

// CheckAppliedLog: append order, replace semantics, empty-on-fresh.
func CheckAppliedLog(ctx context.Context, open OpenFunc, location string) (err error) {
	st, err := open(location)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer st.Close()

	got, err := st.LoadApplied(ctx)
	if err != nil || len(got) != 0 {
		return fmt.Errorf("fresh load = %v, %v (want empty, nil)", got, err)
	}
	want := []t.PatchHash{h(1), h(2), h(3)}
	for _, x := range want {
		if err = st.AppendApplied(ctx, x); err != nil {
			return fmt.Errorf("append: %w", err)
		}
	}
	got, err = st.LoadApplied(ctx)
	if err != nil {
		return fmt.Errorf("load: %w", err)
	}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		return fmt.Errorf("load order mismatch: %v", got)
	}
	replaced := []t.PatchHash{h(1), h(3)}
	if err = st.ReplaceApplied(ctx, replaced); err != nil {
		return fmt.Errorf("replace: %w", err)
	}
	got, err = st.LoadApplied(ctx)
	if err != nil || len(got) != 2 || got[0] != h(1) || got[1] != h(3) {
		return fmt.Errorf("post-replace load = %v, %v", got, err)
	}
	if err = st.ReplaceApplied(ctx, nil); err != nil {
		return fmt.Errorf("replace-to-empty: %w", err)
	}
	got, err = st.LoadApplied(ctx)
	if err != nil || len(got) != 0 {
		return fmt.Errorf("post-empty load = %v, %v", got, err)
	}
	return
}

// CheckSnapshot: absent on fresh, save/load round-trip, replace.
func CheckSnapshot(ctx context.Context, open OpenFunc, location string) (err error) {
	st, err := open(location)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer st.Close()

	if _, ok, err2 := st.LoadSnapshot(ctx); err2 != nil || ok {
		return fmt.Errorf("fresh snapshot = ok:%v err:%v (want absent)", ok, err2)
	}
	snapA := repo.Snapshot{Applied: []t.PatchHash{h(1), h(2)}, Graggle: []byte("GRG1-bytes-A")}
	if err = st.SaveSnapshot(ctx, snapA); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	got, ok, err := st.LoadSnapshot(ctx)
	if err != nil || !ok {
		return fmt.Errorf("load: ok:%v err:%v", ok, err)
	}
	if len(got.Applied) != 2 || got.Applied[0] != h(1) || got.Applied[1] != h(2) || !bytes.Equal(got.Graggle, snapA.Graggle) {
		return fmt.Errorf("snapshot round-trip mismatch: %+v", got)
	}
	snapB := repo.Snapshot{Applied: nil, Graggle: []byte("GRG1-bytes-B")}
	if err = st.SaveSnapshot(ctx, snapB); err != nil {
		return fmt.Errorf("re-save: %w", err)
	}
	got, ok, err = st.LoadSnapshot(ctx)
	if err != nil || !ok || len(got.Applied) != 0 || !bytes.Equal(got.Graggle, snapB.Graggle) {
		return fmt.Errorf("snapshot replace mismatch: %+v ok:%v err:%v", got, ok, err)
	}
	return
}

// CheckReopenDurability: everything written before Close is observable
// after reopening the same location — the property crash recovery
// stands on.
func CheckReopenDurability(ctx context.Context, open OpenFunc, location string) (err error) {
	st, err := open(location)
	if err != nil {
		return fmt.Errorf("open #1: %w", err)
	}
	data := []byte("ENVELOPE-BYTES")
	if err = st.PutEnvelope(ctx, h(7), data); err != nil {
		return err
	}
	if err = st.AppendApplied(ctx, h(7)); err != nil {
		return err
	}
	if err = st.SaveSnapshot(ctx, repo.Snapshot{Applied: []t.PatchHash{h(7)}, Graggle: []byte("G")}); err != nil {
		return err
	}
	if err = st.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	st2, err := open(location)
	if err != nil {
		return fmt.Errorf("open #2: %w", err)
	}
	defer st2.Close()
	got, err := st2.GetEnvelope(ctx, h(7))
	if err != nil || !bytes.Equal(got, data) {
		return fmt.Errorf("envelope did not survive reopen: %q, %v", got, err)
	}
	applied, err := st2.LoadApplied(ctx)
	if err != nil || len(applied) != 1 || applied[0] != h(7) {
		return fmt.Errorf("applied log did not survive reopen: %v, %v", applied, err)
	}
	snap, ok, err := st2.LoadSnapshot(ctx)
	if err != nil || !ok || len(snap.Applied) != 1 {
		return fmt.Errorf("snapshot did not survive reopen: %+v ok:%v err:%v", snap, ok, err)
	}
	return
}

// Run executes the full conformance suite. Each check gets a fresh
// location under t.TempDir().
func Run(tt *testing.T, open OpenFunc) {
	tt.Helper()
	ctx := context.Background()
	checks := []struct {
		name  string
		check func(context.Context, OpenFunc, string) error
	}{
		{"Envelopes", CheckEnvelopes},
		{"AppliedLog", CheckAppliedLog},
		{"Snapshot", CheckSnapshot},
		{"ReopenDurability", CheckReopenDurability},
	}
	for _, c := range checks {
		tt.Run(c.name, func(tt *testing.T) {
			if err := c.check(ctx, open, tt.TempDir()); err != nil {
				tt.Fatal(err)
			}
		})
	}
}
