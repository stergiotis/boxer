// Package capmapsimilarity ranks the competences of a vault by how alike their
// prose is, so a reviewer can find the two notes that describe one thing under
// two names — the merge candidates a hierarchy written by many hands
// accumulates — without reading every pair.
//
// The measure is the normalised compression distance (NCD) over each
// competence's body: NCD(x, y) = (C(xy) − min(C(x), C(y))) / max(C(x), C(y)),
// with C a compressor. 0 is identical text, 1 shares nothing. It is the
// parameter-free similarity boxer already uses for stylometry
// (`public/analytics/similarity/compression`), and the joint length is taken
// the way that package takes it: the reference text is preloaded as the
// compressor's raw dictionary, so C(xy) is approximated as C(x) + C_x(y) and a
// competence is compared against every candidate at the cost of compressing
// the candidate once. That approximation is what makes an all-pairs pass over
// a vault of a thousand notes a matter of seconds rather than minutes.
//
// # What is not compared
//
// Two rules keep the result about resemblance rather than about structure.
// A competence is not compared with its own ancestors or descendants: a
// parent's Vision and Scope restates its children by construction, and a
// ranker that reported every such pair would be reporting the hierarchy back.
// And by default competences are compared only within their catalog, since a
// catalog is a framework imported whole and two frameworks describing the same
// domain resemble each other everywhere — [Options.Cross] inverts the rule for
// the run that wants exactly that mapping.
//
// A competence with no prose is not compared at all. There is nothing to
// measure, and reporting NCD 1 against everything would say "unlike" about a
// note that is merely unwritten.
//
// # Where the result goes
//
// [Rank] returns the ranking; it writes nothing. `boxer capmap similar` records
// each competence's neighbours as `similar:` frontmatter through
// [capmapcorpus.UpsertSimilar], where the parser reads them back as
// [capmapcorpus.RelationKindSimilar] relations carrying the score, and the
// applet book's lint lens lists them. The vault stays authoritative
// (ADR-0168 §SD3): the ranking is a proposal written where a reviewer edits.
package capmapsimilarity

