package ladingadapter

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Snapshot is one complete snapshot of one mount, as its commit record
// describes it.
//
// Every field comes off the root row, so a snapshot that appears here is by
// definition complete: the row exists only because a walk finished writing it
// (ADR-0198 §SD6).
type Snapshot struct {
	// Snap is the instant that names it — what [Open] pins to.
	Snap time.Time
	// ExpiresAt is when its rows leave the store.
	ExpiresAt time.Time
	// Entries and Bytes are the walk's own totals.
	Entries uint64
	Bytes   uint64
	// The policy as it was applied, not as it is declared now.
	TtlClass  string
	TextRule  string
	InlineMax uint64
}

// Snapshots lists a mount's complete snapshots, newest first.
//
// It reads the snapshot index rather than the entry table. The two hold the
// same rows — the index is a materialised view of the root rows — but asking
// the entry table would mean scanning every path of every snapshot to find the
// handful that are root rows, which is exactly the cost the index exists to
// remove.
//
// Expired-but-not-yet-dropped rows are excluded: `TTL` reclaims space at merge
// time, so a row can outlive its expiry on disk, and a reader that ignored
// that would hand back a snapshot whose entries may already be gone.
func Snapshots(ctx context.Context, exec recordstore.ExecutorI, mount identifier.TaggedId) (out []Snapshot, err error) {
	return SnapshotsIn(ctx, exec, ladingschema.Layout{}, mount)
}

// SnapshotsIn is [Snapshots] over a store in the layout's database.
func SnapshotsIn(ctx context.Context, exec recordstore.ExecutorI, layout ladingschema.Layout, mount identifier.TaggedId) (out []Snapshot, err error) {
	if exec == nil {
		return nil, eh.Errorf("no executor")
	}
	if !mount.IsValid() {
		return nil, eh.Errorf("mount id is not a valid tagged id")
	}
	st := lading.SnapshotIndexIn(exec, layout)
	defer st.Close()
	pred := fmt.Sprintf("%s = %d AND %s", ladingschema.ColID, mount.Value(), ladingschema.NotExpired)
	for ent, serr := range st.ScanLadingSnapshot(ctx, recordstore.ScanOpts{ExtraPredicate: pred}) {
		if serr != nil {
			return nil, serr
		}
		if !ent.LadingSnapshot.Has {
			continue
		}
		s := ent.LadingSnapshot.Val
		out = append(out, Snapshot{
			Snap:      ent.Ts.UTC(),
			ExpiresAt: ent.ExpiresAt.UTC(),
			Entries:   s.Entries,
			Bytes:     s.Bytes,
			TtlClass:  s.TtlClass,
			TextRule:  s.TextRule,
			InlineMax: s.InlineMax,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Snap.After(out[j].Snap) })
	return
}

// Mounts lists every mount of the store that has at least one complete,
// unexpired snapshot, in ascending id order.
//
// "Has a complete snapshot" rather than "exists": a mount whose only walk died
// half way has rows and nothing to show, and listing it would name something a
// caller cannot open. Which of these a given caller may *see* is a separate
// question — this states what is there, and a visibility check filters it.
//
// One pass over the index, which is one row per snapshot rather than one per
// path of every snapshot.
func Mounts(ctx context.Context, exec recordstore.ExecutorI) (mounts []identifier.TaggedId, err error) {
	return MountsIn(ctx, exec, ladingschema.Layout{})
}

// MountsIn is [Mounts] over a store in the layout's database.
func MountsIn(ctx context.Context, exec recordstore.ExecutorI, layout ladingschema.Layout) (mounts []identifier.TaggedId, err error) {
	if exec == nil {
		return nil, eh.Errorf("no executor")
	}
	st := lading.SnapshotIndexIn(exec, layout)
	defer st.Close()

	seen := map[identifier.TaggedId]struct{}{}
	for ent, serr := range st.ScanLadingSnapshot(ctx, recordstore.ScanOpts{
		ExtraPredicate: ladingschema.NotExpired,
	}) {
		if serr != nil {
			return nil, serr
		}
		if !ent.LadingSnapshot.Has {
			continue
		}
		seen[identifier.TaggedId(ent.ID)] = struct{}{}
	}
	mounts = make([]identifier.TaggedId, 0, len(seen))
	for m := range seen {
		mounts = append(mounts, m)
	}
	sort.Slice(mounts, func(i, j int) bool { return mounts[i] < mounts[j] })
	return
}

// Latest is the newest complete snapshot of a mount, and reports whether there
// is one.
//
// "Newest complete" is one thing, not two: an incomplete walk has no root row,
// so it is not in the index and cannot be picked here.
func Latest(ctx context.Context, exec recordstore.ExecutorI, mount identifier.TaggedId) (snap Snapshot, found bool, err error) {
	return LatestIn(ctx, exec, ladingschema.Layout{}, mount)
}

// LatestIn is [Latest] over a store in the layout's database.
func LatestIn(ctx context.Context, exec recordstore.ExecutorI, layout ladingschema.Layout, mount identifier.TaggedId) (snap Snapshot, found bool, err error) {
	all, err := SnapshotsIn(ctx, exec, layout, mount)
	if err != nil || len(all) == 0 {
		return
	}
	return all[0], true, nil
}

// OpenLatest pins a mount's newest complete snapshot.
//
// The two calls it composes are not atomic, and deliberately are not: what it
// returns is a view of one snapshot, and that snapshot does not change. A walk
// finishing in between means the next call picks a newer one, never that this
// one shifts underneath its reader.
func OpenLatest(ctx context.Context, exec recordstore.ExecutorI, st lading.Stores, mount identifier.TaggedId, opts ...Option) (inst *FS, found bool, err error) {
	latest, found, err := Latest(ctx, exec, mount)
	if err != nil || !found {
		return
	}
	inst, err = Open(st, mount, latest.Snap, opts...)
	return inst, err == nil, err
}
