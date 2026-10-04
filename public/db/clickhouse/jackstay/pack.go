package jackstay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// A pack (ADR-0271 §SD2) is a directory holding a manifest and one file per
// chunk: the source's Native response body for the chunk, stored as the
// server sent it, compressed or not. [Export] writes it from a server; once
// complete, [OpenPack] reads it as a [SourceI].

// PackFormatVersion is the manifest's format version. [LoadPackManifest]
// refuses any other.
const PackFormatVersion = uint32(1)

// PackManifestName is the manifest's file name inside a pack directory.
const PackManifestName = "manifest.json"

// PackManifest describes a pack. It is rewritten after each exported chunk,
// so it is also the export's journal.
type PackManifest struct {
	FormatVersion uint32 `json:"formatVersion"`
	// ExportId names this export; a restarted export gets a new one.
	ExportId  string    `json:"exportId"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Complete is set once every table's chunks are in the pack. Only a
	// complete pack is read as a source.
	Complete  bool           `json:"complete"`
	Source    Endpoint       `json:"source"`
	Server    ServerInfo     `json:"server"`
	Databases []DatabaseInfo `json:"databases"`
	// Selection, SampleNum/SampleDen and Compression are what the export
	// was asked for; a resumed export must be asked for the same.
	Selection   Selection   `json:"selection"`
	SampleNum   uint32      `json:"sampleNum,omitempty"`
	SampleDen   uint32      `json:"sampleDen,omitempty"`
	Compression string      `json:"compression,omitempty"`
	Tables      []PackTable `json:"tables"`
}

// PackTable is one exported table: the source's description of it, the
// slice the pack holds, and the chunks.
type PackTable struct {
	Source       datacatalog.TableRef `json:"source"`
	Engine       string               `json:"engine"`
	SortingKey   string               `json:"sortingKey"`
	PartitionKey string               `json:"partitionKey"`
	// Bytes is the source table's size on disk, for the target's pre-flight.
	Bytes       uint64       `json:"bytes"`
	CreateQuery string       `json:"createQuery"`
	Columns     []ColumnInfo `json:"columns"`
	// Filter is the effective row filter: the operator's, and the sample
	// predicate when the export sampled. The pack holds that slice only.
	Filter      string      `json:"filter,omitempty"`
	CopyColumns []string    `json:"copyColumns"`
	Chunking    Chunking    `json:"chunking"`
	Chunks      []PackChunk `json:"chunks"`
}

// PackChunk is one chunk file and the digest of the rows it holds.
type PackChunk struct {
	Id      string `json:"id"`
	Pid     string `json:"pid,omitempty"`
	Display string `json:"display,omitempty"`
	// File is the chunk's path relative to the pack directory.
	File     string     `json:"file"`
	Bytes    uint64     `json:"bytes"`
	Sha256   string     `json:"sha256"`
	Encoding string     `json:"encoding,omitempty"`
	Rows     uint64     `json:"rows"`
	Leaves   []PackLeaf `json:"leaves"`
}

// PackLeaf is one leaf's digest (ADR-0259 §SD4).
type PackLeaf struct {
	Leaf uint32 `json:"leaf"`
	N    uint64 `json:"n"`
	Kd   uint64 `json:"kd"`
	Rd   uint64 `json:"rd"`
}

func (inst *PackChunk) leafSet() (ls leafSet) {
	ls = make(leafSet, len(inst.Leaves))
	for _, l := range inst.Leaves {
		ls[l.Leaf] = leafDigest{n: l.N, kd: l.Kd, rd: l.Rd}
	}
	return
}

func packLeaves(ls leafSet) (leaves []PackLeaf) {
	leaves = make([]PackLeaf, 0, len(ls))
	for l, d := range ls {
		if d.n == 0 {
			continue
		}
		leaves = append(leaves, PackLeaf{Leaf: l, N: d.n, Kd: d.kd, Rd: d.rd})
	}
	slices.SortFunc(leaves, func(a, b PackLeaf) int { return int(a.Leaf) - int(b.Leaf) })
	return
}

// Rows is the row count the pack holds of the table.
func (inst *PackTable) Rows() (rows uint64) {
	for i := range inst.Chunks {
		rows += inst.Chunks[i].Rows
	}
	return
}

func (inst *PackTable) chunk(id string) (c *PackChunk) {
	for i := range inst.Chunks {
		if inst.Chunks[i].Id == id {
			return &inst.Chunks[i]
		}
	}
	return nil
}

// Table finds a table of the manifest by its source reference.
func (inst *PackManifest) Table(ref datacatalog.TableRef) (t *PackTable) {
	for i := range inst.Tables {
		if inst.Tables[i].Source == ref {
			return &inst.Tables[i]
		}
	}
	return nil
}

// Marshal is the manifest's bytes.
func (inst *PackManifest) Marshal() (data []byte, err error) {
	data, err = json.Marshal(inst, json.Deterministic(true), jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		err = eh.Errorf("unable to encode pack manifest: %w", err)
		return
	}
	data = append(data, '\n')
	return
}

// SaveIn writes the manifest into the pack directory, atomically.
func (inst *PackManifest) SaveIn(dir string) (err error) {
	var data []byte
	data, err = inst.Marshal()
	if err != nil {
		return
	}
	err = OsFiles{}.WriteFile(filepath.Join(dir, PackManifestName), data)
	if err != nil {
		err = eb.Build().Str("dir", dir).Errorf("unable to write pack manifest: %w", err)
	}
	return
}

// LoadPackManifest reads the manifest of the pack in dir. A missing manifest
// is an error wrapping fs.ErrNotExist.
func LoadPackManifest(dir string) (m PackManifest, err error) {
	var data []byte
	data, err = os.ReadFile(filepath.Join(dir, PackManifestName))
	if err != nil {
		err = eb.Build().Str("dir", dir).Errorf("unable to read pack manifest: %w", err)
		return
	}
	err = json.Unmarshal(data, &m)
	if err != nil {
		err = eb.Build().Str("dir", dir).Errorf("unable to decode pack manifest: %w", err)
		return
	}
	if m.FormatVersion != PackFormatVersion {
		err = eb.Build().Str("dir", dir).Uint64("formatVersion", uint64(m.FormatVersion)).Uint64("supported", uint64(PackFormatVersion)).
			Errorf("unsupported pack format version")
		return
	}
	for i := range m.Tables {
		t := &m.Tables[i]
		if e := t.Chunking.validate(); e != nil {
			err = eb.Build().Str("dir", dir).Str("table", t.Source.String()).Errorf("invalid chunk layout: %w", e)
			return
		}
		for j := range t.Chunks {
			if f := t.Chunks[j].File; f == "" || !filepath.IsLocal(f) {
				err = eb.Build().Str("dir", dir).Str("table", t.Source.String()).Str("file", f).Errorf("chunk file must be a path inside the pack")
				return
			}
		}
	}
	return
}

// Pack is a complete pack read as a source (ADR-0271 §SD3). It holds whole
// chunks as exported, so it serves no row pairs, no FINAL reads and no subset
// of a chunk; every read checks that the plan asks for the pack's own chunk
// layout, copy columns and filter.
type Pack struct {
	dir string
	m   PackManifest
	// verified holds the chunk files whose hash was checked, by path, with
	// the size and modification time they had then.
	verifiedMu sync.Mutex
	verified   map[string]fileStamp
}

type fileStamp struct {
	size    int64
	modTime time.Time
}

var _ SourceI = (*Pack)(nil)

// OpenPack reads the pack in dir; it refuses one whose export is not complete.
func OpenPack(dir string) (p *Pack, err error) {
	var abs string
	abs, err = filepath.Abs(dir)
	if err != nil {
		err = eb.Build().Str("dir", dir).Errorf("unable to resolve pack directory: %w", err)
		return
	}
	var m PackManifest
	m, err = LoadPackManifest(abs)
	if err != nil {
		return
	}
	if !m.Complete {
		err = eb.Build().Str("dir", abs).Errorf("the pack's export is not complete; run the export again to finish it")
		return
	}
	p = &Pack{dir: abs, m: m, verified: make(map[string]fileStamp, 16)}
	return
}

// OpenPackFor opens the pack a plan's source names, and refuses it when it
// holds another export than the one the plan was made from.
func OpenPackFor(ep Endpoint) (p *Pack, err error) {
	p, err = OpenPack(ep.Pack)
	if err != nil {
		return
	}
	if ep.Export != "" && ep.Export != p.m.ExportId {
		err = eb.Build().Str("dir", p.dir).Str("plan", ep.Export).Str("pack", p.m.ExportId).
			Errorf("the pack was exported again since the plan was made; re-run the structure step")
		p = nil
	}
	return
}

// Endpoint is the plan source naming this pack and its export.
func (inst *Pack) Endpoint() (ep Endpoint) {
	return Endpoint{Pack: inst.dir, Export: inst.m.ExportId, User: inst.m.Source.User}
}

// Manifest is the pack's manifest; the caller must not modify it.
func (inst *Pack) Manifest() (m *PackManifest) {
	return &inst.m
}

func (inst *Pack) discover(ctx context.Context) (inv Inventory, err error) {
	inv = Inventory{Server: inst.m.Server, Databases: slices.Clone(inst.m.Databases)}
	inv.Tables = make([]TableInfo, 0, len(inst.m.Tables))
	for i := range inst.m.Tables {
		t := &inst.m.Tables[i]
		inv.Tables = append(inv.Tables, TableInfo{
			Ref:          t.Source,
			Engine:       t.Engine,
			SortingKey:   t.SortingKey,
			PartitionKey: t.PartitionKey,
			TotalRows:    t.Rows(),
			TotalBytes:   t.Bytes,
			CreateQuery:  t.CreateQuery,
			Columns:      slices.Clone(t.Columns),
			Filter:       t.Filter,
		})
	}
	return
}

// table finds the spec's table and checks the spec asks for what the pack
// holds: its layout, columns and slice, read whole or by chunk.
func (inst *Pack) table(spec *DigestSpec) (t *PackTable, err error) {
	t = inst.m.Table(spec.Ref)
	switch {
	case t == nil:
		err = eb.Build().Str("table", spec.Ref.String()).Errorf("the pack holds no such table")
	case spec.Final:
		err = eb.Build().Str("table", spec.Ref.String()).Errorf("a pack cannot be read with FINAL")
	case spec.sel.opaque || spec.sel.sampled || spec.sel.leaves != nil:
		err = eb.Build().Str("table", spec.Ref.String()).Errorf("a pack holds whole chunks only")
	case spec.Filter != t.Filter:
		err = eb.Build().Str("table", spec.Ref.String()).Str("filter", spec.Filter).Str("exported", t.Filter).
			Errorf("the plan's filter is not the one the pack was exported under")
	case !slices.Equal(spec.CopyColumns, t.CopyColumns):
		err = eb.Build().Str("table", spec.Ref.String()).Errorf("the plan copies other columns than the pack holds; re-run the structure step")
	case !chunkingEqual(&spec.Chunking, &t.Chunking):
		err = eb.Build().Str("table", spec.Ref.String()).Errorf("the plan's chunk layout is not the pack's; re-run the structure step")
	}
	if err != nil {
		t = nil
	}
	return
}

func chunkingEqual(a *Chunking, b *Chunking) (eq bool) {
	return a.Kind == b.Kind && a.BoundType == b.BoundType && a.Leaves == b.Leaves &&
		slices.Equal(a.Exprs, b.Exprs) && slices.Equal(a.Bounds, b.Bounds)
}

// chunks are the spec's chunks: all of the table's, or the one it selects.
func (inst *Pack) chunks(spec *DigestSpec) (t *PackTable, chunks []*PackChunk, err error) {
	t, err = inst.table(spec)
	if err != nil {
		return
	}
	if spec.sel.chunked {
		if c := t.chunk(spec.sel.chunk); c != nil {
			chunks = []*PackChunk{c}
		}
		return
	}
	chunks = make([]*PackChunk, 0, len(t.Chunks))
	for i := range t.Chunks {
		chunks = append(chunks, &t.Chunks[i])
	}
	return
}

func (inst *Pack) digests(ctx context.Context, spec *DigestSpec) (out map[string]*chunkDigests, err error) {
	var chunks []*PackChunk
	_, chunks, err = inst.chunks(spec)
	if err != nil {
		return
	}
	out = make(map[string]*chunkDigests, len(chunks))
	for _, c := range chunks {
		if c.Rows == 0 {
			continue
		}
		cd := &chunkDigests{display: c.Display, pid: c.Pid, rows: c.Rows, leaves: make(map[uint32]leafDigest, len(c.Leaves))}
		for _, l := range c.Leaves {
			cd.leaves[l.Leaf] = leafDigest{n: l.N, kd: l.Kd, rd: l.Rd}
		}
		out[c.Id] = cd
	}
	return
}

func (inst *Pack) pairs(ctx context.Context, spec *DigestSpec, leaves []ChunkLeaf) (pairs map[pairKey]*pairSide, err error) {
	err = eb.Build().Str("table", spec.Ref.String()).Errorf("a pack holds no row pairs")
	return
}

func (inst *Pack) chunkList(ctx context.Context, spec *DigestSpec) (rows []chunkListRow, err error) {
	var chunks []*PackChunk
	_, chunks, err = inst.chunks(spec)
	if err != nil {
		return
	}
	rows = make([]chunkListRow, 0, len(chunks))
	for _, c := range chunks {
		if c.Rows == 0 {
			continue
		}
		rows = append(rows, chunkListRow{Chunk: c.Id, Pid: c.Pid, Display: c.Display})
	}
	return
}

// stream opens one chunk's file in the encoding it was stored in, after
// checking it against the manifest's SHA-256, so a file damaged in transit
// fails before a row of it lands.
func (inst *Pack) stream(ctx context.Context, spec *DigestSpec, compression string) (body io.ReadCloser, encoding string, err error) {
	var chunks []*PackChunk
	_, chunks, err = inst.chunks(spec)
	if err != nil {
		return
	}
	if !spec.sel.chunked || len(chunks) != 1 {
		err = eb.Build().Str("table", spec.Ref.String()).Errorf("a pack streams one chunk at a time")
		return
	}
	c := chunks[0]
	path := filepath.Join(inst.dir, c.File)
	err = inst.checkOnce(path, c)
	if err != nil {
		return
	}
	var f *os.File
	f, err = os.Open(path)
	if err != nil {
		err = eb.Build().Str("file", path).Errorf("unable to open chunk file: %w", err)
		return
	}
	body, encoding = f, c.Encoding
	return
}

// precheck verifies the chunk's file, so a damaged one is found before a sync
// clears the target chunk it would replace.
func (inst *Pack) precheck(ctx context.Context, spec *DigestSpec) (err error) {
	var chunks []*PackChunk
	_, chunks, err = inst.chunks(spec)
	if err != nil || !spec.sel.chunked || len(chunks) != 1 {
		return
	}
	return inst.checkOnce(filepath.Join(inst.dir, chunks[0].File), chunks[0])
}

// checkOnce hashes a chunk file unless this pack already did and the file
// has kept its size and modification time since.
func (inst *Pack) checkOnce(path string, c *PackChunk) (err error) {
	fi, err := os.Stat(path)
	if err != nil {
		err = eb.Build().Str("file", path).Errorf("unable to open chunk file: %w", err)
		return
	}
	stamp := fileStamp{size: fi.Size(), modTime: fi.ModTime()}
	inst.verifiedMu.Lock()
	seen, has := inst.verified[path]
	inst.verifiedMu.Unlock()
	if has && seen == stamp {
		return
	}
	err = checkChunkFile(path, c)
	if err != nil {
		return
	}
	inst.verifiedMu.Lock()
	inst.verified[path] = stamp
	inst.verifiedMu.Unlock()
	return
}

func checkChunkFile(path string, c *PackChunk) (err error) {
	var f *os.File
	f, err = os.Open(path)
	if err != nil {
		err = eb.Build().Str("file", path).Errorf("unable to open chunk file: %w", err)
		return
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	var n int64
	n, err = io.Copy(h, f)
	if err != nil {
		err = eb.Build().Str("file", path).Errorf("unable to read chunk file: %w", err)
		return
	}
	if uint64(n) != c.Bytes || hex.EncodeToString(h.Sum(nil)) != c.Sha256 {
		err = eb.Build().Str("file", path).Uint64("bytes", uint64(n)).Uint64("expected", c.Bytes).
			Errorf("chunk file does not match the manifest; the pack is damaged")
	}
	return
}

func (inst *Pack) deriveChunking(ctx context.Context, pt *PlanTable, opts ChunkingOptions) (c Chunking, err error) {
	t := inst.m.Table(pt.Source)
	if t == nil {
		err = eb.Build().Str("table", pt.Source.String()).Errorf("the pack holds no such table")
		return
	}
	c = t.Chunking
	c.Exprs = slices.Clone(t.Chunking.Exprs)
	c.Bounds = slices.Clone(t.Chunking.Bounds)
	return
}

func (inst *Pack) limits() (l sourceLimits) {
	return sourceLimits{noPairs: true, noFinal: true, noSubsets: true, what: "a pack"}
}
