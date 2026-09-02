package mddocfacts

import (
	"encoding/binary"
	"encoding/hex"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mddocvocab"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdextract"
)

// Kind labels, as stored in each row's Kind symbol. The membership id is
// what identifies the kind; the label is for a reader's eyes.
const (
	KindMdDoc       = "mdDoc"
	KindMdHeading   = "mdHeading"
	KindMdCodeBlock = "mdCodeBlock"
	KindMdLink      = "mdLink"
	KindMdEmphasis  = "mdEmphasis"
	KindMdTag       = "mdTag"
	// KindMdFrontmatter labels the raw-written frontmatter row, which has no
	// component; it is what a reader filters the symbol section on.
	KindMdFrontmatter = "mdFrontmatter"
)

// NewMdDocRow builds the document row for src at ts. Two-level identity: Id
// hashes (content, ts) so every ingest is its own row, while NaturalKey — and
// the queryable ContentHash — hash the content alone, so identical text is
// visibly the same document across ingests. Every item row of the document
// carries the same content hash under its DocHash.
func NewMdDocRow(src, title, fileName string, words uint64, ts time.Time) (row MdDoc) {
	contentHash := blake3.Sum256([]byte(src))

	idh := blake3.New(8, nil)
	_, _ = idh.Write([]byte(src))
	var tsb [8]byte
	binary.LittleEndian.PutUint64(tsb[:], uint64(ts.UnixNano()))
	_, _ = idh.Write(tsb[:])

	row = MdDoc{
		Id:          binary.LittleEndian.Uint64(idh.Sum(nil)),
		NaturalKey:  contentHash[:],
		Ts:          ts,
		Kind:        KindMdDoc,
		Title:       title,
		FileName:    fileName,
		Content:     src,
		ContentHash: hex.EncodeToString(contentHash[:]),
		Words:       words,
	}
	return
}

// itemId is an item row's Id: unique within the ingest, since the document's
// Id already carries the ingest time.
func itemId(docId uint64, kind string, ordinal uint64) uint64 {
	h := blake3.New(8, nil)
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], docId)
	_, _ = h.Write(b[:])
	_, _ = h.Write([]byte(kind))
	_, _ = h.Write([]byte{0})
	binary.LittleEndian.PutUint64(b[:], ordinal)
	_, _ = h.Write(b[:])
	return binary.LittleEndian.Uint64(h.Sum(nil))
}

// itemNaturalKey is an item row's natural key: the same across ingests of
// identical content, like the document's own.
func itemNaturalKey(contentHash []byte, kind string, ordinal uint64) []byte {
	h := blake3.New(16, nil)
	_, _ = h.Write([]byte("mddoc." + kind))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(contentHash)
	_, _ = h.Write([]byte{0})
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], ordinal)
	_, _ = h.Write(b[:])
	return h.Sum(nil)
}

// Rows is one document's worth of facts, ready to write: the document row,
// one row per extracted item, and the frontmatter when the document has one.
// It is a pure value — BuildRows never touches a store — so the encoding is
// testable without ClickHouse.
type Rows struct {
	Doc        MdDoc
	Headings   []MdHeading
	CodeBlocks []MdCodeBlock
	Links      []MdLink
	Emphases   []MdEmphasis
	Tags       []MdTag
	// Frontmatter is nil when the document has no YAML block. A block that
	// failed to parse is still present, with no leaves.
	Frontmatter *mdextract.Frontmatter
}

// Count is the number of rows Ingest will write.
func (inst *Rows) Count() (n int) {
	n = 1 + len(inst.Headings) + len(inst.CodeBlocks) + len(inst.Links) + len(inst.Emphases) + len(inst.Tags)
	if inst.Frontmatter != nil {
		n++
	}
	return
}

