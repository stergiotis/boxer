// Package capmapfacts encodes a business-capability corpus as `boxer.facts`
// rows and writes them (ADR-0168 §SD1, §SD2).
//
// The unit is a **competence** everywhere boxer names it — "capability" is the
// runtime's word here (ADR-0026 §SD6), and the corpus takes its own rather than
// overload it. The vault keeps the literature's spelling; see
// [github.com/stergiotis/boxer/public/gov/capmapcorpus] for where the two meet.
//
// It is the bridge between two packages that do not know about each other:
// [github.com/stergiotis/boxer/public/gov/capmapcorpus] reads markdown and has
// no idea it will become facts, and
// [github.com/stergiotis/boxer/public/gov/capmapvocab] holds the membership
// names with no opinion on what carries them. Everything about how a
// competence lands in a columnar row lives here.
//
// # Why not FactsStoreI
//
// The runtime's store interface is a closed per-kind surface — WriteGrant,
// WriteAudit, WriteWorkingset — with no generic write. Adding WriteCompetence
// to it would put a corpus concern into the keelson runtime and oblige every
// implementer to carry two methods for a tool they do not use, so this package
// encodes rows itself and hands the finished Arrow batches to a [RecordSinkI].
// That interface is one method wide and [chclient.Client] already satisfies
// it, which also means the encoding is testable with no ClickHouse anywhere
// near it.
//
// # Ids are derived, not counted
//
// The runtime mints fact ids from a per-process counter, which is right for an
// append-only event trail and wrong here. A relation has to point at the
// competence rows it joins, and a re-ingest of an unchanged vault should
// produce the same rows rather than a second set wearing new ids. So an id is
// [DeriveId] over the row's natural key: stable across runs and across
// processes, and computable for a relation's endpoints without first inserting
// the competences and reading ids back.
package capmapfacts

import (
	"context"
	"encoding/binary"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/gov/capmapvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"lukechampine.com/blake3"
)

// RecordSinkI is where encoded rows go. It is deliberately the one method the
// insert needs, so a test can capture batches and the CLI can pass a live
// ClickHouse client without either knowing about the other.
type RecordSinkI interface {
	InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) (err error)
}

// QualifiedTable is where the rows land, matching the schema's own coordinates.
var QualifiedTable = factsschema.DatabaseName + "." + factsschema.TableName

// Stats reports what an ingest encoded. Relations are counted separately from
// competences because the two are different grains and a surprise in their
// ratio is usually the first sign a vault was pointed at wrongly.
type Stats struct {
	Competences int
	Relations   int
	Rows        int
}

// DeriveId turns a natural key into the row's uint64 id.
//
// The low eight bytes of the key, which is already a blake3 digest, so this is
// a truncation rather than a second hash. Collision risk over a corpus of a
// few thousand rows is negligible, and the payoff is that a relation can
// address its endpoints arithmetically instead of through an insert-then-read.
func DeriveId(naturalKey []byte) (id uint64) {
	if len(naturalKey) < 8 {
		var padded [8]byte
		copy(padded[:], naturalKey)
		return binary.LittleEndian.Uint64(padded[:])
	}
	return binary.LittleEndian.Uint64(naturalKey[:8])
}

// relationNaturalKey identifies one relation by everything that makes it
// distinct: the endpoints, what kind of link it is, and — for a body link —
// which section it sat under, since the same competence may legitimately be
// cited from two sections and those are two relations.
func relationNaturalKey(rel capmapcorpus.Relation) (key []byte) {
	h := blake3.New(16, nil)
	for _, part := range []string{
		"capmap.relation", rel.SourceSlug, rel.Target, string(rel.Kind), rel.Section,
	} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return h.Sum(nil)
}

// BuildRecords encodes a whole corpus into Arrow batches without writing
// anything, which is what makes the encoding testable in the default lane.
//
// now stamps every row's observation timestamp. It is a parameter rather than
// a call to time.Now so a test can assert on a fixed value and two ingests of
// an unchanged vault differ only where the vault differs.
func BuildRecords(corpus capmapcorpus.Corpus, now time.Time) (records []arrow.RecordBatch, stats Stats, err error) {
	total := len(corpus.Competences) + len(corpus.Relations)
	if total == 0 {
		return nil, stats, nil
	}
	ent := dml.NewInEntityFacts(memory.NewGoAllocator(), total)
	for i := range corpus.Competences {
		encodeCompetence(ent, corpus.Competences[i], now)
		if cErr := ent.CommitEntity(); cErr != nil {
			return nil, stats, eh.Errorf("unable to commit competence %q: %w", corpus.Competences[i].Slug, cErr)
		}
		stats.Competences++
	}
	for i := range corpus.Relations {
		encodeRelation(ent, corpus.Relations[i], now)
		if cErr := ent.CommitEntity(); cErr != nil {
			return nil, stats, eh.Errorf("unable to commit relation %q -> %q: %w",
				corpus.Relations[i].SourceSlug, corpus.Relations[i].Target, cErr)
		}
		stats.Relations++
	}
	if records, err = ent.TransferRecords(nil); err != nil {
		return nil, stats, eh.Errorf("unable to transfer records: %w", err)
	}
	stats.Rows = stats.Competences + stats.Relations
	return records, stats, nil
}

