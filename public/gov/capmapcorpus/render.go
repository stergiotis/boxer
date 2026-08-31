package capmapcorpus

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Rendering a corpus back to a vault — the inverse of [ParseDir], and what
// `boxer capmap dump` writes after reading the facts table.
//
// The vault is authoritative (ADR-0168 §SD3), so this is not a second source of
// truth: it is how a corpus that has been through the store gets back into the
// form a person edits and git diffs. Two properties make that safe to rely on.
//
// **What it writes, [ParseDir] reads back the same.** The pair is pinned by a
// parse → render → parse round-trip over a fixture vault, which is the test the
// ADR's verification plan named and the tree did not have until this existed.
//
// **It renders the vault's dialect, not boxer's.** Files are `capability.md`,
// links are `[[slug]]`, and the frontmatter keys are the ones the tooling this
// corpus was ported from wrote. See the package doc for why the two vocabularies
// differ.
//
// What is deliberately *not* written: the numeric `id` the prototype's exporter
// stamped into frontmatter. Ids here are derived from the slug
// ([NaturalKey]), so a stored one could only ever agree or be wrong.

// renderFrontmatter is the YAML stanza on the way out. Field order is emission
// order, which is why the lifecycle keys are spelled out rather than carried in
// an inline map: a map would sort them alphabetically and interleave the eight
// phases, and the vault writes them in lifecycle order.
//
// It is a separate shape from the parse-side `frontmatter` on purpose. That one
// is permissive — it accepts several spellings of a tag list, both wikilink
// forms, three date layouts — and this one has to pick exactly one of each.
type renderFrontmatter struct {
	Name     string `yaml:"name"`
	Abbrev   string `yaml:"abbrev,omitempty"`
	Synopsis string `yaml:"synopsis,omitempty"`
	Level    uint8  `yaml:"level"`
	// Always emitted, empty included: `parent_ids: []` is how the vault says
	// "this is a root", and an absent key would read as "not stated".
	ParentIds []string `yaml:"parent_ids"`
	Domain    string   `yaml:"domain,omitempty"`
	Catalog   string   `yaml:"catalog,omitempty"`
	Owner     string   `yaml:"owner,omitempty"`
	// Scores are always emitted, sentinel included, matching both the vault's
	// convention and the encoding's (ADR-0168 §SD1): "not assessed" is a value.
	Maturity      uint8           `yaml:"maturity"`
	Pain          uint8           `yaml:"pain"`
	Tags          []string        `yaml:"tags,omitempty"`
	Similar       []renderSimilar `yaml:"similar,omitempty"`
	IdentifiedBy  string          `yaml:"lifecycle_identified_by,omitempty"`
	IdentifiedAt  string          `yaml:"lifecycle_identified_at,omitempty"`
	DefinedBy     string          `yaml:"lifecycle_defined_by,omitempty"`
	DefinedAt     string          `yaml:"lifecycle_defined_at,omitempty"`
	AssessedBy    string          `yaml:"lifecycle_assessed_by,omitempty"`
	AssessedAt    string          `yaml:"lifecycle_assessed_at,omitempty"`
	PlannedBy     string          `yaml:"lifecycle_planned_by,omitempty"`
	PlannedAt     string          `yaml:"lifecycle_planned_at,omitempty"`
	BuildingBy    string          `yaml:"lifecycle_building_by,omitempty"`
	BuildingAt    string          `yaml:"lifecycle_building_at,omitempty"`
	OperationalBy string          `yaml:"lifecycle_operational_by,omitempty"`
	OperationalAt string          `yaml:"lifecycle_operational_at,omitempty"`
	OptimizingBy  string          `yaml:"lifecycle_optimizing_by,omitempty"`
	OptimizingAt  string          `yaml:"lifecycle_optimizing_at,omitempty"`
	RetiringBy    string          `yaml:"lifecycle_retiring_by,omitempty"`
	RetiringAt    string          `yaml:"lifecycle_retiring_at,omitempty"`
}

// renderSimilar is one scored resemblance in the emitted stanza.
type renderSimilar struct {
	Ref string  `yaml:"ref"`
	Ncd float64 `yaml:"ncd"`
}

