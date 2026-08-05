package capmapcorpus

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// capabilityFileName is the file a directory-backed capability is defined in —
// analogous to index.html. Its slug is the enclosing directory's name, not
// "capability".
const capabilityFileName = "capability.md"

// h1Re matches an h1 heading, which is what delimits a body section.
var h1Re = regexp.MustCompile(`(?m)^#\s+(.+)$`)

// wikilinkRe matches an Obsidian wikilink, with or without display text:
// [[slug]] or [[slug|shown]]. The alternation excludes brackets and the pipe
// from the target so a malformed link fails to match rather than swallowing
// the rest of the line.
var wikilinkRe = regexp.MustCompile(`\[\[([^\[\]|]+)(?:\|[^\[\]]+)?\]\]`)

// frontmatter mirrors the YAML stanza. It is a parse-only shape: every field
// is copied into [Capability] or turned into a [Relation], and nothing outside
// this file sees it.
type frontmatter struct {
	Name        string          `yaml:"name"`
	Abbrev      string          `yaml:"abbrev"`
	Synopsis    string          `yaml:"synopsis"`
	Level       uint8           `yaml:"level"`
	ParentSlugs []string        `yaml:"parent_ids"`
	Domain      string          `yaml:"domain"`
	Catalog     string          `yaml:"catalog"`
	Maturity    *uint8          `yaml:"maturity"`
	Pain        *uint8          `yaml:"pain"`
	Owner       string          `yaml:"owner"`
	Similar     []similarEntry  `yaml:"similar"`
	Lifecycle   lifecycleFields `yaml:",inline"`
}

// similarEntry is one scored resemblance in frontmatter.
type similarEntry struct {
	Ref string  `yaml:"ref"`
	Ncd float64 `yaml:"ncd"`
}

// lifecycleFields carries the eight who/when pairs. They are flat keys in the
// vault rather than a nested map, so they are spelled out; [lifecycleEvents]
// folds them back into the ordered slice the model exposes.
type lifecycleFields struct {
	IdentifiedBy  string `yaml:"lifecycle_identified_by"`
	IdentifiedAt  string `yaml:"lifecycle_identified_at"`
	DefinedBy     string `yaml:"lifecycle_defined_by"`
	DefinedAt     string `yaml:"lifecycle_defined_at"`
	AssessedBy    string `yaml:"lifecycle_assessed_by"`
	AssessedAt    string `yaml:"lifecycle_assessed_at"`
	PlannedBy     string `yaml:"lifecycle_planned_by"`
	PlannedAt     string `yaml:"lifecycle_planned_at"`
	BuildingBy    string `yaml:"lifecycle_building_by"`
	BuildingAt    string `yaml:"lifecycle_building_at"`
	OperationalBy string `yaml:"lifecycle_operational_by"`
	OperationalAt string `yaml:"lifecycle_operational_at"`
	OptimizingBy  string `yaml:"lifecycle_optimizing_by"`
	OptimizingAt  string `yaml:"lifecycle_optimizing_at"`
	RetiringBy    string `yaml:"lifecycle_retiring_by"`
	RetiringAt    string `yaml:"lifecycle_retiring_at"`
}