// Ingest encodes the corpus and writes it to sink.
//
// Records are released here whatever happens: they hold Arrow buffers, and a
// failed insert is exactly when a caller is least likely to remember.
func Ingest(ctx context.Context, corpus capmapcorpus.Corpus, sink RecordSinkI, table string, now time.Time) (stats Stats, err error) {
	if sink == nil {
		return stats, eh.Errorf("capmapfacts: nil sink")
	}
	if table == "" {
		table = QualifiedTable
	}
	var records []arrow.RecordBatch
	if records, stats, err = BuildRecords(corpus, now); err != nil {
		return stats, err
	}
	if len(records) == 0 {
		return stats, nil
	}
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	if err = sink.InsertArrow(ctx, table, records); err != nil {
		return stats, eh.Errorf("unable to insert %d capmap rows into %s: %w", stats.Rows, table, err)
	}
	return stats, nil
}

// encodeCompetence writes one competence row: BeginEntity through the last
// section, with no commit — the caller commits, matching the runtime's own
// encoders so the batched and single paths cannot drift.
func encodeCompetence(ent *dml.InEntityFacts, competence capmapcorpus.Competence, now time.Time) {
	nk := competence.NaturalKey
	ent.BeginEntity().SetId(DeriveId(nk), nk).SetTimestamp(now)

	sym := ent.GetSectionSymbol()
	// The kind marker's value repeats the kind as text so a row is legible
	// without resolving membership ids; the membership is what identifies it.
	sym.BeginAttribute("competence").AddMembershipLowCardRef(id(capmapvocab.MembKindCompetence)).EndAttribute()
	sym.BeginAttribute(competence.Slug).AddMembershipLowCardRef(id(capmapvocab.MembCompSlug)).EndAttribute()
	addSymbolIf(sym, competence.Domain, capmapvocab.MembCompDomain)
	addSymbolIf(sym, competence.Catalog, capmapvocab.MembCompCatalog)
	addSymbolIf(sym, competence.Owner, capmapvocab.MembCompOwner)
	// Tags: one attribute each, all wearing the same membership. A tag set is
	// a small vocabulary applied across the corpus, which is what the
	// low-cardinality symbol section is for, and one attribute per tag is what
	// makes "every competence tagged X" an indexOf rather than a string scan.
	for _, tag := range competence.Tags {
		addSymbolIf(sym, tag, capmapvocab.MembCompTag)
	}
	// Lifecycle: one attribute per recorded phase, the phase riding as the
	// membership's high-card parameter so who stays attached to which phase.
	for _, ev := range competence.Lifecycle {
		if ev.By == "" {
			continue
		}
		sym.BeginAttribute(ev.By).AddMembershipMixedLowCardRef(
			id(capmapvocab.MembCompLifecycleBy), []byte(ev.Phase)).EndAttribute()
	}
	sym.EndSection()

	str := ent.GetSectionStringArray()
	addStringIf(str, competence.Name, capmapvocab.MembCompName)
	addStringIf(str, competence.Abbrev, capmapvocab.MembCompAbbrev)
	addStringIf(str, competence.Synopsis, capmapvocab.MembCompSynopsis)
	addStringIf(str, competence.VaultPath, capmapvocab.MembCompVaultPath)
	str.EndSection()

	// Scores are written unconditionally, sentinel included: "not assessed" is
	// a value to select on, not an absence to infer from a missing attribute.
	u8 := ent.GetSectionU8Array()
	u8.BeginAttributeSingle(competence.Level).AddMembershipLowCardRef(id(capmapvocab.MembCompLevel)).EndAttribute()
	u8.BeginAttributeSingle(competence.Maturity).AddMembershipLowCardRef(id(capmapvocab.MembCompMaturity)).EndAttribute()
	u8.BeginAttributeSingle(competence.Pain).AddMembershipLowCardRef(id(capmapvocab.MembCompPain)).EndAttribute()
	u8.EndSection()

	if len(competence.Sections) > 0 {
		// The heading rides as the high-card parameter because headings are
		// authored text and cannot be registered names (ADR-0168 §SD5).
		txt := ent.GetSectionTextArray()
		for _, sec := range competence.Sections {
			txt.BeginAttributeSingle(sec.Text).AddMembershipMixedLowCardRef(
				id(capmapvocab.MembCompSection), []byte(sec.Heading)).EndAttribute()
		}
		txt.EndSection()
	}

	if hasLifecycleTimes(competence.Lifecycle) {
		tm := ent.GetSectionTimeArray()
		for _, ev := range competence.Lifecycle {
			if ev.At.IsZero() {
				continue
			}
			tm.BeginAttributeSingle(ev.At).AddMembershipMixedLowCardRef(
				id(capmapvocab.MembCompLifecycleAt), []byte(ev.Phase)).EndAttribute()
		}
		tm.EndSection()
	}
}

