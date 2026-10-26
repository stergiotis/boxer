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
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// markerFileName is the file a directory-backed competence is defined in —
// analogous to index.html. Its slug is the enclosing directory's name, not
// "capability".
//
// The value is the vault's word, not boxer's: the tree is authored in Obsidian
// against the business-capability literature, and renaming its files to match
// boxer's vocabulary would break every other vault written to the convention.
// The package doc has the rule.
const markerFileName = "capability.md"

// h1Re matches an h1 heading, which is what delimits a body section.
var h1Re = regexp.MustCompile(`(?m)^#\s+(.+)$`)

// wikilinkRe matches an Obsidian wikilink, with or without display text:
// [[slug]] or [[slug|shown]]. The alternation excludes brackets and the pipe
// from the target so a malformed link fails to match rather than swallowing
// the rest of the line.
var wikilinkRe = regexp.MustCompile(`\[\[([^\[\]|]+)(?:\|[^\[\]]+)?\]\]`)

// frontmatter mirrors the YAML stanza. It is a parse-only shape: every field
// is copied into [Competence] or turned into a [Relation], and nothing outside
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
	Tags        tagList         `yaml:"tags"`
	Similar     []similarEntry  `yaml:"similar"`
	RealisedBy  []string        `yaml:"realised_by"`
	Assessment  []claimEntry    `yaml:"assessment"`
	Lifecycle   lifecycleFields `yaml:",inline"`
}

// claimEntry is one assessment claim in frontmatter (ADR-0337 §SD2). The
// rating is read as a string whatever the dimension's scale, since YAML
// would otherwise type `7` and `product` differently.
type claimEntry struct {
	Dimension string   `yaml:"dimension"`
	Rating    string   `yaml:"rating"`
	Basis     []string `yaml:"basis"`
	By        string   `yaml:"by"`
	At        string   `yaml:"at"`
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

// ParseDir reads a vault directory whole: its competences sorted by slug,
// every relation they declare resolved against the set that was found, and the
// markdown files that were not competences.
//
// Sorting is not cosmetic: filesystem walk order is not stable across machines,
// and callers downstream diff and ingest these. Relations follow their source.
//
// Two failure modes are treated differently on purpose. A file whose *name*
// cannot be a competence slug is skipped and reported in [Corpus.Skipped] —
// vaults routinely hold reference notes beside competences, and refusing the
// whole read over one would make the common layout unreadable. A file that
// *is* addressable but whose content will not parse fails the call, because
// that is an authoring error to fix and dropping it would understate the
// corpus with no signal.
func ParseDir(vaultDir string) (corpus Corpus, err error) {
	return ParseDirWith(vaultDir, nil)
}

// ParseDirWith is [ParseDir] with the anchors — `realised_by` and the claims'
// bases — resolved against the tree by resolver (ADR-0337 §SD1). A nil
// resolver leaves every anchor [ResolutionUnchecked], which is what a parse
// with no checkout around it can honestly say.
//
// Anchors are resolved after the slug set is known, like wikilinks, and the
// claim problems that depend on resolution are filled in the same pass.
func ParseDirWith(vaultDir string, resolver AnchorResolverI) (corpus Corpus, err error) {
	if resolver == nil {
		resolver = uncheckedResolver{}
	}
	type parsedFile struct {
		path string
		slug string
		comp Competence
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
			// settings — and contains no competences.
			if d.Name() == ".obsidian" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		// A README is a directory's front page, and `readme` normalizes to a
		// perfectly well-formed slug — so without this it is read as a
		// competence, and any wikilink in its prose becomes a relation. That
		// is not hypothetical: ADR-0168 §SD7 puts a committed README.md at the
		// root of this repository's own vault, and the first parse of it
		// reported 80 competences and an extra edge.
		//
		// Reported as skipped rather than dropped, so a vault where the file
		// was meant to be a competence says so.
		if isReadme(d.Name()) {
			corpus.Skipped = append(corpus.Skipped, SkippedFile{
				Path:   relativeTo(vaultDir, path),
				Reason: "a README is a directory's front page, not a competence",
			})
			return nil
		}
		slug, slugErr := slugForPath(path)
		if slugErr != nil {
			corpus.Skipped = append(corpus.Skipped, SkippedFile{
				Path:   relativeTo(vaultDir, path),
				Reason: "name is not a competence slug",
			})
			return nil
		}
		if d.Name() == markerFileName {
			dirBacked[slug] = struct{}{}
		}
		files = append(files, parsedFile{path: path, slug: slug})
		return nil
	})
	if err != nil {
		return Corpus{}, eb.Build().Str("vaultDir", vaultDir).Errorf("unable to walk vault: %w", err)
	}

	known := make(map[string]struct{}, len(files))
	for i := range files {
		if _, dup := known[files[i].slug]; dup {
			return Corpus{}, eb.Build().Str("slug", files[i].slug).Str("path", files[i].path).
				Errorf("duplicate competence slug: slugs are the corpus's identity and must be unique")
		}
		known[files[i].slug] = struct{}{}
	}

	for i := range files {
		files[i].comp, files[i].rels, err = parseFile(files[i].path, files[i].slug, vaultDir)
		if err != nil {
			return Corpus{}, err
		}
	}

	sort.Slice(files, func(a, b int) bool { return files[a].slug < files[b].slug })

	corpus.Competences = make([]Competence, 0, len(files))
	nrels := 0
	for i := range files {
		nrels += len(files[i].rels)
	}
	corpus.Relations = make([]Relation, 0, nrels)
	for i := range files {
		corpus.Competences = append(corpus.Competences, files[i].comp)
		corpus.Relations = append(corpus.Relations, files[i].rels...)
	}
	resolveRelations(corpus.Relations, known, dirBacked)
	resolveAnchors(corpus.Relations, resolver)
	for i := range corpus.Competences {
		markBasisProblems(&corpus.Competences[i], corpus.Relations)
	}
	sort.Slice(corpus.Skipped, func(a, b int) bool { return corpus.Skipped[a].Path < corpus.Skipped[b].Path })
	return corpus, nil
}

