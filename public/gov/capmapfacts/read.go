package capmapfacts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/gov/capmapvocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// Reading the corpus back out of `boxer.facts` — the inverse of [Ingest], and
// what `boxer capmap dump` renders a vault from.
//
// # Why this exists when the provider tables read the vault
//
// The keelson `competence` tables deliberately read markdown rather than the
// store (ADR-0168 §SD8): a lens should not depend on an ingest having been run,
// and it should not spell physical column names. A dump has neither escape —
// its whole job is to say what the *store* holds, so that a corpus can survive
// the vault being lost, and so an edit made anywhere else can be brought back
// into diffable form.
//
// What it does NOT do any more is spell the physical column names out. The
// query is written in column handles and resolved against the generated schema
// (see handles.go), so a regeneration that re-aspects a column cannot leave a
// stale literal behind here.
//
// # What comes back, and what does not
//
// Everything the encoding writes: metadata, scores, tags, prose sections with
// their headings and order, lifecycle who/when, and relations with their kind,
// resolution, section and score.
//
// One thing does not, and it is invisible in the output: whether a frontmatter
// link was written `[[slug]]` or `[[slug/capability]]`. The encoding stores the
// resolved target, not the spelling, so a dumped vault writes the bare form.
// Both name the same competence and resolve identically for the frontmatter
// kinds, so a re-read of a dumped vault yields the same corpus — the difference
// is textual, and only against an original vault that used the qualified form.
//
// # Duplicates are expected
//
// Ids are derived from natural keys, so a re-ingest restates the same entities
// rather than minting new ones — by design, and it means the table holds one
// row per ingest per competence. The newest wins here (`ORDER BY ts DESC LIMIT
// 1 BY id`), which is the read a ReplacingMergeTree would eventually give
// anyway, without waiting for a merge.

// QuerierI is where read-back rows come from. One method, for the same reason
// [RecordSinkI] is one method: [chclient.Client] already satisfies it, and a
// test can serve canned rows without a server.
type QuerierI interface {
	Query(ctx context.Context, sql string) (body io.ReadCloser, err error)
}

// ReadCorpus reads every competence and relation from table.
//
// The result is in [capmapcorpus.ParseDir] order — competences by slug,
// relations following their source — so a corpus that went through the store
// compares directly against one read from a vault.
func ReadCorpus(ctx context.Context, q QuerierI, table string) (corpus capmapcorpus.Corpus, err error) {
	if q == nil {
		return corpus, eh.Errorf("capmapfacts: nil querier")
	}
	if table == "" {
		table = QualifiedTable
	}
	if err = requireSurface(ctx, q); err != nil {
		return corpus, err
	}
	comps, err := readCompetences(ctx, q, table)
	if err != nil {
		return corpus, err
	}
	corpus.Competences = comps

	// Relations address their endpoints by derived id, so the slug behind an
	// id is recovered from the competences just read rather than by joining
	// the table to itself — a join would have to repeat the decode, and the
	// mapping is arithmetic anyway.
	slugById := make(map[uint64]string, len(comps))
	for i := range comps {
		slugById[DeriveId(comps[i].NaturalKey)] = comps[i].Slug
	}
	rels, err := readRelations(ctx, q, table, slugById)
	if err != nil {
		return corpus, err
	}
	corpus.Relations = rels
	capmapcorpus.SortCorpus(&corpus)
	return corpus, nil
}

// competenceRow is one decoded competence as JSONEachRow delivers it. Every
// 64-bit value is a string: ClickHouse quotes them in JSON by default, and
// asking for them as strings explicitly means the decode does not depend on a
// server setting.
type competenceRow struct {
	Slug      string   `json:"slug"`
	Name      string   `json:"name"`
	Abbrev    string   `json:"abbrev"`
	Synopsis  string   `json:"synopsis"`
	Domain    string   `json:"domain"`
	Catalog   string   `json:"catalog"`
	Owner     string   `json:"owner"`
	VaultPath string   `json:"vault_path"`
	Level     uint8    `json:"level"`
	Maturity  uint8    `json:"maturity"`
	Pain      uint8    `json:"pain"`
	Tags      []string `json:"tags"`

	SectionHeadings []string `json:"section_headings"`
	SectionTexts    []string `json:"section_texts"`

	LifecycleByPhases []string `json:"lifecycle_by_phases"`
	LifecycleBy       []string `json:"lifecycle_by"`
	LifecycleAtPhases []string `json:"lifecycle_at_phases"`
	LifecycleAt       []string `json:"lifecycle_at"`
}

