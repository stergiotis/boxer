// Package filestore is the reference [repo.StorageI] implementation: a
// transparent on-disk layout under one root directory, hardened for
// crash consistency.
//
//	<root>/applied.txt          one 64-hex hash per line, apply order
//	<root>/snapshot.bin         repo.Snapshot (own framing, see below)
//	<root>/changes/<hh>/<hash>  framed envelopes, sharded by first byte
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
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
)

// Store implements repo.StorageI over a root directory.
type Store struct {
	root string
}

var _ repo.StorageI = (*Store)(nil)

// Open creates (if needed) and opens a store rooted at dir.
func Open(dir string) (st *Store, err error) {
	if err = os.MkdirAll(filepath.Join(dir, "changes"), 0o755); err != nil {
		err = eh.Errorf("create store layout: %w", err)
		return
	}
	st = &Store{root: dir}
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

func (inst *Store) Close() (err error) {
	return
}