import (
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"

	"github.com/stergiotis/boxer/public/analytics/similarity/compression"
	"github.com/stergiotis/boxer/public/ea"
	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

const (
	// DefaultThreshold keeps a pair when its NCD is at most this. 0.4 is
	// where, on the reference corpus, the pairs stop being paraphrases of one
	// idea and start being two notes that share a vocabulary; it is a
	// starting point, not a finding, and the flag exists to move it.
	DefaultThreshold = 0.4
	// DefaultTop bounds how many neighbours a competence records. A reviewer
	// acts on the nearest few; a list of thirty is a table, not a hint.
	DefaultTop = 5
	// ancestryBound caps the walk up the parent chain, as the applet book's
	// ancestor CTE does: a hierarchy deeper than this is not one, and a cycle
	// in `parent_ids` must not hang the ranker.
	ancestryBound = 8
)

// Options shape a ranking run. The zero value is usable and means the
// defaults.
type Options struct {
	// Threshold is the largest NCD kept; 0 means [DefaultThreshold].
	Threshold float64
	// Top is the most neighbours recorded per competence; 0 means [DefaultTop].
	Top int
	// Cross compares competences across catalogs instead of within one.
	Cross bool
	// Workers bounds the goroutines the all-pairs pass runs on; 0 means
	// GOMAXPROCS.
	Workers int
}

func (inst Options) withDefaults() (opts Options) {
	opts = inst
	if opts.Threshold <= 0 {
		opts.Threshold = DefaultThreshold
	}
	if opts.Top <= 0 {
		opts.Top = DefaultTop
	}
	if opts.Workers <= 0 {
		opts.Workers = runtime.GOMAXPROCS(0)
	}
	return opts
}

// Neighbour is one competence found near another.
type Neighbour struct {
	Slug string  `json:"slug"`
	Name string  `json:"name"`
	Ncd  float64 `json:"ncd"`
}

// Entry is one compared competence and its nearest neighbours, nearest first.
// A competence that was compared and found nothing under the threshold is
// present with an empty Similar, which is how a writer knows to remove a
// stanza an earlier run left.
type Entry struct {
	Slug      string      `json:"slug"`
	Name      string      `json:"name"`
	Level     uint8       `json:"level"`
	Catalog   string      `json:"catalog"`
	VaultPath string      `json:"vault_path"`
	Similar   []Neighbour `json:"similar"`
}

// Result is a ranking and the counts that say how it was made.
type Result struct {
	// Entries holds every compared competence, sorted by slug.
	Entries []Entry `json:"entries"`
	// Compared is the number of pairs measured; Kept the number under the
	// threshold before the per-competence cap.
	Compared int `json:"compared"`
	Kept     int `json:"kept"`
	// Unwritten counts competences left out for having no prose.
	Unwritten int     `json:"unwritten"`
	Threshold float64 `json:"threshold"`
	Top       int     `json:"top"`
	Cross     bool    `json:"cross"`
}

// Text is the prose a competence is compared by: its sections in document
// order, heading and body, the same text the corpus stores as its
// `competencesection` rows. Frontmatter is excluded — a name, a level and a
// parent link are structure, and the ranker exists to see past structure.
func Text(comp capmapcorpus.Competence) (text string) {
	var b strings.Builder
	for _, sec := range comp.Sections {
		if strings.TrimSpace(sec.Text) == "" {
			continue
		}
		b.WriteString("# ")
		b.WriteString(sec.Heading)
		b.WriteString("\n\n")
		b.WriteString(sec.Text)
		b.WriteString("\n\n")
	}
	return b.String()
}

// pair is one measured resemblance under the threshold, by index into the
// corpus's competences.
type pair struct {
	a, b int
	ncd  float64
}

// Rank measures every eligible pair of competences and returns each one's
// nearest neighbours. It is deterministic for a given corpus and options: the
// compressor is, and ties are broken by slug.
func Rank(corpus capmapcorpus.Corpus, opts Options) (res Result, err error) {
	opts = opts.withDefaults()
	res.Threshold, res.Top, res.Cross = opts.Threshold, opts.Top, opts.Cross

	comps := corpus.Competences
	texts := make([]string, len(comps))
	candidates := make([]int, 0, len(comps))
	for i, comp := range comps {
		texts[i] = Text(comp)
		if texts[i] == "" {
			res.Unwritten++
			continue
		}
		candidates = append(candidates, i)
	}
	if len(candidates) == 0 {
		return res, nil
	}

	related := relatedness(corpus)
	eligible := func(a, b int) bool {
		if (comps[a].Catalog == comps[b].Catalog) == opts.Cross {
			return false
		}
		return !related(comps[a].Slug, comps[b].Slug)
	}

	// Each competence's own compressed length, once. The pass below reads
	// these for both ends of a pair.
	own := make([]uint64, len(comps))
	{
		var enc *zstd.Encoder
		if enc, err = newEncoder(nil); err != nil {
			return res, err
		}
		for _, i := range candidates {
			if own[i], err = measure(enc, texts[i]); err != nil {
				return res, eh.Errorf("unable to compress %q: %w", comps[i].Slug, err)
			}
		}
	}

	// The all-pairs pass: one goroutine per reference competence at a time,
	// each with the reference preloaded as its encoder's dictionary, walking
	// the candidates after it. Pairs are unordered, so i < j visits each once.
	var (
		mu       sync.Mutex
		pairs    []pair
		compared int
		firstErr error
		wg       sync.WaitGroup
		work     = make(chan int)
	)
	worker := func() {
		defer wg.Done()
		for pos := range work {
			i := candidates[pos]
			local, n, wErr := rankAgainst(i, candidates[pos+1:], texts, own, eligible, opts.Threshold)
			mu.Lock()
			if wErr != nil && firstErr == nil {
				firstErr = eh.Errorf("unable to rank %q: %w", comps[i].Slug, wErr)
			}
			pairs = append(pairs, local...)
			compared += n
			mu.Unlock()
		}
	}
	for range min(opts.Workers, len(candidates)) {
		wg.Add(1)
		go worker()
	}
	for pos := range candidates {
		work <- pos
	}
	close(work)
	wg.Wait()
	if firstErr != nil {
		return res, firstErr
	}
	res.Compared, res.Kept = compared, len(pairs)

	// Fold the pairs into per-competence lists, nearest first, capped.
	byIndex := make(map[int][]Neighbour, len(candidates))
	for _, p := range pairs {
		byIndex[p.a] = append(byIndex[p.a], Neighbour{Slug: comps[p.b].Slug, Name: comps[p.b].Name, Ncd: p.ncd})
		byIndex[p.b] = append(byIndex[p.b], Neighbour{Slug: comps[p.a].Slug, Name: comps[p.a].Name, Ncd: p.ncd})
	}
	res.Entries = make([]Entry, 0, len(candidates))
	for _, i := range candidates {
		near := byIndex[i]
		sort.Slice(near, func(x, y int) bool {
			if near[x].Ncd != near[y].Ncd {
				return near[x].Ncd < near[y].Ncd
			}
			return near[x].Slug < near[y].Slug
		})
		if len(near) > opts.Top {
			near = near[:opts.Top]
		}
		res.Entries = append(res.Entries, Entry{
			Slug: comps[i].Slug, Name: comps[i].Name, Level: comps[i].Level,
			Catalog: comps[i].Catalog, VaultPath: comps[i].VaultPath, Similar: near,
		})
	}
	sort.Slice(res.Entries, func(x, y int) bool { return res.Entries[x].Slug < res.Entries[y].Slug })
	return res, nil
}

// rankAgainst measures reference i against every eligible candidate in rest,
// with i's text preloaded as the dictionary, and returns the pairs under the
// threshold and how many were measured.
func rankAgainst(i int, rest []int, texts []string, own []uint64, eligible func(a, b int) bool, threshold float64) (kept []pair, compared int, err error) {
	var enc *zstd.Encoder
	if enc, err = newEncoder(unsafeperf.UnsafeStringToBytes(texts[i])); err != nil {
		return nil, 0, err
	}
	for _, j := range rest {
		if !eligible(i, j) {
			continue
		}
		var withDict uint64
		if withDict, err = measure(enc, texts[j]); err != nil {
			return kept, compared, err
		}
		compared++
		ncd := compression.CalculateNormalizedCompressionDistance(own[i]+withDict, own[i], own[j])
		if ncd <= threshold {
			kept = append(kept, pair{a: i, b: j, ncd: ncd})
		}
	}
	return kept, compared, nil
}

// newEncoder builds the compressor the distance is measured with: zstd at its
// default level, single-threaded — the parallelism is across references, and
// an encoder spawning workers of its own would oversubscribe — and, when dict
// is given, with that text as raw history so the next write is compressed
// against it.
func newEncoder(dict []byte) (enc *zstd.Encoder, err error) {
	opts := []zstd.EOption{
		zstd.WithEncoderLevel(zstd.SpeedDefault),
		zstd.WithEncoderConcurrency(1),
		zstd.WithLowerEncoderMem(true),
	}
	if dict != nil {
		opts = append(opts, zstd.WithEncoderDictRaw(0, dict))
	}
	if enc, err = zstd.NewWriter(nil, opts...); err != nil {
		return nil, eh.Errorf("unable to create zstd encoder: %w", err)
	}
	return enc, nil
}

// measure is C(text) under enc's current dictionary, if any.
func measure(enc *zstd.Encoder, text string) (n uint64, err error) {
	w := &ea.SizeMeasureWriter{}
	enc.Reset(w)
	if _, err = enc.Write(unsafeperf.UnsafeStringToBytes(text)); err != nil {
		return 0, eh.Errorf("unable to compress: %w", err)
	}
	if err = enc.Close(); err != nil {
		return 0, eh.Errorf("unable to close encoder: %w", err)
	}
	return w.Size, nil
}

// relatedness builds the ancestor-or-descendant test from the corpus's parent
// relations. Only links that resolved count — an unresolved parent names no
// competence to be related to.
func relatedness(corpus capmapcorpus.Corpus) (related func(a, b string) bool) {
	parents := make(map[string][]string, len(corpus.Competences))
	for _, r := range corpus.Relations {
		if r.Kind != capmapcorpus.RelationKindParent {
			continue
		}
		if r.Resolution != capmapcorpus.ResolutionDirect && r.Resolution != capmapcorpus.ResolutionDirRef {
			continue
		}
		parents[r.SourceSlug] = append(parents[r.SourceSlug], r.Target)
	}
	ancestors := make(map[string]map[string]struct{}, len(corpus.Competences))
	for _, comp := range corpus.Competences {
		set := make(map[string]struct{}, 4)
		frontier := []string{comp.Slug}
		for depth := 0; depth < ancestryBound && len(frontier) > 0; depth++ {
			var next []string
			for _, slug := range frontier {
				for _, p := range parents[slug] {
					if _, seen := set[p]; seen {
						continue
					}
					set[p] = struct{}{}
					next = append(next, p)
				}
			}
			frontier = next
		}
		ancestors[comp.Slug] = set
	}
	return func(a, b string) bool {
		if _, ok := ancestors[a][b]; ok {
			return true
		}
		_, ok := ancestors[b][a]
		return ok
	}
}
