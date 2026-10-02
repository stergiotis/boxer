package jackstay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ExportRequest is what an export is asked for (ADR-0271 §SD2).
type ExportRequest struct {
	// Selection chooses the tables and their row filters, as on the
	// Structure step; DatabaseMap is ignored, since a pack keeps the
	// source's names and the structure step from the pack maps them.
	Selection Selection
	// SampleNum of every SampleDen keys are exported; a zero SampleDen
	// exports every row of the slice.
	SampleNum, SampleDen uint32
	// Compression is the HTTP content encoding the chunks are asked for and
	// stored in ("zstd", "gzip"; empty for none).
	Compression string
	// Restart begins a new export in place of resuming the pack's.
	Restart  bool
	Chunking ChunkingOptions
	// MaxAttempts bounds the reads of a chunk whose source moved while it
	// was read.
	MaxAttempts int
	// Progress, when set, is called after each chunk; BeforeTable before
	// each table.
	Progress    func(r ChunkResult)
	BeforeTable func(t *PackTable)
	// Bytes, when set, is advanced as bytes are written.
	Bytes *atomic.Int64
}

// ExportOutcome is what an export left behind.
type ExportOutcome struct {
	Manifest PackManifest
	// Resumed reports that the pack's earlier export was continued.
	Resumed bool
	// Skipped are the selected tables a pack cannot hold, with the reason.
	Skipped []string
	// Failed counts the chunks not exported; the pack is complete only when
	// it is zero.
	Failed int
}

// Export writes the selected tables of the source into the pack directory
// dir, chunk by chunk (ADR-0271 §SD2). A chunk's file is the source's Native
// response body as sent; it is renamed into place, and the manifest
// rewritten, only when the source chunk's digest read before and after the
// stream agree. Running the export again with the same request resumes: a
// recorded chunk whose source digest has not moved is kept.
func Export(ctx context.Context, src ClientI, srcEp Endpoint, dir string, req ExportRequest, now func() time.Time) (out ExportOutcome, err error) {
	if req.SampleDen != 0 && (req.SampleNum == 0 || req.SampleNum > req.SampleDen) {
		err = eh.Errorf("invalid sample fraction")
		return
	}
	if req.MaxAttempts < 1 {
		req.MaxAttempts = 3
	}
	err = os.MkdirAll(dir, 0o755)
	if err != nil {
		err = eb.Build().Str("dir", dir).Errorf("unable to create pack directory: %w", err)
		return
	}
	var inv Inventory
	inv, err = Discover(ctx, src)
	if err != nil {
		err = eh.Errorf("unable to discover the source: %w", err)
		return
	}
	sel := req.Selection
	sel.DatabaseMap = nil
	var plan Plan
	plan, err = exportPlan(srcEp, &inv, sel, now())
	if err != nil {
		return
	}

	m, resumed, err := beginExport(dir, srcEp, &inv, &plan, req, now())
	if err != nil {
		return
	}
	out.Resumed = resumed
	ex := &exporter{src: src, dir: dir, req: &req, now: now, m: &m}

	keep := make([]PackTable, 0, len(plan.Tables))
	for i := range plan.Tables {
		pt := &plan.Tables[i]
		if !pt.Verdict.IsSyncable() {
			out.Skipped = append(out.Skipped, notDiffable(pt))
			continue
		}
		var t PackTable
		t, err = ex.tableFor(ctx, pt, &inv)
		if err != nil {
			return
		}
		keep = append(keep, t)
	}
	// Tables no longer selected or on the source leave the pack.
	for i := range m.Tables {
		if !slices.ContainsFunc(keep, func(t PackTable) bool { return t.Source == m.Tables[i].Source }) {
			ex.removeFiles(m.Tables[i].Chunks)
		}
	}
	m.Tables = keep
	m.Complete = false
	if err = ex.save(); err != nil {
		return
	}
	for i := range m.Tables {
		t := &m.Tables[i]
		if req.BeforeTable != nil {
			req.BeforeTable(t)
		}
		var failed int
		failed, err = ex.exportTable(ctx, t)
		out.Failed += failed
		if err != nil {
			return
		}
	}
	m.Complete = out.Failed == 0
	err = ex.save()
	out.Manifest = m
	return
}