// BuildRows maps an extraction onto the store's kinds. fileName is the
// document's display name: a basename from an editor, a vault-relative
// slash-separated path from the ingestor.
func BuildRows(src []byte, fileName string, ts time.Time, ex *mdextract.Document) (rows Rows) {
	rows.Doc = NewMdDocRow(string(src), ex.Title, fileName, ex.Words, ts)
	docId := rows.Doc.Id
	hash := rows.Doc.NaturalKey

	section := func(idx int) option.Option[uint64] {
		if idx < 0 {
			return option.None[uint64]()
		}
		return option.Some(uint64(idx))
	}

	rows.Headings = make([]MdHeading, 0, len(ex.Headings))
	for _, h := range ex.Headings {
		row := MdHeading{
			Id:         itemId(docId, KindMdHeading, h.Ordinal),
			NaturalKey: itemNaturalKey(hash, KindMdHeading, h.Ordinal),
			Ts:         ts,
			Kind:       KindMdHeading,
			Doc:        docId,
			DocHash:    hash,
			Ordinal:    h.Ordinal,
			Line:       h.Line,
			Level:      h.Level,
			Text:       h.Text,
			Slug:       h.Slug,
			Parent:     section(h.Parent),
			Path:       h.Path,
		}
		if h.Anchor != "" {
			row.Anchor = option.Some(h.Anchor)
		}
		rows.Headings = append(rows.Headings, row)
	}
	rows.CodeBlocks = make([]MdCodeBlock, 0, len(ex.CodeBlocks))
	for _, c := range ex.CodeBlocks {
		rows.CodeBlocks = append(rows.CodeBlocks, MdCodeBlock{
			Id:         itemId(docId, KindMdCodeBlock, c.Ordinal),
			NaturalKey: itemNaturalKey(hash, KindMdCodeBlock, c.Ordinal),
			Ts:         ts,
			Kind:       KindMdCodeBlock,
			Doc:        docId,
			DocHash:    hash,
			Ordinal:    c.Ordinal,
			Line:       c.Line,
			Section:    section(c.Section),
			Language:   c.Language,
			Info:       c.Info,
			Content:    c.Content,
			Lines:      c.Lines,
		})
	}
	rows.Links = make([]MdLink, 0, len(ex.Links))
	for _, l := range ex.Links {
		rows.Links = append(rows.Links, MdLink{
			Id:         itemId(docId, KindMdLink, l.Ordinal),
			NaturalKey: itemNaturalKey(hash, KindMdLink, l.Ordinal),
			Ts:         ts,
			Kind:       KindMdLink,
			Doc:        docId,
			DocHash:    hash,
			Ordinal:    l.Ordinal,
			Line:       l.Line,
			Section:    section(l.Section),
			Spelling:   l.Kind.String(),
			Target:     l.Target,
			Fragment:   l.Fragment,
			Text:       l.Text,
			External:   l.External,
		})
	}
	rows.Emphases = make([]MdEmphasis, 0, len(ex.Emphases))
	for _, e := range ex.Emphases {
		rows.Emphases = append(rows.Emphases, MdEmphasis{
			Id:         itemId(docId, KindMdEmphasis, e.Ordinal),
			NaturalKey: itemNaturalKey(hash, KindMdEmphasis, e.Ordinal),
			Ts:         ts,
			Kind:       KindMdEmphasis,
			Doc:        docId,
			DocHash:    hash,
			Ordinal:    e.Ordinal,
			Line:       e.Line,
			Section:    section(e.Section),
			Style:      e.Style.String(),
			Text:       e.Text,
		})
	}
	rows.Tags = make([]MdTag, 0, len(ex.Tags))
	for _, t := range ex.Tags {
		rows.Tags = append(rows.Tags, MdTag{
			Id:         itemId(docId, KindMdTag, t.Ordinal),
			NaturalKey: itemNaturalKey(hash, KindMdTag, t.Ordinal),
			Ts:         ts,
			Kind:       KindMdTag,
			Doc:        docId,
			DocHash:    hash,
			Ordinal:    t.Ordinal,
			Line:       t.Line,
			Section:    section(t.Section),
			Source:     t.Source.String(),
			Name:       t.Tag,
		})
	}
	rows.Frontmatter = ex.Frontmatter
	return
}

// IngestDocument extracts src and buffers every row for it. Rows ship on the
// store's next Flush, like every write. The returned Rows are what was
// buffered — the document Id is the key a reader filters on.
func (inst *MddocStore) IngestDocument(src []byte, fileName string, ts time.Time) (rows Rows, err error) {
	rows = BuildRows(src, fileName, ts, mdextract.Extract(src))
	err = inst.IngestRows(rows)
	return
}

