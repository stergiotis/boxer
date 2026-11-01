package mdgraph

import (
	"path"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdextract"
)

// noteExt is the extension a note's path carries.
const noteExt = ".md"

// Note is one document of the corpus.
type Note struct {
	// Path is the slash path relative to the corpus root, with its
	// extension; paths are unique within a corpus.
	Path string
	// Doc is the document's extraction; nil reads as a document with no
	// links, tags or aliases.
	Doc *mdextract.Document
}

// ResolutionE is what a link turned out to point at.
type ResolutionE uint8

const (
	// ResolutionNote is a link to a note of the corpus.
	ResolutionNote ResolutionE = iota
	// ResolutionUnresolved is a link to a note the corpus does not hold.
	ResolutionUnresolved
	// ResolutionSkipped is a link that is not a note link: external, a
	// fragment of the linking note itself, or an attachment.
	ResolutionSkipped
)

var AllResolutions = []ResolutionE{ResolutionNote, ResolutionUnresolved, ResolutionSkipped}

func (inst ResolutionE) String() string {
	switch inst {
	case ResolutionNote:
		return "note"
	case ResolutionUnresolved:
		return "unresolved"
	case ResolutionSkipped:
		return "skipped"
	}
	return "unknown"
}

// Resolver decides which note of a corpus a link means (ADR-0353 §SD3).
type Resolver struct {
	paths   []string           // as given
	stems   []string           // lowercased paths without the note extension
	byPath  map[string]int32   // lowercased path → note
	byName  map[string][]int32 // lowercased base name without the extension → notes
	byAlias map[string][]int32 // lowercased alias → notes
}

// NewResolver indexes the corpus's paths, base names and aliases.
func NewResolver(notes []Note) (inst *Resolver) {
	inst = &Resolver{
		paths:   make([]string, len(notes)),
		stems:   make([]string, len(notes)),
		byPath:  make(map[string]int32, len(notes)),
		byName:  make(map[string][]int32, len(notes)),
		byAlias: make(map[string][]int32),
	}
	for i, n := range notes {
		id := int32(i)
		lp := strings.ToLower(n.Path)
		stem := strings.TrimSuffix(lp, noteExt)
		inst.paths[i] = n.Path
		inst.stems[i] = stem
		if _, dup := inst.byPath[lp]; !dup {
			inst.byPath[lp] = id
		}
		name := path.Base(stem)
		inst.byName[name] = append(inst.byName[name], id)
		if n.Doc == nil || n.Doc.Frontmatter == nil {
			continue
		}
		for _, a := range n.Doc.Frontmatter.Aliases {
			a = strings.ToLower(strings.TrimSpace(a))
			if a != "" {
				inst.byAlias[a] = append(inst.byAlias[a], id)
			}
		}
	}
	return
}

// Resolve finds the note the link l of note src means. key is the
// normalised target of an unresolved link, which names its node.
func (inst *Resolver) Resolve(src int32, l mdextract.Link) (dst int32, res ResolutionE, key string) {
	dst = -1
	target := strings.TrimSpace(l.Target)
	if l.External || l.Kind == mdextract.LinkKindAutolink || target == "" {
		res = ResolutionSkipped
		return
	}
	if ext := path.Ext(target); ext != "" && !strings.EqualFold(ext, noteExt) {
		res = ResolutionSkipped
		return
	}
	switch l.Kind {
	case mdextract.LinkKindWikilink, mdextract.LinkKindEmbed:
		dst, key = inst.resolvePage(src, target)
	default:
		dst, key = inst.resolvePath(src, target)
	}
	if dst < 0 {
		res = ResolutionUnresolved
		return
	}
	res = ResolutionNote
	key = ""
	return
}

// resolvePage applies the page rule of a wikilink or embed.
func (inst *Resolver) resolvePage(src int32, page string) (dst int32, key string) {
	key = strings.TrimSuffix(strings.ToLower(strings.TrimPrefix(page, "/")), noteExt)
	if strings.Contains(key, "/") {
		var cands []int32
		for i, stem := range inst.stems {
			if stem == key || strings.HasSuffix(stem, "/"+key) {
				cands = append(cands, int32(i))
			}
		}
		return inst.closest(src, cands), key
	}
	if cands := inst.byName[key]; len(cands) > 0 {
		return inst.closest(src, cands), key
	}
	return inst.closest(src, inst.byAlias[key]), key
}

// resolvePath applies the path rule of an inline link or image: relative
// to the linking note, then to the corpus root, then the page rule for a
// bare name.
func (inst *Resolver) resolvePath(src int32, target string) (dst int32, key string) {
	rel := strings.ToLower(path.Join(path.Dir(inst.srcPath(src)), target))
	root := strings.ToLower(path.Clean(strings.TrimPrefix(target, "/")))
	tries := []string{rel, root}
	if strings.HasPrefix(target, "/") {
		tries = tries[1:]
	}
	for _, p := range tries {
		if id, ok := inst.byPath[p]; ok {
			return id, ""
		}
		if id, ok := inst.byPath[p+noteExt]; ok {
			return id, ""
		}
	}
	if !strings.Contains(target, "/") {
		return inst.resolvePage(src, target)
	}
	return -1, strings.TrimSuffix(tries[0], noteExt)
}

func (inst *Resolver) srcPath(src int32) string {
	if src < 0 || int(src) >= len(inst.paths) {
		return ""
	}
	return inst.paths[src]
}

// closest picks among candidate notes the one whose directory shares the
// longest prefix with src's, then the shortest path, then the first in
// byte order; -1 when there are none.
func (inst *Resolver) closest(src int32, cands []int32) (best int32) {
	best = -1
	srcDir := path.Dir(inst.srcPath(src))
	bestShared := 0
	for _, c := range cands {
		shared := sharedDirPrefix(srcDir, path.Dir(inst.paths[c]))
		if best >= 0 && !inst.isCloser(c, shared, best, bestShared) {
			continue
		}
		best, bestShared = c, shared
	}
	return
}

// isCloser reports whether candidate c, sharing shared directory segments
// with the linking note, beats the current best.
func (inst *Resolver) isCloser(c int32, shared int, best int32, bestShared int) bool {
	if shared != bestShared {
		return shared > bestShared
	}
	if lc, lb := len(inst.paths[c]), len(inst.paths[best]); lc != lb {
		return lc < lb
	}
	return inst.paths[c] < inst.paths[best]
}

// sharedDirPrefix counts the leading directory segments a and b share.
func sharedDirPrefix(a, b string) (n int) {
	if a == "." || b == "." {
		return 0
	}
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return
}