// relationRow is one decoded relation.
type relationRow struct {
	SourceId   string  `json:"source_id"`
	TargetId   string  `json:"target_id"`
	Target     string  `json:"target"`
	Kind       string  `json:"kind"`
	Resolution string  `json:"resolution"`
	Section    string  `json:"section"`
	Ncd        float64 `json:"ncd"`
}

func readCompetences(ctx context.Context, q QuerierI, table string) (comps []capmapcorpus.Competence, err error) {
	sql := competenceSQL(table)
	rows, err := queryJSON[competenceRow](ctx, q, table, sql)
	if err != nil {
		return nil, eh.Errorf("unable to read competences from %s: %w", table, err)
	}
	comps = make([]capmapcorpus.Competence, 0, len(rows))
	for _, r := range rows {
		if r.Slug == "" {
			// A row wearing the competence kind but carrying no slug is not
			// addressable, so there is nothing to write it back as.
			return nil, eh.Errorf("capmapfacts: a competence row in %s carries no slug", table)
		}
		comp := capmapcorpus.Competence{
			Slug:       r.Slug,
			NaturalKey: capmapcorpus.NaturalKey(r.Slug),
			VaultPath:  r.VaultPath,
			Name:       r.Name,
			Abbrev:     r.Abbrev,
			Synopsis:   r.Synopsis,
			Domain:     r.Domain,
			Catalog:    r.Catalog,
			Owner:      r.Owner,
			Level:      r.Level,
			Maturity:   r.Maturity,
			Pain:       r.Pain,
			// An empty JSON array decodes to an empty slice, and the parser
			// yields nil for a competence with no tags. They mean the same
			// thing and must compare the same, so the store's answer is
			// normalised to the parser's.
			Tags:      nilIfEmpty(r.Tags),
			Sections:  sectionsOf(r),
			Lifecycle: lifecycleOf(r),
		}
		comps = append(comps, comp)
	}
	return comps, nil
}

// requireSurface refuses to read from a server that has not been provisioned
// with the SQL read surface, and says how to fix it.
//
// The queries expand into the read-back family (ADR-0181 §SD3), so a server
// without it answers `Unknown function LW_VALUE_BY_TAG_EQUAL` — true, and no
// help at all to an operator who is trying to recover a corpus. This is the
// version handshake ADR-0171 §SD2 exists for, used for the thing it was meant
// for: telling a caller *why* an expansion cannot run here.
//
// A mismatched version is refused rather than attempted. The expansion this
// build emits is written against the surface this build declares, and a
// silently-different one is the failure mode that produces wrong rows rather
// than an error.
func requireSurface(ctx context.Context, q QuerierI) (err error) {
	body, err := q.Query(ctx, "SELECT "+lwsqlsurface.VersionFunctionName+"() AS v FORMAT TabSeparated")
	if err != nil {
		return eh.Errorf(
			"capmapfacts: this server carries no leeway SQL read surface (%s), which the read-back queries expand into; install it with `boxer leeway sqlsurface install`: %w",
			lwsqlsurface.VersionFunctionName, err)
	}
	defer func() { _ = body.Close() }()
	raw, rErr := io.ReadAll(body)
	if rErr != nil {
		return eh.Errorf("capmapfacts: unable to read the surface version: %w", rErr)
	}
	got, pErr := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if pErr != nil {
		return eh.Errorf("capmapfacts: %s() answered %q, which is not a version: %w",
			lwsqlsurface.VersionFunctionName, strings.TrimSpace(string(raw)), pErr)
	}
	if got != uint64(lwsqlsurface.Version) {
		return eh.Errorf(
			"capmapfacts: this server carries leeway SQL read surface v%d and this build emits v%d; reconcile them with `boxer leeway sqlsurface install`",
			got, lwsqlsurface.Version)
	}
	return nil
}

