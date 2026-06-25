// Package filestore is the reference [repo.StorageI] implementation: a
// transparent on-disk layout under one root directory, hardened for
// crash consistency.
//
//	<root>/applied.txt          one 64-hex hash per line, apply order
//	<root>/snapshot.bin         repo.Snapshot (own framing, see below)
//	<root>/retention.txt        one "<64-hex> <index> <unixnano>" per line
//	<root>/changes/<hh>/<hash>  framed envelopes, sharded by first byte
//	<root>/lock                 advisory inter-process lock (see lock_unix.go)
//
// Durability discipline: envelope files and the snapshot are written to
// a temp file in the destination directory, fsynced, renamed into
// place, and the directory fsynced (atomic replace). Log appends use
// O_APPEND + fsync; LoadApplied drops a torn trailing line — the engine
// never acknowledged that append. ReplaceApplied rewrites the whole log
// atomically via the same temp+rename path.
//
// The layout stays human-debuggable on purpose: envelopes are framed
// jsonv1 by default and the applied log is a text file.
package filestore

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
)

// ErrLocked is returned by Open when another live process already holds
// the store's directory lock. The engine assumes a single writer per
// store (StorageI methods run under the engine's in-memory locks); two
// processes on one root would interleave applied-log and snapshot writes
// and corrupt acknowledged state, so Open refuses rather than race.
var ErrLocked = errors.New("store directory is locked by another process")

// Store implements repo.StorageI over a root directory.
type Store struct {
	root string
	// lockFile holds the advisory whole-directory lock for this store's
	// lifetime (see acquireLock); Close releases it, and process death
	// releases it automatically (no stale lock to clean up after a crash).
	// nil on platforms without advisory locking.
	lockFile *os.File
}

var _ repo.StorageI = (*Store)(nil)

// Open creates (if needed) and opens a store rooted at dir. It takes an
// advisory inter-process lock on dir; a second Open against a root a live
// process already holds fails with ErrLocked.
func Open(dir string) (st *Store, err error) {
	if err = os.MkdirAll(filepath.Join(dir, "changes"), 0o755); err != nil {
		err = eh.Errorf("create store layout: %w", err)
		return
	}
	lockFile, lerr := acquireLock(dir)
	if lerr != nil {
		err = lerr
		return
	}
	st = &Store{root: dir, lockFile: lockFile}
	return
}

func (inst *Store) appliedPath() string  { return filepath.Join(inst.root, "applied.txt") }
func (inst *Store) snapshotPath() string { return filepath.Join(inst.root, "snapshot.bin") }

func (inst *Store) envelopePath(h t.PatchHash) string {
	hexHash := hex.EncodeToString(h[:])
	return filepath.Join(inst.root, "changes", hexHash[:2], hexHash)
}