// RenderCompetence renders one competence as the markdown of its vault file:
// the YAML stanza, then the body sections in document order.
//
// rels are the relations sourced at this competence — [WriteVault] groups them;
// a caller doing it itself may pass the whole corpus's slice, since anything
// with another source is ignored. Only the frontmatter kinds are emitted:
// a `wikilink` relation was read *out of* a body section, and the body is
// written back verbatim, so emitting it again would duplicate the link.
func RenderCompetence(comp Competence, rels []Relation) (out []byte, err error) {
	fm := renderFrontmatter{
		Name:      comp.Name,
		Abbrev:    comp.Abbrev,
		Synopsis:  comp.Synopsis,
		Level:     comp.Level,
		ParentIds: []string{},
		Domain:    comp.Domain,
		Catalog:   comp.Catalog,
		Owner:     comp.Owner,
		Maturity:  comp.Maturity,
		Pain:      comp.Pain,
		Tags:      comp.Tags,
	}
	for _, rel := range rels {
		if rel.SourceSlug != comp.Slug {
			continue
		}
		switch rel.Kind {
		case RelationKindParent:
			fm.ParentIds = append(fm.ParentIds, renderRef(rel))
		case RelationKindSimilar:
			fm.Similar = append(fm.Similar, renderSimilar{Ref: renderRef(rel), Ncd: rel.Ncd})
		}
	}
	applyLifecycle(&fm, comp.Lifecycle)

	stanza, mErr := yaml.Marshal(fm)
	if mErr != nil {
		return nil, eh.Errorf("unable to render frontmatter for %q: %w", comp.Slug, mErr)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(stanza)
	b.WriteString("---\n")
	for _, sec := range comp.Sections {
		b.WriteString("\n# ")
		b.WriteString(sec.Heading)
		b.WriteString("\n\n")
		b.WriteString(sec.Text)
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}

// applyLifecycle spreads the recorded events back over the flat frontmatter
// keys. A phase with no record leaves both of its keys absent, which is what
// keeps "not recorded" distinguishable from "recorded as empty".
func applyLifecycle(fm *renderFrontmatter, events []LifecycleEvent) {
	for _, ev := range events {
		by, at := ev.By, renderTimestamp(ev.At)
		switch ev.Phase {
		case PhaseIdentified:
			fm.IdentifiedBy, fm.IdentifiedAt = by, at
		case PhaseDefined:
			fm.DefinedBy, fm.DefinedAt = by, at
		case PhaseAssessed:
			fm.AssessedBy, fm.AssessedAt = by, at
		case PhasePlanned:
			fm.PlannedBy, fm.PlannedAt = by, at
		case PhaseBuilding:
			fm.BuildingBy, fm.BuildingAt = by, at
		case PhaseOperational:
			fm.OperationalBy, fm.OperationalAt = by, at
		case PhaseOptimizing:
			fm.OptimizingBy, fm.OptimizingAt = by, at
		case PhaseRetiring:
			fm.RetiringBy, fm.RetiringAt = by, at
		}
	}
}

// renderTimestamp writes a lifecycle date in the shortest layout [parseTimestamp]
// reads back exactly: a bare date when there is no time of day, which is what a
// hand-authored vault carries, and a full timestamp otherwise.
func renderTimestamp(t time.Time) (s string) {
	if t.IsZero() {
		return ""
	}
	u := t.UTC()
	if u.Hour() == 0 && u.Minute() == 0 && u.Second() == 0 && u.Nanosecond() == 0 {
		return u.Format("2006-01-02")
	}
	return u.Format("2006-01-02 15:04:05")
}

// renderRef writes a frontmatter reference back in the spelling it was read in.
//
// Both forms name the same competence, and for the frontmatter kinds neither is
// more correct — resolveRelations exempts them from the dirref rule, because no
// editor follows them. Keeping the spelling anyway is what makes a dump of an
// unedited vault a no-op diff rather than a rewrite of every parent link.
func renderRef(rel Relation) (s string) {
	target := rel.Target
	if rel.qualified {
		target += markerSuffix
	}
	return wikilink(target)
}

// wikilink wraps a target in the vault's link syntax.
func wikilink(target string) (s string) {
	return "[[" + target + "]]"
}

// WriteStats reports what a vault write produced.
type WriteStats struct {
	Files int
	// Directories counts the directory-backed competences — the ones with
	// children, written as `{slug}/capability.md`.
	Directories int
}

// WriteVault renders a whole corpus into dir, one file per competence.
//
// Placement follows [Competence.VaultPath] when the corpus carries one, so a
// vault that was read, stored and dumped comes back where it started. When it
// does not — a corpus assembled by hand, or one whose paths were lost — the
// layout is derived: a competence with children becomes a directory holding a
// `capability.md`, a leaf becomes `{slug}.md` beside its siblings, and both
// nest under the first parent.
//
// It writes and creates; it never deletes. A caller that wants a clean vault
// empties the directory itself, which keeps "regenerate" from being a verb that
// can lose an edit nobody had ingested yet.
func WriteVault(corpus Corpus, dir string) (stats WriteStats, err error) {
	if dir == "" {
		return stats, eh.Errorf("capmapcorpus: no output directory")
	}
	paths, err := vaultPaths(corpus)
	if err != nil {
		return stats, err
	}
	bySource := make(map[string][]Relation, len(corpus.Competences))
	for _, rel := range corpus.Relations {
		bySource[rel.SourceSlug] = append(bySource[rel.SourceSlug], rel)
	}
	for _, comp := range corpus.Competences {
		rel := paths[comp.Slug]
		content, rErr := RenderCompetence(comp, bySource[comp.Slug])
		if rErr != nil {
			return stats, rErr
		}
		full := filepath.Join(dir, rel)
		if mkErr := os.MkdirAll(filepath.Dir(full), 0o755); mkErr != nil {
			return stats, eh.Errorf("unable to create directory for %q: %w", rel, mkErr)
		}
		if wErr := os.WriteFile(full, content, 0o644); wErr != nil {
			return stats, eb.Build().Str("rel", rel).Errorf("unable to write: %w", wErr)
		}
		stats.Files++
		if filepath.Base(rel) == markerFileName {
			stats.Directories++
		}
	}
	return stats, nil
}

// vaultPaths decides where every competence goes, as a vault-relative path.
//
// Two competences landing on one path is an error rather than a last-writer
// win: it means the corpus carries two rows for one file, and silently dropping
// one would make the dump quietly lossy.
func vaultPaths(corpus Corpus) (paths map[string]string, err error) {
	comps := make(map[string]*Competence, len(corpus.Competences))
	for i := range corpus.Competences {
		comps[corpus.Competences[i].Slug] = &corpus.Competences[i]
	}
	parent := firstParents(corpus.Relations, comps)
	dirBacked := make(map[string]struct{}, len(parent))
	for _, p := range parent {
		dirBacked[p] = struct{}{}
	}

	paths = make(map[string]string, len(corpus.Competences))
	claimed := make(map[string]string, len(corpus.Competences))
	for _, comp := range corpus.Competences {
		rel := derivePath(comp.Slug, comps, parent, dirBacked, 0)
		if stored, ok := safeVaultPath(comp.VaultPath); ok {
			rel = stored
		}
		if prev, dup := claimed[rel]; dup {
			return nil, eh.Errorf("capmapcorpus: %q and %q both map to vault path %q", prev, comp.Slug, rel)
		}
		claimed[rel] = comp.Slug
		paths[comp.Slug] = rel
	}
	return paths, nil
}

// firstParents maps each competence to the parent it is filed under: the
// alphabetically first, matching what the treemap does with the same problem.
// A tree is one place per node, and multi-parenting is a fact about the graph
// rather than about the filesystem.
func firstParents(rels []Relation, comps map[string]*Competence) (parent map[string]string) {
	parent = make(map[string]string, len(rels))
	for _, rel := range rels {
		if rel.Kind != RelationKindParent {
			continue
		}
		if _, known := comps[rel.Target]; !known {
			continue
		}
		if cur, has := parent[rel.SourceSlug]; !has || rel.Target < cur {
			parent[rel.SourceSlug] = rel.Target
		}
	}
	return parent
}

// maxPathDepth bounds the ancestor walk. A hierarchy deeper than this is a
// cycle, and a cycle would otherwise recurse until the stack ran out.
const maxPathDepth = 8

// derivePath places a competence that carries no stored path: under its first
// parent's directory, as a directory of its own when it has children.
func derivePath(slug string, comps map[string]*Competence, parent map[string]string, dirBacked map[string]struct{}, depth int) (rel string) {
	base := slug + ".md"
	if _, isDir := dirBacked[slug]; isDir {
		base = filepath.Join(slug, markerFileName)
	}
	if p, has := parent[slug]; has && depth < maxPathDepth {
		return filepath.Join(filepath.Dir(derivePath(p, comps, parent, dirBacked, depth+1)), base)
	}
	// A root goes under its catalog, which is where ParseDir would read the
	// catalog back from when the frontmatter does not state one.
	if comp, has := comps[slug]; has && comp.Catalog != "" {
		return filepath.Join(comp.Catalog, base)
	}
	return base
}

// safeVaultPath accepts a stored path only if it stays inside the output
// directory and names a markdown file. A corpus read out of a store is data
// like any other, and `../../etc/passwd` in a path column must not become a
// write.
func safeVaultPath(raw string) (rel string, ok bool) {
	if raw == "" {
		return "", false
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) {
		return "", false
	}
	cleaned := filepath.Clean(filepath.FromSlash(raw))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	if strings.ToLower(filepath.Ext(cleaned)) != ".md" {
		return "", false
	}
	return cleaned, true
}

// SortCorpus puts a corpus in the order [ParseDir] produces: competences by
// slug, relations following their source. A corpus assembled from a query comes
// back in whatever order the rows arrived, and two corpora are only comparable
// once both are in this one.
func SortCorpus(corpus *Corpus) {
	sort.SliceStable(corpus.Competences, func(i, j int) bool {
		return corpus.Competences[i].Slug < corpus.Competences[j].Slug
	})
	sort.SliceStable(corpus.Relations, func(i, j int) bool {
		a, b := corpus.Relations[i], corpus.Relations[j]
		if a.SourceSlug != b.SourceSlug {
			return a.SourceSlug < b.SourceSlug
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Section != b.Section {
			return a.Section < b.Section
		}
		return a.Target < b.Target
	})
}
