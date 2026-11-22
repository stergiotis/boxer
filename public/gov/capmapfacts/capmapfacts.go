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
// WriteLog, WriteLaunch — with no generic write. Adding WriteCompetence
// to it would put a corpus concern into the keelson runtime and oblige every
// implementer to carry two methods for a tool they do not use, so this package
// encodes rows itself and hands the finished Arrow batches to a [RecordSinkI].
// That interface is one method wide and [chclient.Client] already satisfies
// it, which also means the encoding is testable with no ClickHouse anywhere
// near it.
//
// # Ids are resolved from the natural key
//
// A relation has to point at the competence rows it joins, and a re-ingest
// of an unchanged vault should restate the same entities rather than a
// second set wearing new ids. So a row's id is what the facts table's keyed
// id generator resolves the row's natural key to (ADR-0405 §SD2): the same
// id from every process and across ingests, drawn from the table's stream.
// A relation's endpoints resolve through the same generator, so they are
// the ids of the competence rows. The encoder therefore needs a generator,
// and a load needs the server that holds the id directory.
//
// Rows written before that carry [LegacyId], a truncation of the natural
// key. The read-back knows both.
package capmapfacts

import (
	"context"
	"encoding/binary"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema/dml"
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

// IdScope is the scope the corpus's natural keys are resolved in. One scope
// serves competences and relations: their keys are digests under two
// domains and cannot meet.
const IdScope = "capmap"

// LegacyId is the id a row carried before ids were resolved through the
// facts table's generator (ADR-0405 §SD5): the low eight bytes of its
// natural key. Rows written then keep it, and a relation among them points
// at its endpoints by it, so the read-back accepts it beside the id a
// competence row carries.
func LegacyId(naturalKey []byte) (id uint64) {
	if len(naturalKey) < 8 {
		var padded [8]byte
		copy(padded[:], naturalKey)
		return binary.LittleEndian.Uint64(padded[:])
	}
	return binary.LittleEndian.Uint64(naturalKey[:8])
}

// corpusIds holds the id of every natural key an ingest names.
type corpusIds map[string]uint64

// resolveIds resolves every natural key the corpus names — each
// competence, each relation, and each relation's endpoints — in one batch
// when the generator takes one. An endpoint is resolved whether or not its
// competence is in this corpus, so a relation's foreign key is the id that
// competence has or will have.
func resolveIds(ctx context.Context, corpus capmapcorpus.Corpus, ids identifier.IdGeneratorI) (resolved corpusIds, err error) {
	if ids == nil {
		return nil, eh.Errorf("capmapfacts: no id generator: a row's id is resolved from its natural key (ADR-0405)")
	}
	resolved = make(corpusIds, len(corpus.Competences)+2*len(corpus.Relations))
	var keys identgen.KeysColumn
	add := func(key []byte) {
		if _, seen := resolved[string(key)]; seen {
			return
		}
		resolved[string(key)] = 0
		keys = keys.AppendKey(key)
	}
	for i := range corpus.Competences {
		add(corpus.Competences[i].NaturalKey)
	}
	for i := range corpus.Relations {
		rel := corpus.Relations[i]
		add(relationNaturalKey(rel))
		add(capmapcorpus.NaturalKey(rel.SourceSlug))
		if resolvesToCompetence(rel.Resolution) {
			add(capmapcorpus.NaturalKey(rel.Target))
		}
	}
	var tagged []identifier.TaggedId
	if batch, ok := ids.(identgen.BatchInternalizerI); ok {
		if tagged, _, err = batch.AppendIds(ctx, nil, keys, nil); err != nil {
			return nil, eh.Errorf("capmapfacts: resolve ids: %w", err)
		}
	} else {
		tagged = make([]identifier.TaggedId, keys.Len())
		for i := range tagged {
			if tagged[i], _, err = ids.GetId(ctx, keys.At(i)); err != nil {
				return nil, eh.Errorf("capmapfacts: resolve ids: %w", err)
			}
		}
	}
	for i, id := range tagged {
		resolved[string(keys.At(i))] = id.Value()
	}
	return
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
// an unchanged vault differ only where the vault differs. origin is who
// ingests (ADR-0330 §SD3); every row carries it under its own id.
//
// ids is the facts table's keyed id generator for [IdScope]: it is what
// makes a competence's id the same across ingests and processes. A
// generator that ignores the key still gives one ingest consistent ids — a
// relation points at the competence rows beside it — and gives the next
// ingest others.
func BuildRecords(ctx context.Context, corpus capmapcorpus.Corpus, now time.Time, origin Origin, ids identifier.IdGeneratorI) (records []arrow.RecordBatch, stats Stats, err error) {
	total := len(corpus.Competences) + len(corpus.Relations)
	if total == 0 {
		return nil, stats, nil
	}
	resolved, err := resolveIds(ctx, corpus, ids)
	if err != nil {
		return nil, stats, err
	}
	ent := dml.NewInEntityFacts(memory.NewGoAllocator(), total)
	for i := range corpus.Competences {
		if eErr := encodeCompetence(ent, corpus.Competences[i], now, origin, resolved); eErr != nil {
			return nil, stats, eb.Build().Str("slug", corpus.Competences[i].Slug).Errorf("unable to encode competence: %w", eErr)
		}
		if cErr := ent.CommitEntity(); cErr != nil {
			return nil, stats, eb.Build().Str("slug", corpus.Competences[i].Slug).Errorf("unable to commit competence: %w", cErr)
		}
		stats.Competences++
	}
	for i := range corpus.Relations {
		if eErr := encodeRelation(ent, corpus.Relations[i], now, origin, resolved); eErr != nil {
			return nil, stats, eb.Build().Str("sourceSlug", corpus.Relations[i].SourceSlug).Str("target", corpus.Relations[i].Target).
				Errorf("unable to encode relation: %w", eErr)
		}
		if cErr := ent.CommitEntity(); cErr != nil {
			return nil, stats, eb.Build().Str("sourceSlug", corpus.Relations[i].SourceSlug).Str("target", corpus.Relations[i].Target).
				Errorf("unable to commit relation: %w", cErr)
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
func Ingest(ctx context.Context, corpus capmapcorpus.Corpus, sink RecordSinkI, table string, now time.Time, origin Origin, ids identifier.IdGeneratorI) (stats Stats, err error) {
	if sink == nil {
		return stats, eh.Errorf("capmapfacts: nil sink")
	}
	if table == "" {
		table = QualifiedTable
	}
	var records []arrow.RecordBatch
	if records, stats, err = BuildRecords(ctx, corpus, now, origin, ids); err != nil {
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
		return stats, eb.Build().Int("rows", stats.Rows).Str("table", table).Errorf("unable to insert capmap rows: %w", err)
	}
	return stats, nil
}

// encodeCompetence writes one competence row: BeginEntity through the last
// section, with no commit — the caller commits, matching the runtime's own
// encoders so the batched and single paths cannot drift. The row carries
// six components (Origin, CompetenceRecord, CompetenceTags,
// CompetenceLifecycle, CompetenceClaims, CompetenceClaimBases); the symbol
// section holds attributes of all six, the string and time sections of two
// each.
func encodeCompetence(ent *dml.InEntityFacts, competence capmapcorpus.Competence, now time.Time, origin Origin, ids corpusIds) (err error) {
	nk := competence.NaturalKey
	id := ids[string(nk)]
	ent.BeginEntity().SetId(id, nk).SetTimestamp(now)
	rec, tags, lifecycle, claims, bases := competenceComponents(competence)
	origin.Id = id

	sym := ent.GetSectionSymbol()
	if err = OriginEmitSectionSymbol(sym, origin); err != nil {
		return
	}
	if err = CompetenceRecordEmitSectionSymbol(sym, rec); err != nil {
		return
	}
	if err = CompetenceTagsEmitSectionSymbol(sym, tags); err != nil {
		return
	}
	if err = CompetenceLifecycleEmitSectionSymbol(sym, lifecycle); err != nil {
		return
	}
	if err = CompetenceClaimsEmitSectionSymbol(sym, claims); err != nil {
		return
	}
	if err = CompetenceClaimBasesEmitSectionSymbol(sym, bases); err != nil {
		return
	}
	sym.EndSection()

	str := ent.GetSectionStringArray()
	if err = CompetenceRecordEmitSectionStringArray(str, rec); err != nil {
		return
	}
	if err = CompetenceClaimsEmitSectionStringArray(str, claims); err != nil {
		return
	}
	str.EndSection()

	u8 := ent.GetSectionU8Array()
	if err = CompetenceRecordEmitSectionU8Array(u8, rec); err != nil {
		return
	}
	u8.EndSection()

	if len(rec.Sections) > 0 {
		txt := ent.GetSectionTextArray()
		if err = CompetenceRecordEmitSectionTextArray(txt, rec); err != nil {
			return
		}
		txt.EndSection()
	}
	if len(rec.LifecycleAt) > 0 || len(claims.At) > 0 {
		tm := ent.GetSectionTimeArray()
		if err = CompetenceRecordEmitSectionTimeArray(tm, rec); err != nil {
			return
		}
		if err = CompetenceClaimsEmitSectionTimeArray(tm, claims); err != nil {
			return
		}
		tm.EndSection()
	}
	u64 := ent.GetSectionU64Array()
	if err = OriginEmitSectionU64Array(u64, origin); err != nil {
		return
	}
	u64.EndSection()
	return
}

// competenceComponents maps a competence onto its row's components. Empty
// text is left out, as is a lifecycle event without a who (from
// CompetenceLifecycle) or without a time (from LifecycleAt), a claim's
// assessor or date when absent, and a claim marked as a duplicate of an
// earlier one on its dimension — the parameter would collide, and the first
// claim is the one that counts.
func competenceComponents(c capmapcorpus.Competence) (rec CompetenceRecord, tags CompetenceTags, lifecycle CompetenceLifecycle, claims CompetenceClaims, bases CompetenceClaimBases) {
	rec = CompetenceRecord{
		Slug:      c.Slug,
		Domain:    nonEmpty(c.Domain),
		Catalog:   nonEmpty(c.Catalog),
		Owner:     nonEmpty(c.Owner),
		Name:      nonEmpty(c.Name),
		Abbrev:    nonEmpty(c.Abbrev),
		Synopsis:  nonEmpty(c.Synopsis),
		VaultPath: nonEmpty(c.VaultPath),
		Level:     c.Level,
		Maturity:  c.Maturity,
		Pain:      c.Pain,
	}
	for _, sec := range c.Sections {
		rec.Sections = append(rec.Sections, CompetenceSection{Heading: sec.Heading, Text: sec.Text})
	}
	for _, tag := range c.Tags {
		if tag != "" {
			tags.Tags = append(tags.Tags, CompetenceTag{Value: tag})
		}
	}
	for _, ev := range c.Lifecycle {
		if ev.By != "" {
			lifecycle.By = append(lifecycle.By, CompetencePhaseBy{Phase: string(ev.Phase), By: ev.By})
		}
		if !ev.At.IsZero() {
			rec.LifecycleAt = append(rec.LifecycleAt, CompetencePhaseAt{Phase: string(ev.Phase), At: ev.At})
		}
	}
	for _, claim := range c.Assessment {
		if claim.HasProblem(capmapcorpus.ClaimProblemDuplicateDimension) {
			continue
		}
		claims.Rating = append(claims.Rating, ClaimRating{Dimension: claim.Dimension, Rating: claim.Rating})
		if claim.By != "" {
			claims.By = append(claims.By, ClaimBy{Dimension: claim.Dimension, By: claim.By})
		}
		if !claim.At.IsZero() {
			claims.At = append(claims.At, ClaimAt{Dimension: claim.Dimension, At: claim.At})
		}
		for _, a := range claim.Basis {
			bases.Basis = append(bases.Basis, ClaimBasis{Dimension: claim.Dimension, Anchor: a.String()})
		}
	}
	return
}

// encodeRelation writes one relation row, carrying the origin.
func encodeRelation(ent *dml.InEntityFacts, rel capmapcorpus.Relation, now time.Time, origin Origin, ids corpusIds) (err error) {
	nk := relationNaturalKey(rel)
	id := ids[string(nk)]
	ent.BeginEntity().SetId(id, nk).SetTimestamp(now)
	rec := relationRecord(rel, ids)
	origin.Id = id

	sym := ent.GetSectionSymbol()
	if err = OriginEmitSectionSymbol(sym, origin); err != nil {
		return
	}
	if err = RelationRecordEmitSectionSymbol(sym, rec); err != nil {
		return
	}
	sym.EndSection()

	fk := ent.GetSectionForeignKey()
	if err = RelationRecordEmitSectionForeignKey(fk, rec); err != nil {
		return
	}
	fk.EndSection()

	if rec.Ncd.Has {
		f64 := ent.GetSectionF64Array()
		if err = RelationRecordEmitSectionF64Array(f64, rec); err != nil {
			return
		}
		f64.EndSection()
	}
	u64 := ent.GetSectionU64Array()
	if err = OriginEmitSectionU64Array(u64, origin); err != nil {
		return
	}
	u64.EndSection()
	return
}

// relationRecord maps a relation onto its row's component: a foreign key
// to the target only when it resolved to a competence, a score only for a
// similarity.
func relationRecord(rel capmapcorpus.Relation, ids corpusIds) (rec RelationRecord) {
	rec = RelationRecord{
		RelKind:      string(rel.Kind),
		Resolution:   rel.Resolution.String(),
		TargetText:   rel.Target,
		Section:      nonEmpty(rel.Section),
		TargetStatus: nonEmpty(rel.TargetStatus),
		Source:       ids[string(capmapcorpus.NaturalKey(rel.SourceSlug))],
	}
	if resolvesToCompetence(rel.Resolution) {
		rec.Target = option.Some(ids[string(capmapcorpus.NaturalKey(rel.Target))])
	}
	if rel.Kind == capmapcorpus.RelationKindSimilar {
		rec.Ncd = option.Some(rel.Ncd)
	}
	return
}

// resolvesToCompetence reports whether the relation's target is a competence
// in the corpus, and therefore has a row to point a foreign key at.
func resolvesToCompetence(r capmapcorpus.ResolutionE) (ok bool) {
	return r == capmapcorpus.ResolutionDirect || r == capmapcorpus.ResolutionDirRef
}

// nonEmpty is the absence rule for text: empty is not written.
func nonEmpty(v string) (o option.Option[string]) {
	if v != "" {
		o = option.Some(v)
	}
	return
}
