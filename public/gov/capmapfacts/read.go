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
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
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

// queryJSON resolves the query's column handles, runs it, and decodes its
// JSONEachRow body.
//
// Resolution happens here rather than in the builders because it is the one
// place both queries pass through, and it happens before the FORMAT clause is
// appended because the pass parses what it is given and FORMAT is not part of
// the statement grammar it reads.
func queryJSON[T any](ctx context.Context, q QuerierI, table string, sql string) (rows []T, err error) {
	resolved, err := resolveHandles(sql, table)
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

// attrsFor yields the indices of the attributes carrying memb, in write order.
//
// It goes through the cardinality column rather than using the membership's
// position directly, because `lr` is every attribute's memberships
// concatenated: position and attribute index agree only while every earlier
// attribute contributed exactly one. That happens to hold for what [Ingest]
// writes today, and would stop holding the day an attribute with a mixed
// membership is written before a plain one.
func attrsFor(membCol, cardCol string, memb uint64) (expr string) {
	return fmt.Sprintf(
		"arrayMap(p -> arrayFirstIndex(x -> x >= p, arrayCumSum(%s)), arrayFilter((i, m) -> m = %d, arrayEnumerate(%s), %s))",
		cardCol, memb, membCol, membCol)
}

// positionsFor yields the positions of memb within the flattened membership
// column — which is what indexes the mixed-membership parameter (the heading,
// the lifecycle phase), since that column is flattened the same way.
func positionsFor(membCol string, memb uint64) (expr string) {
	return fmt.Sprintf("arrayFilter((i, m) -> m = %d, arrayEnumerate(%s), %s)", memb, membCol, membCol)
}

// pickScalar reads the one value of a scalar section's attribute carrying memb,
// or zero when the row has none.
//
// The index is clamped to 1 before it reaches arrayElement: the guard and the
// pick are both evaluated (ClickHouse's `if` is not lazy over columns), and a
// zero index is the one arrayElement refuses.
func pickScalar(valCol, membCol, cardCol string, memb uint64, zero string) (expr string) {
	attrs := attrsFor(membCol, cardCol, memb)
	return fmt.Sprintf("if(empty(%[1]s), %[2]s, arrayElement(%[3]s, greatest(%[1]s[1], 1)))", attrs, zero, valCol)
}

// pickArrayValue is pickScalar for an array-valued section, where the value
// column is flattened by `len` and an attribute's values start where the
// previous attribute's ended. Every attribute this encoding writes holds
// exactly one value, so the attribute's last value is its only one.
func pickArrayValue(valCol, lenCol, membCol, cardCol string, memb uint64, zero string) (expr string) {
	attrs := attrsFor(membCol, cardCol, memb)
	return fmt.Sprintf("if(empty(%[1]s), %[2]s, arrayElement(%[3]s, greatest(arrayElement(arrayCumSum(%[4]s), greatest(%[1]s[1], 1)), 1)))",
		attrs, zero, valCol, lenCol)
}

// pickArrayValues is pickArrayValue for every attribute carrying memb, in write
// order — tags, prose sections, lifecycle entries.
func pickArrayValues(valCol, lenCol, membCol, cardCol string, memb uint64) (expr string) {
	attrs := attrsFor(membCol, cardCol, memb)
	return fmt.Sprintf("arrayMap(a -> arrayElement(%s, greatest(arrayElement(arrayCumSum(%s), a), 1)), %s)", valCol, lenCol, attrs)
}

// pickScalars is pickScalar for every attribute carrying memb — the tag list.
func pickScalars(valCol, membCol, cardCol string, memb uint64) (expr string) {
	return fmt.Sprintf("arrayMap(a -> arrayElement(%s, a), %s)", valCol, attrsFor(membCol, cardCol, memb))
}

// pickParameters yields the mixed-membership parameters of memb — the section
// heading, the lifecycle phase — decoded from bytes to text.
func pickParameters(paramCol, membCol string, memb uint64) (expr string) {
	return fmt.Sprintf("arrayMap(p -> toString(arrayElement(%s, p)), %s)", paramCol, positionsFor(membCol, memb))
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
		pickScalar(hSymValue, hSymLr, hSymLrCard, membId(capmapvocab.MembCompSlug), "''"),
		pickArrayValue(hStrValue, hStrLen, hStrLr, hStrLrCard, membId(capmapvocab.MembCompName), "''"),
		pickArrayValue(hStrValue, hStrLen, hStrLr, hStrLrCard, membId(capmapvocab.MembCompAbbrev), "''"),
		pickArrayValue(hStrValue, hStrLen, hStrLr, hStrLrCard, membId(capmapvocab.MembCompSynopsis), "''"),
		pickScalar(hSymValue, hSymLr, hSymLrCard, membId(capmapvocab.MembCompDomain), "''"),
		pickScalar(hSymValue, hSymLr, hSymLrCard, membId(capmapvocab.MembCompCatalog), "''"),
		pickScalar(hSymValue, hSymLr, hSymLrCard, membId(capmapvocab.MembCompOwner), "''"),
		pickArrayValue(hStrValue, hStrLen, hStrLr, hStrLrCard, membId(capmapvocab.MembCompVaultPath), "''"),
		pickArrayValue(hU8Value, hU8Len, hU8Lr, hU8LrCard, membId(capmapvocab.MembCompLevel), "0"),
		pickArrayValue(hU8Value, hU8Len, hU8Lr, hU8LrCard, membId(capmapvocab.MembCompMaturity), "0"),
		pickArrayValue(hU8Value, hU8Len, hU8Lr, hU8LrCard, membId(capmapvocab.MembCompPain), "0"),
		pickScalars(hSymValue, hSymLr, hSymLrCard, membId(capmapvocab.MembCompTag)),
		pickParameters(hTxtMrhp, hTxtLmr, membId(capmapvocab.MembCompSection)),
		pickArrayValues(hTxtValue, hTxtLen, hTxtLmr, hTxtLmrCard, membId(capmapvocab.MembCompSection)),
		pickParameters(hSymMrhp, hSymLmr, membId(capmapvocab.MembCompLifecycleBy)),
		pickScalars(hSymValue, hSymLmr, hSymLmrCard, membId(capmapvocab.MembCompLifecycleBy)),
		pickParameters(hTimeMrhp, hTimeLmr, membId(capmapvocab.MembCompLifecycleAt)),
		pickArrayValues(hTimeValue, hTimeLen, hTimeLmr, hTimeLmrCard, membId(capmapvocab.MembCompLifecycleAt)),
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
		pickScalar(hFkValue, hFkLr, hFkLrCard, membId(capmapvocab.MembRelSource), "0"),
		pickScalar(hFkValue, hFkLr, hFkLrCard, membId(capmapvocab.MembRelTarget), "0"),
		pickScalar(hSymValue, hSymLr, hSymLrCard, membId(capmapvocab.MembRelTargetText), "''"),
		pickScalar(hSymValue, hSymLr, hSymLrCard, membId(capmapvocab.MembRelKind), "''"),
		pickScalar(hSymValue, hSymLr, hSymLrCard, membId(capmapvocab.MembRelResolution), "''"),
		pickScalar(hSymValue, hSymLr, hSymLrCard, membId(capmapvocab.MembRelSection), "''"),
		pickArrayValue(hF64Value, hF64Len, hF64Lr, hF64LrCard, membId(capmapvocab.MembRelNcd), "0"),
		table,
		hSymLr, membId(capmapvocab.MembKindRelation),
		newestPerId())
}