// nilIfEmpty is what keeps "read from the store" and "read from markdown"
// comparable: JSON has an empty array where Go has a nil slice.
func nilIfEmpty(v []string) (out []string) {
	if len(v) == 0 {
		return nil
	}
	return v
}

// sectionsOf pairs the headings with the prose they label. The two arrive as
// parallel arrays because they are two columns of one section — the heading
// rides the membership parameter, the prose is the value (ADR-0168 §SD5).
func sectionsOf(r competenceRow) (sections []capmapcorpus.Section) {
	n := min(len(r.SectionHeadings), len(r.SectionTexts))
	if n == 0 {
		return nil
	}
	sections = make([]capmapcorpus.Section, 0, n)
	for i := range n {
		sections = append(sections, capmapcorpus.Section{Heading: r.SectionHeadings[i], Text: r.SectionTexts[i]})
	}
	return sections
}

// lifecycleOf rejoins the who and the when, which are written to two different
// sections and can be present independently: the vault often carries a date
// with no name, or a name with no date.
//
// Phase order is the model's, not the row's, so a corpus read back compares
// equal to one parsed from markdown.
func lifecycleOf(r competenceRow) (events []capmapcorpus.LifecycleEvent) {
	by := make(map[string]string, len(r.LifecycleByPhases))
	for i, phase := range r.LifecycleByPhases {
		if i < len(r.LifecycleBy) {
			by[phase] = r.LifecycleBy[i]
		}
	}
	at := make(map[string]time.Time, len(r.LifecycleAtPhases))
	for i, phase := range r.LifecycleAtPhases {
		if i < len(r.LifecycleAt) {
			at[phase] = parseFactsTime(r.LifecycleAt[i])
		}
	}
	for _, phase := range capmapcorpus.AllPhases() {
		who, hasWho := by[string(phase)]
		when, hasWhen := at[string(phase)]
		if !hasWho && !hasWhen {
			continue
		}
		events = append(events, capmapcorpus.LifecycleEvent{Phase: phase, By: who, At: when})
	}
	return events
}

// factsTimeLayout is what the read-back query formats a lifecycle timestamp as,
// in Go's spelling. Its ClickHouse spelling is `%Y-%m-%d %H:%i:%S` — note `%i`
// for minutes, where `%M` is the month's *name* and silently produces
// "12:August:00".
const factsTimeLayout = "2006-01-02 15:04:05"

// parseFactsTime reads the timestamp shape the query formats. An unreadable one
// is the zero time rather than an error, matching the vault parser: one absent
// lifecycle date does not invalidate a competence.
func parseFactsTime(s string) (t time.Time) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(factsTimeLayout, s)
	if err != nil {
		return time.Time{}
	}
	if parsed.Unix() == 0 {
		return time.Time{}
	}
	return parsed
}

func readRelations(ctx context.Context, q QuerierI, table string, slugById map[uint64]string) (rels []capmapcorpus.Relation, err error) {
	rows, err := queryJSON[relationRow](ctx, q, table, relationSQL(table))
	if err != nil {
		return nil, eh.Errorf("unable to read relations from %s: %w", table, err)
	}
	rels = make([]capmapcorpus.Relation, 0, len(rows))
	for _, r := range rows {
		sourceId, pErr := strconv.ParseUint(r.SourceId, 10, 64)
		if pErr != nil {
			return nil, eh.Errorf("capmapfacts: relation source id %q is not a number: %w", r.SourceId, pErr)
		}
		source, known := slugById[sourceId]
		if !known {
			// The source competence is not in the table. Skipping would make
			// the dump quietly lossy in exactly the case worth knowing about —
			// a partially-ingested corpus.
			return nil, eh.Errorf("capmapfacts: relation to %q has source id %d, which no competence in %s carries",
				r.Target, sourceId, table)
		}
		rels = append(rels, capmapcorpus.Relation{
			SourceSlug: source,
			Target:     r.Target,
			Kind:       capmapcorpus.RelationKindE(r.Kind),
			Resolution: capmapcorpus.ParseResolution(r.Resolution),
			Section:    r.Section,
			Ncd:        r.Ncd,
		})
	}
	return rels, nil
}

