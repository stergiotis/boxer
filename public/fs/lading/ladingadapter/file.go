package ladingadapter

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// content modes, as the walker stores them.
const (
	contentNone   = "none"
	contentBlocks = "blocks"
	contentRef    = "ref"
)

// info is one entry as an [fs.FileInfo].
type info struct {
	name string
	row  entryRow
}

// entryRow is the subset of a decoded entry an FileInfo needs, so the info
// type does not carry the whole row into every DirEntry.
type entryRow struct {
	mode  fs.FileMode
	size  int64
	mtime time.Time
}

func infoOf(e *entry) fs.FileInfo {
	return info{
		name: path.Base(e.name),
		row: entryRow{
			mode:  modeOf(e),
			size:  int64(e.row.Size),
			mtime: e.row.Mtime,
		},
	}
}

func (inst info) Name() string       { return inst.name }
func (inst info) Size() int64        { return inst.row.size }
func (inst info) Mode() fs.FileMode  { return inst.row.mode }
func (inst info) ModTime() time.Time { return inst.row.mtime }
func (inst info) IsDir() bool        { return inst.row.mode.IsDir() }
func (inst info) Sys() any           { return nil }

// modeOf is the stored mode as fs.FileMode. The walker stores Lstat's bits
// verbatim, type bits included, so nothing has to be reconstructed here.
func modeOf(e *entry) fs.FileMode { return fs.FileMode(e.row.Mode) }

// File is an open handle on one entry of a snapshot.
//
// It satisfies [io.ReaderAt] and [io.Seeker] as well as [fs.File], and a
// directory handle satisfies [fs.ReadDirFile].
//
// # When the bytes are fetched
//
// Lazily, and how depends on how the file was cut. A file cut at fixed offsets
// — the non-text case — can map a byte offset to a block ordinal by
// arithmetic, so [File.ReadAt] fetches exactly the blocks a range touches and
// nothing else. A file cut at newlines cannot: its block boundaries are where
// the newlines were, so an offset says nothing about an ordinal. That file is
// materialised whole on first read.
//
// Materialising is bounded rather than open-ended: a file only has blocks at
// all if it was under the mount's inline threshold, so the threshold is the
// bound. It is the same number that bounds the walker's memory per file.
type File struct {
	fsys *FS
	// name is the name the caller opened, which is what an error must say —
	// not the path the entry resolved to.
	name string
	e    *entry

	off int64

	// blocks caches what has been fetched, by ordinal. A materialised file
	// lands here as one entry per block too, so the two paths share it.
	blocks map[uint32][]byte
	// whole is the file's content once a newline-cut file has been
	// materialised — the case an offset cannot address by arithmetic.
	whole []byte

	// dir is a directory handle's listing, read on the first ReadDir.
	dir    []fs.DirEntry
	dirOff int

	closed bool
}

var (
	_ fs.File        = (*File)(nil)
	_ fs.ReadDirFile = (*File)(nil)
	_ io.ReaderAt    = (*File)(nil)
	_ io.Seeker      = (*File)(nil)
)

func (inst *FS) openEntry(name string, e *entry) (fs.File, error) {
	return &File{fsys: inst, name: name, e: e, blocks: map[uint32][]byte{}}, nil
}

// Stat describes the open file.
func (inst *File) Stat() (fs.FileInfo, error) {
	if inst.closed {
		return nil, pathErr("stat", inst.name, fs.ErrClosed)
	}
	return infoOf(inst.e), nil
}

// Close releases the handle. The snapshot is immutable, so there is nothing to
// release but the handle's own caches; a second Close is an error, as it is
// for every other fs.File.
func (inst *File) Close() error {
	if inst.closed {
		return pathErr("close", inst.name, fs.ErrClosed)
	}
	inst.closed = true
	inst.whole, inst.blocks, inst.dir = nil, nil, nil
	return nil
}

// Read reads from the current offset.
func (inst *File) Read(p []byte) (n int, err error) {
	if inst.closed {
		return 0, pathErr("read", inst.name, fs.ErrClosed)
	}
	n, err = inst.ReadAt(p, inst.off)
	inst.off += int64(n)
	if errors.Is(err, io.EOF) && n > 0 {
		err = nil
	}
	return
}