// resolveAnchors asks the resolver about every anchor relation, in place.
// A target that will not parse as an anchor never reaches here: parseFile
// refuses it, since an anchor of an unknown kind is an authoring error rather
// than a link nobody could vouch for.
func resolveAnchors(rels []Relation, resolver AnchorResolverI) {
	for i := range rels {
		if rels[i].Kind != RelationKindRealisedBy && rels[i].Kind != RelationKindBasis {
			continue
		}
		a, err := ParseAnchor(rels[i].Target)
		if err != nil {
			rels[i].Resolution = ResolutionUnresolved
			continue
		}
		res := resolver.ResolveAnchor(a)
		rels[i].Resolution = res.Resolution
		rels[i].TargetStatus = res.Status
	}
}

// markBasisProblems adds the resolution-dependent problems to a competence's
// claims: a basis anchor the tree does not carry, or one that resolves to a
// retired decision. The basis relations are the record; this folds their
// answer back onto the claim so a reader of one grain sees it.
func markBasisProblems(comp *Competence, rels []Relation) {
	for i := range comp.Assessment {
		claim := &comp.Assessment[i]
		var unresolved, retired bool
		for _, rel := range rels {
			if rel.SourceSlug != comp.Slug || rel.Kind != RelationKindBasis || rel.Section != claim.Dimension {
				continue
			}
			if rel.Resolution == ResolutionUnresolved {
				unresolved = true
			}
			if IsRetiredStatus(rel.TargetStatus) {
				retired = true
			}
		}
		if unresolved {
			claim.Problems = append(claim.Problems, ClaimProblemUnresolvedBasis)
		}
		if retired {
			claim.Problems = append(claim.Problems, ClaimProblemRetiredBasis)
		}
	}
}