// IngestRows buffers one document's rows: the document, its items, and the
// frontmatter row. Each row is its own entity; every one carries its natural
// key in the envelope, which the generated Ingest<Kind> verbs do not set.
func (inst *MddocStore) IngestRows(rows Rows) (err error) {
	ts := rows.Doc.Ts
	if err = inst.Begin(rows.Doc.Id, ts, MddocEnvelope{NaturalKey: rows.Doc.NaturalKey}).AddMdDoc(rows.Doc).Commit(); err != nil {
		return eh.Errorf("ingest document row: %w", err)
	}
	for i := range rows.Headings {
		r := &rows.Headings[i]
		if err = inst.Begin(r.Id, ts, MddocEnvelope{NaturalKey: r.NaturalKey}).AddMdHeading(*r).Commit(); err != nil {
			return eb.Build().Uint64("ordinal", r.Ordinal).Errorf("ingest heading: %w", err)
		}
	}
	for i := range rows.CodeBlocks {
		r := &rows.CodeBlocks[i]
		if err = inst.Begin(r.Id, ts, MddocEnvelope{NaturalKey: r.NaturalKey}).AddMdCodeBlock(*r).Commit(); err != nil {
			return eb.Build().Uint64("ordinal", r.Ordinal).Errorf("ingest code block: %w", err)
		}
	}
	for i := range rows.Links {
		r := &rows.Links[i]
		if err = inst.Begin(r.Id, ts, MddocEnvelope{NaturalKey: r.NaturalKey}).AddMdLink(*r).Commit(); err != nil {
			return eb.Build().Uint64("ordinal", r.Ordinal).Errorf("ingest link: %w", err)
		}
	}
	for i := range rows.Emphases {
		r := &rows.Emphases[i]
		if err = inst.Begin(r.Id, ts, MddocEnvelope{NaturalKey: r.NaturalKey}).AddMdEmphasis(*r).Commit(); err != nil {
			return eb.Build().Uint64("ordinal", r.Ordinal).Errorf("ingest emphasis: %w", err)
		}
	}
	for i := range rows.Tags {
		r := &rows.Tags[i]
		if err = inst.Begin(r.Id, ts, MddocEnvelope{NaturalKey: r.NaturalKey}).AddMdTag(*r).Commit(); err != nil {
			return eb.Build().Uint64("ordinal", r.Ordinal).Errorf("ingest tag: %w", err)
		}
	}
	if rows.Frontmatter != nil {
		if err = inst.ingestFrontmatter(rows.Doc, rows.Frontmatter); err != nil {
			return eh.Errorf("ingest frontmatter: %w", err)
		}
	}
	return
}