// ReadAt reads a byte range, without moving the offset.
//
// A range that crosses block boundaries is one query for the blocks it
// touches, not one per block and not one per chunk.
func (inst *File) ReadAt(p []byte, off int64) (n int, err error) {
	if inst.closed {
		return 0, pathErr("readat", inst.name, fs.ErrClosed)
	}
	if off < 0 {
		return 0, pathErr("readat", inst.name, fs.ErrInvalid)
	}
	if len(p) == 0 {
		return 0, nil
	}
	if inst.e.row.Mode&uint32(fs.ModeDir) != 0 {
		return 0, pathErr("readat", inst.name, errors.New("is a directory"))
	}

	size, err := inst.size()
	if err != nil {
		return 0, pathErr("readat", inst.name, err)
	}
	if off >= size {
		return 0, io.EOF
	}
	end := min(off+int64(len(p)), size)

	if inst.canSeekBlocks() {
		n, err = inst.readFixed(p, off, end)
	} else {
		if inst.whole == nil {
			inst.whole, err = inst.fsys.content(inst.e)
			if err != nil {
				return 0, pathErr("readat", inst.name, err)
			}
		}
		if off >= int64(len(inst.whole)) {
			return 0, io.EOF
		}
		n = copy(p, inst.whole[off:min(end, int64(len(inst.whole)))])
	}
	if err != nil {
		return n, pathErr("readat", inst.name, err)
	}
	if int64(n) < int64(len(p)) {
		err = io.EOF
	}
	return
}

// Seek moves the read offset. Seeking past the end is legal, as it is on a
// real file; the next Read reports EOF.
func (inst *File) Seek(offset int64, whence int) (int64, error) {
	if inst.closed {
		return 0, pathErr("seek", inst.name, fs.ErrClosed)
	}
	size, err := inst.size()
	if err != nil {
		return 0, pathErr("seek", inst.name, err)
	}
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = inst.off + offset
	case io.SeekEnd:
		next = size + offset
	default:
		return 0, pathErr("seek", inst.name, fs.ErrInvalid)
	}
	if next < 0 {
		return 0, pathErr("seek", inst.name, fs.ErrInvalid)
	}
	inst.off = next
	return next, nil
}