// exportPlan judges the source's tables as if the target were empty: every
// table a sync could create is one an export can hold, with the copy column
// list a sync from the pack inserts by.
func exportPlan(srcEp Endpoint, inv *Inventory, sel Selection, now time.Time) (plan Plan, err error) {
	ops, err := newTableOps()
	if err != nil {
		return
	}
	plan, err = BuildPlan(ops, srcEp, Endpoint{Pack: "export"}, inv, &Inventory{}, sel, now)
	return
}

// beginExport loads the manifest an earlier export left, when it was asked for
// the same, or begins a new one.
func beginExport(dir string, srcEp Endpoint, inv *Inventory, plan *Plan, req ExportRequest, now time.Time) (m PackManifest, resumed bool, err error) {
	old, lerr := LoadPackManifest(dir)
	switch {
	case lerr == nil && !req.Restart:
		why := ""
		switch {
		case old.Source.URL != srcEp.URL || old.Server.UUID != inv.Server.UUID:
			why = "another source server"
		case !sameSelection(old.Selection, plan.Selection):
			why = "another selection or filter"
		case old.SampleNum != req.SampleNum || old.SampleDen != req.SampleDen:
			why = "another sample"
		case old.Compression != req.Compression:
			why = "another compression"
		}
		if why != "" {
			err = eb.Build().Str("dir", dir).Errorf("the pack holds an export of %s; ask for the same, or restart the export", why)
			return
		}
		m, resumed = old, true
		m.Server, m.Databases = inv.Server, exportedDatabases(inv, plan)
		return
	case lerr == nil:
		// A restart removes the earlier export's files.
		ex := &exporter{dir: dir}
		for i := range old.Tables {
			ex.removeFiles(old.Tables[i].Chunks)
		}
	case !errors.Is(lerr, fs.ErrNotExist):
		err = lerr
		return
	}
	var id string
	id, err = gonanoid.New()
	if err != nil {
		err = eh.Errorf("unable to mint an export id: %w", err)
		return
	}
	m = PackManifest{
		FormatVersion: PackFormatVersion,
		ExportId:      id,
		CreatedAt:     now.UTC(),
		Source:        srcEp,
		Server:        inv.Server,
		Databases:     exportedDatabases(inv, plan),
		Selection:     plan.Selection,
		SampleNum:     req.SampleNum,
		SampleDen:     req.SampleDen,
		Compression:   req.Compression,
	}
	return
}

func sameSelection(a Selection, b Selection) (same bool) {
	return slices.Equal(a.Databases, b.Databases) && a.LeewayOnly == b.LeewayOnly && maps.Equal(a.Filters, b.Filters)
}

func exportedDatabases(inv *Inventory, plan *Plan) (dbs []DatabaseInfo) {
	for _, db := range inv.Databases {
		if slices.Contains(plan.Selection.Databases, db.Name) {
			dbs = append(dbs, db)
		}
	}
	return
}

type exporter struct {
	src ClientI
	dir string
	req *ExportRequest
	now func() time.Time
	m   *PackManifest
}

func (inst *exporter) save() (err error) {
	inst.m.UpdatedAt = inst.now().UTC()
	return inst.m.SaveIn(inst.dir)
}

func (inst *exporter) removeFiles(chunks []PackChunk) {
	for _, c := range chunks {
		if filepath.IsLocal(c.File) {
			_ = os.Remove(filepath.Join(inst.dir, c.File))
		}
	}
}