// queryJSON prepares the query, runs it, and decodes its JSONEachRow body.
//
// Preparation happens here rather than in the builders because it is the one
// place both queries pass through, and before the FORMAT clause is appended
// because the passes parse what they are given and FORMAT is not part of the
// statement grammar they read.
func queryJSON[T any](ctx context.Context, q QuerierI, table string, sql string) (rows []T, err error) {
	resolved, err := prepare(sql, table)
	if err != nil {
		return nil, err
	}
	body, err := q.Query(ctx, resolved+"\nFORMAT JSONEachRow")
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	dec := json.NewDecoder(body)
	for {
		var row T
		if dErr := dec.Decode(&row); dErr != nil {
			if dErr == io.EOF {
				break
			}
			return nil, eh.Errorf("unable to decode a result row: %w", dErr)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// membId is the uint64 a membership is written under.
func membId(m registry.RegisteredNaturalKey) (v uint64) {
	return m.GetId().Value()
}

// How this file reads an attribute — all of it through the SQL read surface.
//
// **`LW_GET` locates one.** The call names a section and a membership id, and
// the expansion pass resolves the lanes and emits the read-back expression
// (ADR-0181 §SD3). Every scalar this corpus stores — a slug, a name, a level, a
// relation's endpoints — reads that way.
//
// **`LW_SEL` selects several.** A membership carried by more than one attribute
// of a row is the plural question, and this encoding asks it three times: a tag
// each, a section each, a lifecycle entry each. `LW_SEL_ATTRS` returns the
// attribute indices the membership occupies and `LW_SEL` the membership-lane
// positions, co-indexed with each other, so the value lane and the parameter
// lane project through them and stay aligned.
//
// The mixed channel is what carries the section heading and the lifecycle phase
// (§SD5), and it is nameable: `chan:low-card-ref-high-card-params`. That closed
// the last reason this file had to compute anything itself — the version before
// this one filtered the identity lane by hand, which is exactly the arithmetic
// the expansion now emits on its behalf.

// mixedChannel is the channel the section headings and lifecycle phases ride:
// one membership shared by several attributes, told apart by a parameter.
const mixedChannel = "chan:low-card-ref-high-card-params"

// plainChannel is the ordinary one-membership-per-attribute channel. Every
// section carries several channels, so naming one is not optional.
const plainChannel = "chan:low-card-ref"

// getScalar reads the attribute carrying memb on a scalar section.
func getScalar(section string, memb uint64) (expr string) {
	return fmt.Sprintf("LW_GET('%s', '%d', '%s')", section, memb, plainChannel)
}

// getListFirst reads the first value of the attribute carrying memb on an
// array-valued section.
//
// Every attribute this encoding writes on those sections holds exactly one
// value, so the first is the only one — and asking for the first is the honest
// spelling of that, where indexing the run's end would quietly return a
// different element the day the assumption stops holding.
func getListFirst(section string, memb uint64) (expr string) {
	return fmt.Sprintf("arrayElement(LW_GET_LIST('%s', '%d', '%s'), 1)", section, memb, plainChannel)
}

// selValues reads every attribute carrying memb on a scalar section — the tag
// list, and the lifecycle names on the mixed channel.
func selValues(section string, memb uint64, channel string, valueLane string) (expr string) {
	return fmt.Sprintf("LW_CO_GATHER(%s, LW_SEL_ATTRS('%s', '%d', '%s'))", valueLane, section, memb, channel)
}

// selArrayValues is selValues for an array-valued section: each selected
// attribute's single value, in write order — the prose sections, the lifecycle
// dates.
//
// The gather goes through LW_RAGGED_ELEM rather than the value lane directly,
// because on those sections the lane is flattened across attributes: an
// attribute index is not a value index, and pairing the two reads the wrong
// rows without raising anything.
func selArrayValues(section string, memb uint64, channel string, valueLane string, lenLane string) (expr string) {
	return fmt.Sprintf("arrayMap(a -> LW_RAGGED_ELEM(%s, %s, a, 1), LW_SEL_ATTRS('%s', '%d', '%s'))",
		valueLane, lenLane, section, memb, channel)
}

// selParameters yields the mixed-membership parameters of memb — the section
// heading, the lifecycle phase.
//
// It selects with LW_SEL, not LW_SEL_ATTRS: the parameter lane is co-indexed
// with the membership lane, not with the attributes, and the two differ exactly
// when an attribute carries more than one membership.
func selParameters(section string, memb uint64, paramLane string) (expr string) {
	return fmt.Sprintf("LW_CO_GATHER(%s, LW_SEL('%s', '%d', '%s'))", paramLane, section, memb, mixedChannel)
}

// newestPerId is the tail every read shares: a re-ingest restates entities
// under the same ids, so without it a dump would emit each competence once per
// ingest.
func newestPerId() (clause string) {
	return fmt.Sprintf("ORDER BY %s DESC LIMIT 1 BY %s", hTs, hId)
}

func competenceSQL(table string) (sql string) {
	return fmt.Sprintf(`SELECT
  %s AS slug,
  %s AS name,
  %s AS abbrev,
  %s AS synopsis,
  %s AS domain,
  %s AS catalog,
  %s AS owner,
  %s AS vault_path,
  %s AS level,
  %s AS maturity,
  %s AS pain,
  %s AS tags,
  %s AS section_headings,
  %s AS section_texts,
  %s AS lifecycle_by_phases,
  %s AS lifecycle_by,
  %s AS lifecycle_at_phases,
  arrayMap(v -> formatDateTime(v, '%%Y-%%m-%%d %%H:%%i:%%S', 'UTC'), %s) AS lifecycle_at
FROM %s
WHERE has(%s, %d)
%s`,
		getScalar("symbol", membId(capmapvocab.MembCompSlug)),
		getListFirst("stringArray", membId(capmapvocab.MembCompName)),
		getListFirst("stringArray", membId(capmapvocab.MembCompAbbrev)),
		getListFirst("stringArray", membId(capmapvocab.MembCompSynopsis)),
		getScalar("symbol", membId(capmapvocab.MembCompDomain)),
		getScalar("symbol", membId(capmapvocab.MembCompCatalog)),
		getScalar("symbol", membId(capmapvocab.MembCompOwner)),
		getListFirst("stringArray", membId(capmapvocab.MembCompVaultPath)),
		getListFirst("u8Array", membId(capmapvocab.MembCompLevel)),
		getListFirst("u8Array", membId(capmapvocab.MembCompMaturity)),
		getListFirst("u8Array", membId(capmapvocab.MembCompPain)),
		selValues("symbol", membId(capmapvocab.MembCompTag), plainChannel, hSymValue),
		selParameters("textArray", membId(capmapvocab.MembCompSection), hTxtMrhp),
		selArrayValues("textArray", membId(capmapvocab.MembCompSection), mixedChannel, hTxtValue, hTxtLen),
		selParameters("symbol", membId(capmapvocab.MembCompLifecycleBy), hSymMrhp),
		selValues("symbol", membId(capmapvocab.MembCompLifecycleBy), mixedChannel, hSymValue),
		selParameters("timeArray", membId(capmapvocab.MembCompLifecycleAt), hTimeMrhp),
		selArrayValues("timeArray", membId(capmapvocab.MembCompLifecycleAt), mixedChannel, hTimeValue, hTimeLen),
		table,
		hSymLr, membId(capmapvocab.MembKindCompetence),
		newestPerId())
}

func relationSQL(table string) (sql string) {
	return fmt.Sprintf(`SELECT
  toString(%s) AS source_id,
  toString(%s) AS target_id,
  %s AS target,
  %s AS kind,
  %s AS resolution,
  %s AS section,
  %s AS ncd
FROM %s
WHERE has(%s, %d)
%s`,
		getScalar("foreignKey", membId(capmapvocab.MembRelSource)),
		getScalar("foreignKey", membId(capmapvocab.MembRelTarget)),
		getScalar("symbol", membId(capmapvocab.MembRelTargetText)),
		getScalar("symbol", membId(capmapvocab.MembRelKind)),
		getScalar("symbol", membId(capmapvocab.MembRelResolution)),
		getScalar("symbol", membId(capmapvocab.MembRelSection)),
		getListFirst("f64Array", membId(capmapvocab.MembRelNcd)),
		table,
		hSymLr, membId(capmapvocab.MembKindRelation),
		newestPerId())
}