// isReadme reports whether a file name is a README, in any of the spellings a
// tree actually uses. Case-insensitive because the file is written by whoever
// set the directory up, not by the vault tooling.
func isReadme(name string) (ok bool) {
	return strings.EqualFold(name, "README.md")
}

// relativeTo renders path against the vault root, falling back to the absolute
// path when it cannot be expressed that way.
func relativeTo(root, path string) (rel string) {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}

// parseFile reads one markdown file into a competence and the relations it
// declares. The relations come back unresolved; ParseDir resolves them once
// the whole slug set is known.
func parseFile(path string, slug string, vaultDir string) (comp Competence, rels []Relation, err error) {
	var data []byte
	if data, err = os.ReadFile(path); err != nil {
		return comp, nil, eb.Build().Str("path", path).Errorf("unable to read: %w", err)
	}
	var (
		fm   frontmatter
		body string
	)
	if fm, body, err = splitFrontmatter(string(data)); err != nil {
		return comp, nil, eb.Build().Str("path", path).Errorf("unable to parse: %w", err)
	}

	relPath, relErr := filepath.Rel(vaultDir, path)
	if relErr != nil {
		relPath = path
	}

	comp = Competence{
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
		Tags:       normalizeTags(fm.Tags),
		Sections:   parseSections(body),
		Lifecycle:  lifecycleEvents(fm.Lifecycle),
	}
	// A domain is a grouping label, not an identity, so an unconventional one
	// is kept as written rather than refused.
	comp.Domain, _ = NormalizeTarget(fm.Domain)
	comp.Catalog = fm.Catalog
	if comp.Catalog == "" {
		// The catalog a competence belongs to is otherwise its top-level
		// directory, which is how the vault groups imported frameworks.
		if top, _, found := strings.Cut(relPath, string(os.PathSeparator)); found {
			comp.Catalog = top
		}
	}

	if comp.Assessment, err = parseClaims(fm.Assessment); err != nil {
		return comp, nil, eb.Build().Str("path", path).Errorf("unable to parse assessment: %w", err)
	}

	rels = make([]Relation, 0, len(fm.ParentSlugs)+len(fm.Similar)+len(fm.RealisedBy))
	for _, raw := range fm.RealisedBy {
		a, aErr := ParseAnchor(raw)
		if aErr != nil {
			return comp, nil, eb.Build().Str("path", path).Errorf("unable to parse realised_by: %w", aErr)
		}
		rels = append(rels, anchorRelation(slug, a, RelationKindRealisedBy, ""))
	}
	for _, claim := range comp.Assessment {
		for _, a := range claim.Basis {
			rels = append(rels, anchorRelation(slug, a, RelationKindBasis, claim.Dimension))
		}
	}
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
	rels = append(rels, sectionWikilinks(slug, comp.Sections)...)
	return comp, rels, nil
}

// anchorRelation builds the relation an anchor becomes. Its resolution is
// the zero value until resolveAnchors runs; a caller that never resolves
// reads it as unresolved, on the same forgetting-cannot-pass rule as every
// other relation.
func anchorRelation(source string, a Anchor, kind RelationKindE, section string) (rel Relation) {
	return Relation{SourceSlug: source, Target: a.String(), Kind: kind, Section: section}
}