// tableFor is the manifest entry of a plan table: the earlier export's, when
// it describes the same table under the same slice, so its chunks can be
// kept; a new one otherwise.
func (inst *exporter) tableFor(ctx context.Context, pt *PlanTable, inv *Inventory) (t PackTable, err error) {
	st, _ := inv.Table(pt.Source)
	filter := pt.Filter
	if inst.req.SampleDen != 0 {
		spec, _, _ := pt.DigestSpecs(false)
		filter = andPredicates(pt.Filter, spec.SamplePredicate(inst.req.SampleNum, inst.req.SampleDen))
	}
	t = PackTable{
		Source:       pt.Source,
		Engine:       pt.Engine,
		SortingKey:   pt.SortingKey,
		PartitionKey: pt.PartitionKey,
		Bytes:        pt.Bytes,
		CreateQuery:  st.CreateQuery,
		Columns:      slices.Clone(st.Columns),
		Filter:       filter,
		CopyColumns:  slices.Clone(pt.CopyColumns),
	}
	if old := inst.m.Table(pt.Source); old != nil {
		if old.SortingKey == t.SortingKey && old.PartitionKey == t.PartitionKey && old.Filter == t.Filter && slices.Equal(old.CopyColumns, t.CopyColumns) {
			t.Chunking, t.Chunks = old.Chunking, old.Chunks
			return
		}
		inst.removeFiles(old.Chunks)
	}
	t.Chunking, err = DeriveChunking(ctx, inst.src, pt.Source, pt.SortingKey, pt.PartitionKey, pt.Rows, inst.req.Chunking)
	if err != nil {
		err = eb.Build().Str("table", pt.Source.String()).Errorf("unable to derive the chunk layout: %w", err)
	}
	return
}

func (inst *PackTable) spec() (spec DigestSpec) {
	return DigestSpec{Ref: inst.Source, KeyExprs: SplitKeyExprs(inst.SortingKey), CopyColumns: inst.CopyColumns, Chunking: inst.Chunking, Filter: inst.Filter}
}

// exportTable brings the table's chunks in the pack in line with the source:
// chunks the source lacks are removed, the others exported unless the pack
// already holds them at the source's digest.
func (inst *exporter) exportTable(ctx context.Context, t *PackTable) (failed int, err error) {
	spec := t.spec()
	var list []chunkListRow
	list, err = queryRows[chunkListRow](ctx, inst.src, spec.ChunkListQuery())
	if err != nil {
		err = eb.Build().Str("table", t.Source.String()).Errorf("unable to list source chunks: %w", err)
		return
	}
	var gone []PackChunk
	t.Chunks = slices.DeleteFunc(t.Chunks, func(c PackChunk) bool {
		if slices.ContainsFunc(list, func(r chunkListRow) bool { return r.Chunk == c.Id }) {
			return false
		}
		gone = append(gone, c)
		return true
	})
	inst.removeFiles(gone)
	t.Done = false
	for _, c := range list {
		if err = ctx.Err(); err != nil {
			return
		}
		r := inst.exportChunk(ctx, t, &spec, c)
		if r.Status == ChunkStatusFailed {
			failed++
		}
		if inst.req.Progress != nil {
			inst.req.Progress(r)
		}
	}
	slices.SortFunc(t.Chunks, func(a, b PackChunk) int { return compareChunkIds(a.Id, b.Id) })
	t.Done = failed == 0
	err = inst.save()
	return
}