// ReadDir serves a directory handle, in the paged form [fs.ReadDirFile]
// specifies: n <= 0 returns everything, n > 0 returns at most n and reports
// io.EOF once exhausted.
func (inst *File) ReadDir(n int) ([]fs.DirEntry, error) {
	if inst.closed {
		return nil, pathErr("readdir", inst.name, fs.ErrClosed)
	}
	if !modeOf(inst.e).IsDir() {
		return nil, pathErr("readdir", inst.name, errors.New("not a directory"))
	}
	if inst.dir == nil {
		kids, err := inst.fsys.children(inst.e.name)
		if err != nil {
			return nil, pathErr("readdir", inst.name, err)
		}
		inst.dir = make([]fs.DirEntry, 0, len(kids))
		for _, k := range kids {
			inst.dir = append(inst.dir, fs.FileInfoToDirEntry(infoOf(k)))
		}
	}
	rest := inst.dir[inst.dirOff:]
	if n <= 0 {
		inst.dirOff = len(inst.dir)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	rest = rest[:min(n, len(rest))]
	inst.dirOff += len(rest)
	return rest, nil
}

// size is the file's length as the entry records it, restated from the stored
// content where the two could differ.
func (inst *File) size() (int64, error) {
	return int64(inst.e.row.Size), nil
}

// canSeekBlocks reports whether a byte offset maps to a block ordinal by
// arithmetic — true exactly when the file was cut at fixed offsets.
//
// The text case is the exception and it is not an accident: a text file's
// blocks end where its newlines were, which is what makes a line-oriented
// query over them exact, and is exactly what makes their lengths unknown
// without reading them.
func (inst *File) canSeekBlocks() bool {
	return inst.e.row.Content == contentBlocks &&
		!inst.e.row.Text &&
		inst.e.row.BlockSize > 0
}

// readFixed serves a range from a fixed-cut file, fetching only the blocks it
// touches.
func (inst *File) readFixed(p []byte, off, end int64) (n int, err error) {
	bs := int64(inst.e.row.BlockSize)
	first, last := uint32(off/bs), uint32((end-1)/bs)

	err = inst.fetchBlocks(first, last)
	if err != nil {
		return 0, err
	}
	for seq := first; seq <= last && int64(n) < end-off; seq++ {
		b, ok := inst.blocks[seq]
		if !ok {
			return n, eb.Build().Uint32("seq", seq).Uint32("blocks", inst.e.row.Blocks).
				Errorf("lading: a block of %s is missing from the snapshot", inst.name)
		}
		start := int64(seq) * bs
		lo := max(off-start, 0)
		hi := min(end-start, int64(len(b)))
		if lo >= hi {
			continue
		}
		n += copy(p[n:], b[lo:hi])
	}
	return n, nil
}

// fetchBlocks reads an ordinal range in one query, skipping what is cached.
func (inst *File) fetchBlocks(first, last uint32) error {
	missing := false
	for seq := first; seq <= last; seq++ {
		if _, ok := inst.blocks[seq]; !ok {
			missing = true
			break
		}
	}
	if !missing {
		return nil
	}
	rows, err := inst.fsys.blocks(inst.e.name, first, last)
	if err != nil {
		return err
	}
	for seq, data := range rows {
		inst.blocks[seq] = data
	}
	return nil
}

// content materialises one entry's whole content.
func (inst *FS) content(e *entry) ([]byte, error) {
	switch e.row.Content {
	case contentNone, "":
		return nil, ErrNoContent
	case contentRef:
		if inst.fetcher == nil {
			return nil, ErrReferenced
		}
		return inst.fetcher.FetchContent(inst.ctx, inst.mount, inst.snap, e.name, e.row.ContentHash)
	}
	rows, err := inst.blocks(e.name, 0, ^uint32(0))
	if err != nil {
		return nil, err
	}
	seqs := make([]uint32, 0, len(rows))
	for seq := range rows {
		seqs = append(seqs, seq)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	out := make([]byte, 0, e.row.Size)
	for i, seq := range seqs {
		if uint32(i) != seq {
			return nil, eb.Build().Int("seq", i).Str("path", e.name).
				Errorf("lading: a block of %s is missing from the snapshot", e.name)
		}
		out = append(out, rows[seq]...)
	}
	return out, nil
}

// blocks reads one file's blocks in an ordinal range, keyed by ordinal.
//
// The predicate is a key range, not a scan: a file's blocks sit under
// `path ‖ 0x00 ‖ be32(seq)`, so a prefix selects exactly that file's — "a.txt"
// cannot reach "a.txt.bak", because 0x00 sorts below every byte a path may
// carry — and the ordinal bounds narrow it further, big-endian so the byte
// order is the numeric order.
func (inst *FS) blocks(name string, first, last uint32) (map[uint32][]byte, error) {
	if inst.st.Data == nil {
		return nil, errors.New("lading: no block store bound; this view can stat but not read")
	}
	pred := fmt.Sprintf("%s = %d AND %s = %s AND %s BETWEEN %s AND %s",
		inst.col("id"), inst.mount.Value(),
		inst.col("ts"), tsLiteral(inst.snap),
		inst.col("naturalKey"), quote(blockKey(name, first)), quote(blockKey(name, last)))

	out := map[uint32][]byte{}
	for ent, err := range inst.st.Data.ScanLadingBlock(inst.ctx, recordstore.ScanOpts{ExtraPredicate: pred}) {
		if err != nil {
			return nil, err
		}
		if !ent.LadingBlock.Has {
			continue
		}
		seq, ok := ordinalOf(ent.NaturalKey, name)
		if !ok {
			continue
		}
		out[seq] = ent.LadingBlock.Val.Data
	}
	return out, nil
}

// blockKey is the walker's encoding, restated for the read side: the path, a
// NUL, and the ordinal big-endian.
func blockKey(name string, seq uint32) string {
	return name + "\x00" +
		string([]byte{byte(seq >> 24), byte(seq >> 16), byte(seq >> 8), byte(seq)})
}

// ordinalOf recovers a block's ordinal from its natural key, and checks the
// key really is this file's rather than a neighbour whose name shares a
// prefix.
func ordinalOf(key []byte, name string) (seq uint32, ok bool) {
	if len(key) != len(name)+5 || string(key[:len(name)]) != name || key[len(name)] != 0 {
		return 0, false
	}
	tail := key[len(name)+1:]
	return uint32(tail[0])<<24 | uint32(tail[1])<<16 | uint32(tail[2])<<8 | uint32(tail[3]), true
}