// parseClaims reads the assessment list. A claim on a dimension this build
// does not register, or with a rating its scale does not hold, fails the
// parse: both are authoring errors with one right spelling, and reading them
// as something else would put a value in the corpus the vault does not hold.
// What is a judgement rather than a spelling — an empty basis, a missing
// assessor or date — is recorded as a problem on the claim instead.
func parseClaims(entries []claimEntry) (claims []Claim, err error) {
	if len(entries) == 0 {
		return nil, nil
	}
	claims = make([]Claim, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		spec, ok := LookupDimension(strings.TrimSpace(e.Dimension))
		if !ok {
			return nil, eb.Build().Str("dimension", e.Dimension).Errorf("unknown assessment dimension")
		}
		rating := strings.TrimSpace(e.Rating)
		if _, ok = spec.Rank(rating); !ok {
			return nil, eb.Build().Str("dimension", spec.Name).Str("rating", rating).Errorf("rating is not on the dimension's scale")
		}
		claim := Claim{Dimension: spec.Name, Rating: rating, By: strings.TrimSpace(e.By), At: parseTimestamp(e.At)}
		for _, raw := range e.Basis {
			a, aErr := ParseAnchor(raw)
			if aErr != nil {
				return nil, eb.Build().Str("dimension", spec.Name).Errorf("unable to parse basis: %w", aErr)
			}
			claim.Basis = append(claim.Basis, a)
		}
		if len(claim.Basis) == 0 {
			claim.Problems = append(claim.Problems, ClaimProblemEmptyBasis)
		}
		if claim.By == "" {
			claim.Problems = append(claim.Problems, ClaimProblemNoAssessor)
		}
		if claim.At.IsZero() {
			claim.Problems = append(claim.Problems, ClaimProblemNoDate)
		}
		if _, dup := seen[spec.Name]; dup {
			claim.Problems = append(claim.Problems, ClaimProblemDuplicateDimension)
		}
		seen[spec.Name] = struct{}{}
		claims = append(claims, claim)
	}
	return claims, nil
}

// makeRelation builds one relation from a raw link target, pre-marking the
// targets that cannot name a competence. ok is false only for an empty target,
// which carries nothing to record.
//
// A not-well-formed target is kept rather than dropped, with
// [ResolutionExternal] set here so the later resolve pass leaves it alone. It
// is the citation case: dropping it would silently discard what the Standards
// and Obligations sections exist to record, and resolving it would report a
// quarter of this corpus's links as broken.
func makeRelation(source, rawTarget string, kind RelationKindE, section string) (rel Relation, ok bool) {
	trimmed, qualified := trimMarkerSuffix(rawTarget)
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

// markerSuffix is how a link addresses a directory-backed competence the way
// Obsidian resolves it: the file is `{slug}/capability.md`, so the link that
// actually follows is `[[{slug}/capability]]`. The vault's spelling, for the
// same reason as [markerFileName].
const markerSuffix = "/" + "capability"

// trimMarkerSuffix reduces an explicitly-qualified link to the slug it
// names, reporting whether it was written that way.
//
// Both spellings denote the same competence, so both must resolve to one
// target — otherwise a corpus that has been normalised to the qualified form
// reports every one of those links as broken. The flag is kept because the two
// are not equally correct: the bare form dangles in Obsidian and the qualified
// form does not, which is the distinction [ResolutionDirRef] reports.
func trimMarkerSuffix(raw string) (out string, qualified bool) {
	trimmed := strings.TrimSpace(raw)
	if base, found := strings.CutSuffix(trimmed, markerSuffix); found && base != "" {
		return base, true
	}
	return trimmed, false
}

// sectionWikilinks extracts every [[link]] from the body, tagged with the
// section it was found under. The section is the point: it is what makes "what
// does this competence's Standards section cite" a query rather than a scan.
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
// [ResolutionDirRef] rather than [ResolutionDirect]: the competence exists
// here, but Obsidian looks for `{slug}.md` and will not follow it. The
// frontmatter kinds are exempt — they are resolved by this model only and are
// never followed by the editor, so the distinction would be noise.
func resolveRelations(rels []Relation, known, dirBacked map[string]struct{}) {
	for i := range rels {
		// Already classified at parse time as naming something outside the
		// corpus; no lookup can change that, since no competence could carry
		// such a slug.
		if rels[i].Resolution == ResolutionExternal {
			continue
		}
		if _, ok := known[rels[i].Target]; !ok {
			rels[i].Resolution = ResolutionUnresolved
			continue
		}
		// A bare [[slug]] naming a directory-backed competence dangles in
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
// per-competence lint columns; as a filter over one grain it needs no pass and
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
// with no stanza is an error rather than a body-only competence: every field
// that identifies a competence lives in the frontmatter.
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
// competence.
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