func (inst *exporter) exportChunk(ctx context.Context, t *PackTable, spec *DigestSpec, c chunkListRow) (r ChunkResult) {
	r = ChunkResult{Table: t.Source.String(), Chunk: c.Chunk, Display: c.Display}
	if r.Display == "" {
		r.Display = t.Chunking.RangeDisplay(c.Chunk)
	}
	cs := spec.ForChunk(c.Chunk, c.Pid)
	before, err := readLeafSet(ctx, inst.src, &cs)
	if err != nil {
		r.Status, r.Note = ChunkStatusFailed, err.Error()
		return
	}
	if old := t.chunk(c.Chunk); old != nil && old.leafSet().equal(before) {
		if e := checkChunkSize(filepath.Join(inst.dir, old.File), old.Bytes); e == nil {
			r.Status, r.Rows = ChunkStatusDone, old.Rows
			return
		}
	}
	file := filepath.Join(packFileStem(t.Source.String(), c.Chunk) + packFileExt(inst.req.Compression))
	for attempt := 1; attempt <= inst.req.MaxAttempts; attempt++ {
		r.Attempts = attempt
		var pc PackChunk
		pc, err = inst.writeChunk(ctx, &cs, file)
		if err != nil {
			r.Status, r.Note = ChunkStatusFailed, err.Error()
			return
		}
		r.Bytes += pc.Bytes
		var after leafSet
		after, err = readLeafSet(ctx, inst.src, &cs)
		if err != nil {
			_ = os.Remove(filepath.Join(inst.dir, file) + ".partial")
			r.Status, r.Note = ChunkStatusFailed, err.Error()
			return
		}
		if !after.equal(before) {
			// The source moved while the chunk was read; the file may
			// hold either state, so it is read again.
			_ = os.Remove(filepath.Join(inst.dir, file) + ".partial")
			r.Note = "the source chunk changed while it was read"
			before = after
			continue
		}
		path := filepath.Join(inst.dir, file)
		if err = os.Rename(path+".partial", path); err != nil {
			r.Status, r.Note = ChunkStatusFailed, "unable to move the chunk file into place: "+err.Error()
			return
		}
		pc.Id, pc.Pid, pc.Display, pc.File = c.Chunk, c.Pid, c.Display, file
		pc.Rows, pc.Leaves = before.total().n, packLeaves(before)
		if old := t.chunk(c.Chunk); old != nil {
			if old.File != file {
				inst.removeFiles([]PackChunk{*old})
			}
			*old = pc
		} else {
			t.Chunks = append(t.Chunks, pc)
		}
		if err = inst.save(); err != nil {
			r.Status, r.Note = ChunkStatusFailed, err.Error()
			return
		}
		r.Status, r.Rows, r.Note = ChunkStatusCopied, pc.Rows, ""
		return
	}
	r.Status = ChunkStatusFailed
	return
}

// writeChunk streams the chunk's rows into file.partial, synced, and returns
// its size, hash and encoding.
func (inst *exporter) writeChunk(ctx context.Context, spec *DigestSpec, file string) (pc PackChunk, err error) {
	path := filepath.Join(inst.dir, file) + ".partial"
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		err = eb.Build().Str("file", path).Errorf("unable to create the chunk's directory: %w", err)
		return
	}
	body, encoding, err := ServerSource(inst.src).stream(ctx, spec, inst.req.Compression)
	if err != nil {
		err = eh.Errorf("unable to read source rows: %w", err)
		return
	}
	defer func() { _ = body.Close() }()
	var f *os.File
	f, err = os.Create(path)
	if err != nil {
		err = eb.Build().Str("file", path).Errorf("unable to create chunk file: %w", err)
		return
	}
	h := sha256.New()
	cr := &countingReader{r: body, total: inst.req.Bytes}
	_, err = io.Copy(io.MultiWriter(f, h), cr)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		err = eb.Build().Str("file", path).Errorf("unable to write chunk file: %w", err)
		return
	}
	pc = PackChunk{Bytes: uint64(cr.n), Sha256: hex.EncodeToString(h.Sum(nil)), Encoding: encoding}
	return
}

func checkChunkSize(path string, size uint64) (err error) {
	var fi os.FileInfo
	fi, err = os.Stat(path)
	if err == nil && uint64(fi.Size()) != size {
		err = eb.Build().Str("file", path).Errorf("chunk file has another size than recorded")
	}
	return
}

// packFileStem names a chunk's file by hashes of the table and the chunk id,
// so any table name and chunk id make a portable path.
func packFileStem(table string, chunk string) (stem string) {
	th := sha256.Sum256([]byte(table))
	ch := sha256.Sum256([]byte(chunk))
	return filepath.Join("chunks", hex.EncodeToString(th[:8]), hex.EncodeToString(ch[:12]))
}

func packFileExt(encoding string) (ext string) {
	switch encoding {
	case "zstd":
		return ".native.zst"
	case "gzip":
		return ".native.gz"
	}
	return ".native"
}