// ParseDir reads a vault directory whole: its capabilities sorted by slug,
// every relation they declare resolved against the set that was found, and the
// markdown files that were not capabilities.
//
// Sorting is not cosmetic: filesystem walk order is not stable across machines,
// and callers downstream diff and ingest these. Relations follow their source.
//
// Two failure modes are treated differently on purpose. A file whose *name*
// cannot be a capability slug is skipped and reported in [Corpus.Skipped] —
// vaults routinely hold reference notes beside capabilities, and refusing the
// whole read over one would make the common layout unreadable. A file that
// *is* addressable but whose content will not parse fails the call, because
// that is an authoring error to fix and dropping it would understate the
// corpus with no signal.
func ParseDir(vaultDir string) (corpus Corpus, err error) {
	type parsedFile struct {
		path string
		slug string
		cap  Capability
		rels []Relation
	}

	// Collect first, parse second: the directory-backed slug set has to be
	// known before any wikilink can be resolved, and that is only complete
	// once the whole tree has been walked.
	var files []parsedFile
	dirBacked := make(map[string]struct{}, 64)
	err = filepath.WalkDir(vaultDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// .obsidian is the editor's own state — workspace layout, graph
			// settings — and contains no capabilities.
			if d.Name() == ".obsidian" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		slug, slugErr := slugForPath(path)
		if slugErr != nil {
			corpus.Skipped = append(corpus.Skipped, SkippedFile{
				Path:   relativeTo(vaultDir, path),
				Reason: "name is not a capability slug",
			})
			return nil
		}
		if d.Name() == capabilityFileName {
			dirBacked[slug] = struct{}{}
		}
		files = append(files, parsedFile{path: path, slug: slug})
		return nil
	})
	if err != nil {
		return Corpus{}, eh.Errorf("unable to walk vault %q: %w", vaultDir, err)
	}

	known := make(map[string]struct{}, len(files))
	for i := range files {
		if _, dup := known[files[i].slug]; dup {
			return Corpus{}, eh.Errorf("duplicate capability slug %q at %s: slugs are the corpus's identity and must be unique",
				files[i].slug, files[i].path)
		}
		known[files[i].slug] = struct{}{}
	}

	for i := range files {
		files[i].cap, files[i].rels, err = parseFile(files[i].path, files[i].slug, vaultDir)
		if err != nil {
			return Corpus{}, err
		}
	}

	sort.Slice(files, func(a, b int) bool { return files[a].slug < files[b].slug })

	corpus.Capabilities = make([]Capability, 0, len(files))
	nrels := 0
	for i := range files {
		nrels += len(files[i].rels)
	}
	corpus.Relations = make([]Relation, 0, nrels)
	for i := range files {
		corpus.Capabilities = append(corpus.Capabilities, files[i].cap)
		corpus.Relations = append(corpus.Relations, files[i].rels...)
	}
	resolveRelations(corpus.Relations, known, dirBacked)
	sort.Slice(corpus.Skipped, func(a, b int) bool { return corpus.Skipped[a].Path < corpus.Skipped[b].Path })
	return corpus, nil
}

// relativeTo renders path against the vault root, falling back to the absolute
// path when it cannot be expressed that way.
func relativeTo(root, path string) (rel string) {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}

// parseFile reads one markdown file into a capability and the relations it
// declares. The relations come back unresolved; ParseDir resolves them once
// the whole slug set is known.
func parseFile(path string, slug string, vaultDir string) (cap Capability, rels []Relation, err error) {
	var data []byte
	if data, err = os.ReadFile(path); err != nil {
		return cap, nil, eh.Errorf("unable to read %q: %w", path, err)
	}
	var (
		fm   frontmatter
		body string
	)
	if fm, body, err = splitFrontmatter(string(data)); err != nil {
		return cap, nil, eh.Errorf("unable to parse %q: %w", path, err)
	}

	relPath, relErr := filepath.Rel(vaultDir, path)
	if relErr != nil {
		relPath = path
	}

	cap = Capability{
		Slug:       slug,
		NaturalKey: NaturalKey(slug),
		VaultPath:  relPath,
		Name:       fm.Name,
		Abbrev:     fm.Abbrev,
		Synopsis:   fm.Synopsis,
		Owner:      fm.Owner,
		Level:      fm.Level,
		Maturity:   scoreOrNotAssessed(fm.Maturity),
		Pain:       scoreOrNotAssessed(fm.Pain),
		Sections:   parseSections(body),
		Lifecycle:  lifecycleEvents(fm.Lifecycle),
	}
	// A domain is a grouping label, not an identity, so an unconventional one
	// is kept as written rather than refused.
	cap.Domain, _ = NormalizeTarget(fm.Domain)
	cap.Catalog = fm.Catalog
	if cap.Catalog == "" {
		// The catalog a capability belongs to is otherwise its top-level
		// directory, which is how the vault groups imported frameworks.
		if top, _, found := strings.Cut(relPath, string(os.PathSeparator)); found {
			cap.Catalog = top
		}
	}

	rels = make([]Relation, 0, len(fm.ParentSlugs)+len(fm.Similar))
	for _, raw := range fm.ParentSlugs {
		if rel, ok := makeRelation(slug, stripWikilink(raw), RelationKindParent, ""); ok {
			rels = append(rels, rel)
		}
	}
	for _, s := range fm.Similar {
		if rel, ok := makeRelation(slug, stripWikilink(s.Ref), RelationKindSimilar, ""); ok {
			rel.Ncd = s.Ncd
			rels = append(rels, rel)
		}
	}
	rels = append(rels, sectionWikilinks(slug, cap.Sections)...)
	return cap, rels, nil
}