// encodeRelation writes one relation row.
//
// The target appears twice on purpose: as a foreign key when it resolved to a
// competence, and always as text. A broken link and a citation have a target
// worth recording and no row to point at, so the text column is the one that
// is never empty.
func encodeRelation(ent *dml.InEntityFacts, rel capmapcorpus.Relation, now time.Time) {
	nk := relationNaturalKey(rel)
	ent.BeginEntity().SetId(DeriveId(nk), nk).SetTimestamp(now)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("relation").AddMembershipLowCardRef(id(capmapvocab.MembKindRelation)).EndAttribute()
	sym.BeginAttribute(string(rel.Kind)).AddMembershipLowCardRef(id(capmapvocab.MembRelKind)).EndAttribute()
	sym.BeginAttribute(rel.Resolution.String()).AddMembershipLowCardRef(id(capmapvocab.MembRelResolution)).EndAttribute()
	sym.BeginAttribute(rel.Target).AddMembershipLowCardRef(id(capmapvocab.MembRelTargetText)).EndAttribute()
	addSymbolIf(sym, rel.Section, capmapvocab.MembRelSection)
	sym.EndSection()

	fk := ent.GetSectionForeignKey()
	fk.BeginAttribute(DeriveId(capmapcorpus.NaturalKey(rel.SourceSlug))).
		AddMembershipLowCardRef(id(capmapvocab.MembRelSource)).EndAttribute()
	if resolvesToCompetence(rel.Resolution) {
		fk.BeginAttribute(DeriveId(capmapcorpus.NaturalKey(rel.Target))).
			AddMembershipLowCardRef(id(capmapvocab.MembRelTarget)).EndAttribute()
	}
	fk.EndSection()

	if rel.Kind == capmapcorpus.RelationKindSimilar {
		f64 := ent.GetSectionF64Array()
		f64.BeginAttributeSingle(rel.Ncd).AddMembershipLowCardRef(id(capmapvocab.MembRelNcd)).EndAttribute()
		f64.EndSection()
	}
}

// resolvesToCompetence reports whether the relation's target is a competence
// in the corpus, and therefore has a row to point a foreign key at.
func resolvesToCompetence(r capmapcorpus.ResolutionE) (ok bool) {
	return r == capmapcorpus.ResolutionDirect || r == capmapcorpus.ResolutionDirRef
}

func hasLifecycleTimes(events []capmapcorpus.LifecycleEvent) (any bool) {
	for _, ev := range events {
		if !ev.At.IsZero() {
			return true
		}
	}
	return false
}

// id is the uint64 the DML builders take for a membership.
func id(m registry.RegisteredNaturalKey) (v uint64) {
	return m.GetId().Value()
}

// addSymbolIf writes a low-cardinality attribute, skipping empty values.
//
// Empty is an absence rather than a value here: writing "" would put a
// meaningless entry in a dictionary-encoded column and make "has no owner"
// indistinguishable from "owner is the empty string".
func addSymbolIf(sec *dml.InEntityFactsSectionSymbol, value string, memb registry.RegisteredNaturalKey) {
	if value == "" {
		return
	}
	sec.BeginAttribute(value).AddMembershipLowCardRef(id(memb)).EndAttribute()
}

// addStringIf is addSymbolIf for the high-cardinality string section.
func addStringIf(sec *dml.InEntityFactsSectionStringArray, value string, memb registry.RegisteredNaturalKey) {
	if value == "" {
		return
	}
	sec.BeginAttributeSingle(value).AddMembershipLowCardRef(id(memb)).EndAttribute()
}