// ingestFrontmatter writes the frontmatter row through the raw DML. The row
// has no component: its leaves ride the mixed membership channel, which the
// generated lane does not read (doc/explanation/facts-bound-record-stores.md),
// and Raw() and Add<Kind>() are exclusive per entity — so the frontmatter is
// a row of its own beside the document, pointing back at it.
//
// Sections keep one channel each: the kind marker, the document reference and
// the hash ride low-card-ref on symbol, foreignKey and blobArray; the leaves
// ride the mixed channel on the typed sections — stringArray, i64Array,
// f64Array, bool, timeArray — and on symbolArray for the value-less markers.
// Nothing here mixes channels within a section.
func (inst *MddocStore) ingestFrontmatter(doc MdDoc, fm *mdextract.Frontmatter) (err error) {
	id := itemId(doc.Id, KindMdFrontmatter, 0)
	b := inst.Begin(id, doc.Ts, MddocEnvelope{NaturalKey: itemNaturalKey(doc.NaturalKey, KindMdFrontmatter, 0)})
	raw := b.Raw()

	sym := raw.GetSectionSymbol()
	sym.BeginAttribute(KindMdFrontmatter).
		AddMembershipLowCardRef(mddocvocab.MembKindFrontmatter.GetId().Value()).
		EndAttributeP()
	sym.EndSection()

	fk := raw.GetSectionForeignKey()
	fk.BeginAttribute(doc.Id).
		AddMembershipLowCardRef(mddocvocab.MembFrontmatterDoc.GetId().Value()).
		EndAttributeP()
	fk.EndSection()

	blob := raw.GetSectionBlobArray()
	blob.BeginAttributeSingle(doc.NaturalKey).
		AddMembershipLowCardRef(mddocvocab.MembFrontmatterDocHash.GetId().Value()).
		EndAttributeP()
	blob.EndSection()

	pathId := mddocvocab.MembFrontmatterPath.GetId().Value()
	paramsId := mddocvocab.MembFrontmatterParams.GetId().Value()
	// address attaches a leaf's two memberships; the params one only when
	// the path crosses an array, the jsonbench construction.
	address := func(add func(uint64, []byte), l *mdextract.Leaf) (aerr error) {
		add(pathId, []byte(l.Path))
		if len(l.Params) == 0 {
			return
		}
		var raw []byte
		raw, aerr = membership.EncodeParams(l.Params...)
		if aerr != nil {
			return eb.Build().Str("path", l.Path).Errorf("encode params: %w", aerr)
		}
		add(paramsId, raw)
		return
	}

	// A section's frame is entered once per entity, so leaves are bucketed
	// by section before any attribute opens.
	var strs, ints, floats, bools, times, markers []*mdextract.Leaf
	for i := range fm.Leaves {
		l := &fm.Leaves[i]
		switch l.Kind {
		case mdextract.LeafKindString:
			strs = append(strs, l)
		case mdextract.LeafKindInt:
			ints = append(ints, l)
		case mdextract.LeafKindFloat:
			floats = append(floats, l)
		case mdextract.LeafKindBool:
			bools = append(bools, l)
		case mdextract.LeafKindTime:
			times = append(times, l)
		default:
			markers = append(markers, l)
		}
	}
	if len(strs) > 0 {
		sec := raw.GetSectionStringArray()
		for _, l := range strs {
			a := sec.BeginAttributeSingle(l.S)
			err = address(a.AddMembershipMixedLowCardRefP, l)
			a.EndAttributeP()
			if err != nil {
				_ = b.Rollback()
				return
			}
		}
		sec.EndSection()
	}
	if len(ints) > 0 {
		sec := raw.GetSectionI64Array()
		for _, l := range ints {
			a := sec.BeginAttributeSingle(l.I)
			err = address(a.AddMembershipMixedLowCardRefP, l)
			a.EndAttributeP()
			if err != nil {
				_ = b.Rollback()
				return
			}
		}
		sec.EndSection()
	}
	if len(floats) > 0 {
		sec := raw.GetSectionF64Array()
		for _, l := range floats {
			a := sec.BeginAttributeSingle(l.F)
			err = address(a.AddMembershipMixedLowCardRefP, l)
			a.EndAttributeP()
			if err != nil {
				_ = b.Rollback()
				return
			}
		}
		sec.EndSection()
	}
	if len(bools) > 0 {
		sec := raw.GetSectionBool()
		for _, l := range bools {
			a := sec.BeginAttribute(l.B)
			err = address(a.AddMembershipMixedLowCardRefP, l)
			a.EndAttributeP()
			if err != nil {
				_ = b.Rollback()
				return
			}
		}
		sec.EndSection()
	}
	if len(times) > 0 {
		sec := raw.GetSectionTimeArray()
		for _, l := range times {
			a := sec.BeginAttributeSingle(l.T)
			err = address(a.AddMembershipMixedLowCardRefP, l)
			a.EndAttributeP()
			if err != nil {
				_ = b.Rollback()
				return
			}
		}
		sec.EndSection()
	}
	if len(markers) > 0 {
		sec := raw.GetSectionSymbolArray()
		for _, l := range markers {
			a := sec.BeginAttributeSingle(l.Kind.String())
			err = address(a.AddMembershipMixedLowCardRefP, l)
			a.EndAttributeP()
			if err != nil {
				_ = b.Rollback()
				return
			}
		}
		sec.EndSection()
	}
	return b.Commit()
}