// makeRelation builds one relation from a raw link target, pre-marking the
// targets that cannot name a capability. ok is false only for an empty target,
// which carries nothing to record.
//
// A not-well-formed target is kept rather than dropped, with
// [ResolutionExternal] set here so the later resolve pass leaves it alone. It
// is the citation case: dropping it would silently discard what the Standards
// and Obligations sections exist to record, and resolving it would report a
// quarter of this corpus's links as broken.
func makeRelation(source, rawTarget string, kind RelationKindE, section string) (rel Relation, ok bool) {
	trimmed, qualified := trimCapabilitySuffix(rawTarget)
	target, wellFormed := NormalizeTarget(trimmed)
	if target == "" {
		return rel, false
	}
	rel = Relation{SourceSlug: source, Target: target, Kind: kind, Section: section, qualified: qualified}
	if !wellFormed {
		rel.Resolution = ResolutionExternal
	}
	return rel, true
}

// capabilitySuffix is how a link addresses a directory-backed capability the
// way Obsidian resolves it: the file is `{slug}/capability.md`, so the link
// that actually follows is `[[{slug}/capability]]`.
const capabilitySuffix = "/" + "capability"

// trimCapabilitySuffix reduces an explicitly-qualified link to the slug it
// names, reporting whether it was written that way.
//
// Both spellings denote the same capability, so both must resolve to one
// target — otherwise a corpus that has been normalised to the qualified form
// reports every one of those links as broken. The flag is kept because the two
// are not equally correct: the bare form dangles in Obsidian and the qualified
// form does not, which is the distinction [ResolutionDirRef] reports.
func trimCapabilitySuffix(raw string) (out string, qualified bool) {
	trimmed := strings.TrimSpace(raw)
	if base, found := strings.CutSuffix(trimmed, capabilitySuffix); found && base != "" {
		return base, true
	}
	return trimmed, false
}

// sectionWikilinks extracts every [[link]] from the body, tagged with the
// section it was found under. The section is the point: it is what makes "what
// does this capability's Standards section cite" a query rather than a scan.
func sectionWikilinks(slug string, sections []Section) (rels []Relation) {
	for _, sec := range sections {
		for _, m := range wikilinkRe.FindAllStringSubmatch(sec.Text, -1) {
			if rel, ok := makeRelation(slug, m[1], RelationKindWikilink, sec.Heading); ok {
				rels = append(rels, rel)
			}
		}
	}
	return rels
}

// resolveRelations marks each relation against the slugs the corpus actually
// holds, in place.
//
// A wikilink whose target is directory-backed resolves as
// [ResolutionDirRef] rather than [ResolutionDirect]: the capability exists
// here, but Obsidian looks for `{slug}.md` and will not follow it. The
// frontmatter kinds are exempt — they are resolved by this model only and are
// never followed by the editor, so the distinction would be noise.
func resolveRelations(rels []Relation, known, dirBacked map[string]struct{}) {
	for i := range rels {
		// Already classified at parse time as naming something outside the
		// corpus; no lookup can change that, since no capability could carry
		// such a slug.
		if rels[i].Resolution == ResolutionExternal {
			continue
		}
		if _, ok := known[rels[i].Target]; !ok {
			rels[i].Resolution = ResolutionUnresolved
			continue
		}
		// A bare [[slug]] naming a directory-backed capability dangles in
		// Obsidian, which looks for {slug}.md. A link already written as
		// [[slug/capability]] does not, so it is not flagged.
		if _, isDir := dirBacked[rels[i].Target]; isDir && rels[i].Kind == RelationKindWikilink && !rels[i].qualified {
			rels[i].Resolution = ResolutionDirRef
			continue
		}
		rels[i].Resolution = ResolutionDirect
	}
}

// UnresolvedRelations returns the relations whose target is not in the corpus
// — the broken links. This is what the vault tooling used to precompute into
// per-capability lint columns; as a filter over one grain it needs no pass and
// cannot go stale.
func UnresolvedRelations(rels []Relation) (broken []Relation) {
	for _, r := range rels {
		if r.Resolution == ResolutionUnresolved {
			broken = append(broken, r)
		}
	}
	return broken
}

