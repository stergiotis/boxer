package mdnotebook

import (
	"encoding/hex"
	"io/fs"
	"path"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RecordExt is the extension of a record's blob: an Arrow IPC stream.
const RecordExt = ".arrow"

// BlobHash is the name of a blob: the first sixteen bytes of the BLAKE3
// digest of its content, in hex.
func BlobHash(data []byte) (name string) {
	sum := blake3.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

// StatementHash is the hash a record's frontmatter entry keeps of the
// statement it was computed from. Leading and trailing white space do not
// count.
func StatementHash(statement string) (hash string) {
	return BlobHash([]byte(strings.TrimSpace(statement)))
}

// PinOf is the one hash over a notebook's records: their names and the
// names of their blobs, in name order (ADR-0371 §SD8). records must be
// sorted by name, as [Notebook.Records] is.
//
// It is computed, never stored. A blob's name is its content's hash and the
// frontmatter lists every blob, so the pin follows from the text; written
// into the text as well it would be a second copy that could disagree.
func PinOf(records []RecordRef) (pin string) {
	h := blake3.New(16, nil)
	for _, r := range records {
		_, _ = h.Write([]byte(r.Name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(blobStem(r.Blob)))
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func blobStem(p string) string {
	base := path.Base(p)
	return strings.TrimSuffix(base, path.Ext(base))
}

// ReadBlob reads a blob and checks its content against its name.
func ReadBlob(fsys fs.FS, p string) (data []byte, err error) {
	data, err = fs.ReadFile(fsys, p)
	if err != nil {
		return nil, eb.Build().Str("path", p).Errorf("read blob: %w", err)
	}
	if got, want := BlobHash(data), blobStem(p); got != want {
		return nil, eb.Build().Str("path", p).Str("content", got).Errorf("the blob's content does not match its name")
	}
	return data, nil
}

// IsBlobName reports a path whose file is named as a blob is: by a content
// hash, the hex of sixteen bytes.
func IsBlobName(p string) bool {
	stem := blobStem(p)
	if len(stem) != 32 {
		return false
	}
	for i := 0; i < len(stem); i++ {
		if c := stem[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ReadAsset reads a file a notebook's text names beside it: a recording or
// a prose image. One named as a blob is checked against its name, as
// [ReadBlob] does. One under any other name is read as the file it is: a
// notebook written by hand names its pictures as any markdown does, and is
// drawn with them.
func ReadAsset(fsys fs.FS, p string) (data []byte, err error) {
	if IsBlobName(p) {
		return ReadBlob(fsys, p)
	}
	data, err = fs.ReadFile(fsys, p)
	if err != nil {
		return nil, eb.Build().Str("path", p).Errorf("read file: %w", err)
	}
	return data, nil
}

// Asset is a recording or an image as [Load] read it.
type Asset struct {
	Data []byte
	// Err is why the file could not be read, nil when it was.
	Err error
}

// Loaded is a notebook's records, read and checked, and the files its media
// cells and prose images name.
type Loaded struct {
	// Records holds each record that could be read, by name.
	Records map[string]arrow.RecordBatch
	// Stale names the records whose cell no longer hashes to what the
	// frontmatter says the blob was computed from.
	Stale map[string]bool
	// StaleVia names, for a record whose own statement is as it was
	// collected, the edited record it reads, directly or through other
	// records: what is stored for it was computed from that record's
	// earlier statement.
	StaleVia map[string]string
	// Assets holds each recording and prose image by its path. The bytes
	// are needed until they are decoded; a caller that has done so may drop
	// the map.
	Assets map[string]Asset
	// Problems are records, recordings and images that could not be read.
	Problems []Problem
}

// Release releases the records.
func (inst *Loaded) Release() {
	for _, r := range inst.Records {
		r.Release()
	}
	inst.Records = nil
}

// Load reads the records a notebook's frontmatter lists, its recordings and
// the images of its prose from fsys, whose root is the notebook's
// directory. A file that cannot be read is a problem, not an error: the
// rest of the notebook is still drawn.
func Load(nb *Notebook, fsys fs.FS) (ld *Loaded) {
	ld = &Loaded{Records: make(map[string]arrow.RecordBatch), Stale: make(map[string]bool), Assets: make(map[string]Asset)}
	asset := func(cell int, what string, p string) {
		if _, seen := ld.Assets[p]; seen {
			return
		}
		data, err := ReadAsset(fsys, p)
		ld.Assets[p] = Asset{Data: data, Err: err}
		if err != nil {
			ld.Problems = append(ld.Problems, Problem{Cell: cell, Text: what + " " + p + ": " + err.Error()})
		}
	}
	for _, ref := range nb.Records {
		rec, err := readRecord(fsys, ref.Blob)
		if err != nil {
			ld.Problems = append(ld.Problems, Problem{Cell: -1, Text: "record " + ref.Name + ": " + err.Error()})
			continue
		}
		ld.Records[ref.Name] = rec
	}
	// In document order: a cell's recording, a prose cell's images.
	nextImage := 0
	for i := range nb.Cells {
		if nb.Cells[i].Kind == CellKindMedia {
			asset(i, "recording", nb.Cells[i].Path)
		}
		for ; nextImage < len(nb.Images) && nb.Images[nextImage].Cell == i; nextImage++ {
			asset(i, "image", nb.Images[nextImage].Path)
		}
	}
	for i := range nb.Cells {
		c := &nb.Cells[i]
		if c.Kind != CellKindRecord {
			continue
		}
		if ref, found := nb.Record(c.Name); found && ref.Of != "" && ref.Of != StatementHash(statementText(c.Body)) {
			ld.Stale[c.Name] = true
		}
	}
	ld.StaleVia = staleVia(nb, ld.Stale)
	return ld
}

// StaleSource reports a record whose stored result is not what the text
// would compute, and the edited record that makes it so: the record itself,
// or one it reads.
func (inst *Loaded) StaleSource(record string) (edited string, stale bool) {
	if inst.Stale[record] {
		return record, true
	}
	edited, stale = inst.StaleVia[record]
	return
}

// staleVia carries staleness through the records: a record that reads an
// edited one was computed from the earlier statement too (ADR-0371 §SD8). A
// record reads the records before it that its statement names as tables, as
// collection composes it (compose). A statement that does not parse reads
// none: it is a problem collection reports.
func staleVia(nb *Notebook, stale map[string]bool) (via map[string]string) {
	if len(stale) == 0 {
		// Nothing was edited, so no statement needs parsing.
		return nil
	}
	via = make(map[string]string)
	before := make(map[string]struct{})
	for _, c := range nb.recordCells() {
		if _, dup := before[c.Name]; dup {
			continue
		}
		if !stale[c.Name] {
			if pr, err := nanopass.Parse(statementText(c.Body)); err == nil {
				for _, ref := range analysis.ExtractTables(pr) {
					if _, earlier := before[ref.Table]; !earlier || ref.Database != "" {
						continue
					}
					if stale[ref.Table] {
						via[c.Name] = ref.Table
						break
					}
					if edited, derived := via[ref.Table]; derived {
						via[c.Name] = edited
						break
					}
				}
			}
		}
		before[c.Name] = struct{}{}
	}
	return via
}

func readRecord(fsys fs.FS, p string) (rec arrow.RecordBatch, err error) {
	data, err := ReadBlob(fsys, p)
	if err != nil {
		return nil, err
	}
	return oneBatch(data)
}