// writeAtomic writes data to path via temp file + fsync + rename +
// directory fsync.
func writeAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return eh.Errorf("mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return eh.Errorf("temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return eh.Errorf("write: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return eh.Errorf("fsync: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return eh.Errorf("close: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return eh.Errorf("rename: %w", err)
	}
	err = syncDir(dir)
	return
}

func syncDir(dir string) (err error) {
	d, err := os.Open(dir)
	if err != nil {
		return eh.Errorf("open dir: %w", err)
	}
	serr := d.Sync()
	cerr := d.Close()
	if serr != nil {
		err = eh.Errorf("fsync dir: %w", serr)
		return
	}
	if cerr != nil {
		err = eh.Errorf("close dir: %w", cerr)
	}
	return
}

func (inst *Store) PutEnvelope(ctx context.Context, h t.PatchHash, framed []byte) (err error) {
	path := inst.envelopePath(h)
	if _, serr := os.Stat(path); serr == nil {
		// Content-addressed and immutable: first write wins.
		return
	}
	err = writeAtomic(path, framed)
	return
}

func (inst *Store) GetEnvelope(ctx context.Context, h t.PatchHash) (framed []byte, err error) {
	framed, rerr := os.ReadFile(inst.envelopePath(h))
	if rerr != nil {
		if errors.Is(rerr, fs.ErrNotExist) {
			err = eh.Errorf("%s: %w", h, repo.ErrEnvelopeNotFound)
			return
		}
		err = eh.Errorf("read envelope %s: %w", h, rerr)
	}
	return
}

func (inst *Store) HasEnvelope(ctx context.Context, h t.PatchHash) (ok bool, err error) {
	_, serr := os.Stat(inst.envelopePath(h))
	switch {
	case serr == nil:
		ok = true
	case errors.Is(serr, fs.ErrNotExist):
	default:
		err = eh.Errorf("stat envelope %s: %w", h, serr)
	}
	return
}

func (inst *Store) AppendApplied(ctx context.Context, h t.PatchHash) (err error) {
	f, oerr := os.OpenFile(inst.appliedPath(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if oerr != nil {
		err = eh.Errorf("open applied log: %w", oerr)
		return
	}
	_, werr := f.WriteString(hex.EncodeToString(h[:]) + "\n")
	serr := f.Sync()
	cerr := f.Close()
	if werr != nil {
		err = eh.Errorf("append applied log: %w", werr)
		return
	}
	if serr != nil {
		err = eh.Errorf("fsync applied log: %w", serr)
		return
	}
	if cerr != nil {
		err = eh.Errorf("close applied log: %w", cerr)
	}
	return
}

func (inst *Store) ReplaceApplied(ctx context.Context, hs []t.PatchHash) (err error) {
	var sb strings.Builder
	for _, h := range hs {
		sb.WriteString(hex.EncodeToString(h[:]))
		sb.WriteByte('\n')
	}
	err = writeAtomic(inst.appliedPath(), []byte(sb.String()))
	return
}

func (inst *Store) LoadApplied(ctx context.Context) (hs []t.PatchHash, err error) {
	data, rerr := os.ReadFile(inst.appliedPath())
	if rerr != nil {
		if errors.Is(rerr, fs.ErrNotExist) {
			return // fresh store: empty log
		}
		err = eh.Errorf("read applied log: %w", rerr)
		return
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		var h t.PatchHash
		if uerr := h.UnmarshalText([]byte(line)); uerr != nil {
			if i == len(lines)-1 {
				// Torn trailing append from a crash mid-write: the
				// engine never acknowledged it; drop silently.
				break
			}
			err = eh.Errorf("applied log line %d malformed: %w", i+1, uerr)
			return
		}
		hs = append(hs, h)
	}
	return
}

// Snapshot file framing: "PSNP" | uvarint(count) | count×hash32 |
// graggle bytes (rest of file).
const snapshotMagic = "PSNP"

func (inst *Store) SaveSnapshot(ctx context.Context, snap repo.Snapshot) (err error) {
	buf := make([]byte, 0, 4+10+len(snap.Applied)*32+len(snap.Graggle))
	buf = append(buf, snapshotMagic...)
	buf = binary.AppendUvarint(buf, uint64(len(snap.Applied)))
	for _, h := range snap.Applied {
		buf = append(buf, h[:]...)
	}
	buf = append(buf, snap.Graggle...)
	err = writeAtomic(inst.snapshotPath(), buf)
	return
}

func (inst *Store) LoadSnapshot(ctx context.Context) (snap repo.Snapshot, ok bool, err error) {
	data, rerr := os.ReadFile(inst.snapshotPath())
	if rerr != nil {
		if errors.Is(rerr, fs.ErrNotExist) {
			return
		}
		err = eh.Errorf("read snapshot: %w", rerr)
		return
	}
	if len(data) < len(snapshotMagic) || string(data[:len(snapshotMagic)]) != snapshotMagic {
		err = eh.Errorf("snapshot file magic mismatch")
		return
	}
	rest := data[len(snapshotMagic):]
	n, used := binary.Uvarint(rest)
	if used <= 0 || uint64(len(rest)-used) < n*32 {
		err = eh.Errorf("snapshot file applied-list truncated")
		return
	}
	rest = rest[used:]
	snap.Applied = make([]t.PatchHash, n)
	for i := range snap.Applied {
		copy(snap.Applied[i][:], rest[i*32:(i+1)*32])
	}
	snap.Graggle = rest[n*32:]
	ok = true
	return
}

func (inst *Store) retentionPath() string { return filepath.Join(inst.root, "retention.txt") }

// SaveRetention atomically replaces the retention ledger. Entries are
// written sorted by node id (deterministic, debuggable); an empty slice
// writes an empty file.
func (inst *Store) SaveRetention(ctx context.Context, entries []repo.RetentionEntry) (err error) {
	sorted := make([]repo.RetentionEntry, len(entries))
	copy(sorted, entries)
	slices.SortFunc(sorted, func(a, b repo.RetentionEntry) int { return t.CompareNodeID(a.Node, b.Node) })
	var sb strings.Builder
	for _, e := range sorted {
		sb.WriteString(hex.EncodeToString(e.Node.Patch[:]))
		sb.WriteByte(' ')
		sb.WriteString(strconv.FormatUint(e.Node.Index, 10))
		sb.WriteByte(' ')
		sb.WriteString(strconv.FormatInt(e.UnixNano, 10))
		sb.WriteByte('\n')
	}
	err = writeAtomic(inst.retentionPath(), []byte(sb.String()))
	return
}

// LoadRetention reads the retention ledger. A missing file (fresh store)
// yields an empty slice. The whole file is replaced atomically, so there
// is no torn-tail case: any malformed line is corruption.
func (inst *Store) LoadRetention(ctx context.Context) (entries []repo.RetentionEntry, err error) {
	data, rerr := os.ReadFile(inst.retentionPath())
	if rerr != nil {
		if errors.Is(rerr, fs.ErrNotExist) {
			return
		}
		err = eh.Errorf("read retention ledger: %w", rerr)
		return
	}
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, " ")
		if len(fields) != 3 {
			err = eh.Errorf("retention ledger line %d: want 3 fields, got %d", i+1, len(fields))
			return
		}
		var e repo.RetentionEntry
		if uerr := e.Node.Patch.UnmarshalText([]byte(fields[0])); uerr != nil {
			err = eh.Errorf("retention ledger line %d: %w", i+1, uerr)
			return
		}
		idx, perr := strconv.ParseUint(fields[1], 10, 64)
		if perr != nil {
			err = eh.Errorf("retention ledger line %d index: %w", i+1, perr)
			return
		}
		e.Node.Index = idx
		nanos, perr := strconv.ParseInt(fields[2], 10, 64)
		if perr != nil {
			err = eh.Errorf("retention ledger line %d unixnano: %w", i+1, perr)
			return
		}
		e.UnixNano = nanos
		entries = append(entries, e)
	}
	return
}

func (inst *Store) Close() (err error) {
	err = releaseLock(inst.lockFile)
	inst.lockFile = nil
	return
}