// splitFrontmatter separates the YAML stanza from the markdown body. A file
// with no stanza is an error rather than a body-only capability: every field
// that identifies a capability lives in the frontmatter.
func splitFrontmatter(content string) (fm frontmatter, body string, err error) {
	const open = "---\n"
	if !strings.HasPrefix(content, open) {
		return fm, "", eh.Errorf("no opening frontmatter delimiter")
	}
	stanza, remainder, found := strings.Cut(content[len(open):], "\n---")
	if !found {
		return fm, "", eh.Errorf("no closing frontmatter delimiter")
	}
	if err = yaml.Unmarshal([]byte(stanza), &fm); err != nil {
		return fm, "", eh.Errorf("unable to unmarshal frontmatter: %w", err)
	}
	body = strings.TrimPrefix(remainder, "\n")
	return fm, body, nil
}

// parseSections splits the body on h1 headings, in document order. Text before
// the first heading is dropped — the vault's shape puts nothing there, and
// keeping it would need a heading to name it by.
func parseSections(body string) (sections []Section) {
	locs := h1Re.FindAllStringIndex(body, -1)
	names := h1Re.FindAllStringSubmatch(body, -1)
	if len(locs) == 0 {
		return nil
	}
	sections = make([]Section, 0, len(locs))
	for i, loc := range locs {
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		sections = append(sections, Section{
			Heading: strings.TrimSpace(names[i][1]),
			Text:    strings.TrimSpace(body[loc[1]:end]),
		})
	}
	return sections
}

// lifecycleEvents folds the flat who/when frontmatter keys into ordered
// events, keeping only the phases that carry something. A phase with neither a
// name nor a parsable date is absent rather than zero-valued, so a reader
// cannot mistake "not recorded" for "recorded as empty".
func lifecycleEvents(f lifecycleFields) (events []LifecycleEvent) {
	pairs := []struct {
		phase PhaseE
		by    string
		at    string
	}{
		{PhaseIdentified, f.IdentifiedBy, f.IdentifiedAt},
		{PhaseDefined, f.DefinedBy, f.DefinedAt},
		{PhaseAssessed, f.AssessedBy, f.AssessedAt},
		{PhasePlanned, f.PlannedBy, f.PlannedAt},
		{PhaseBuilding, f.BuildingBy, f.BuildingAt},
		{PhaseOperational, f.OperationalBy, f.OperationalAt},
		{PhaseOptimizing, f.OptimizingBy, f.OptimizingAt},
		{PhaseRetiring, f.RetiringBy, f.RetiringAt},
	}
	for _, p := range pairs {
		at := parseTimestamp(p.at)
		if p.by == "" && at.IsZero() {
			continue
		}
		events = append(events, LifecycleEvent{Phase: p.phase, By: p.by, At: at})
	}
	return events
}

// timestampLayouts are accepted for a lifecycle date, most specific first. The
// vault is hand-authored, so a bare date is the common case and a full
// timestamp the exception.
var timestampLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// parseTimestamp yields the zero time for anything it cannot read, including
// the epoch sentinel a previous exporter wrote for "unset". An unreadable date
// is not an error: it is one absent lifecycle event in an otherwise usable
// capability.
func parseTimestamp(s string) (t time.Time) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range timestampLayouts {
		if parsed, err := time.Parse(layout, s); err == nil {
			if parsed.Unix() == 0 {
				return time.Time{}
			}
			return parsed
		}
	}
	return time.Time{}
}

// scoreOrNotAssessed distinguishes an absent frontmatter key from an explicit
// 0. Maturity 0 is "ad-hoc", a real judgement; absent is not.
func scoreOrNotAssessed(v *uint8) (score uint8) {
	if v == nil {
		return NotAssessed
	}
	return *v
}

// stripWikilink unwraps the [[...]] the vault uses for frontmatter references.
// A plain slug passes through unchanged, so both spellings are accepted.
func stripWikilink(s string) (out string) {
	out = strings.TrimSpace(s)
	out = strings.TrimPrefix(out, "[[")
	out = strings.TrimSuffix(out, "]]")
	return strings.TrimSpace(out)
}
