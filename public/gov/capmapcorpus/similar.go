package capmapcorpus

import (
	"bytes"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// SimilarEntry is one scored resemblance to record in a note's frontmatter: the
// target competence and the normalised compression distance to it, 0 being
// identical text and 1 sharing nothing. The ranker in
// `public/gov/capmapsimilarity` produces these; this package only writes them.
type SimilarEntry struct {
	// Target is the other competence's slug.
	Target string
	// Ncd is the normalised compression distance, written to four decimals.
	Ncd float64
	// Qualified writes the link as `[[slug/capability]]` — the spelling
	// Obsidian resolves for a directory-backed competence — rather than
	// `[[slug]]`. The parser reads both as the same competence (frontmatter
	// kinds are exempt from the dirref rule), so this is about the link
	// working in the editor, not about what the corpus means.
	Qualified bool
}

// similarKey is the frontmatter key the stanza lives under, the same one
// [frontmatter] reads.
const similarKey = "similar"

// UpsertSimilar replaces the `similar:` stanza of a note with entries — adding
// the key when the note has none, and removing it when entries is empty — and
// leaves everything else alone: the other keys in the order they were written,
// their quoting, and the body byte for byte.
//
// It edits the YAML as a node tree rather than re-rendering the frontmatter
// from a [Competence], because a re-render would restate every key in the
// renderer's order and style and turn a one-stanza change into a whole-file
// diff. What yaml.v3 does not preserve is whitespace it considers
// insignificant — the indent of a nested sequence, a blank line between keys —
// so a note written by hand in an unusual layout may come back renormalised
// there. A test pins that the parsed competence is unchanged by the edit.
//
// changed reports whether out differs from content, so a caller writing files
// can leave an unchanged note's mtime alone.
func UpsertSimilar(content []byte, entries []SimilarEntry) (out []byte, changed bool, err error) {
	const open = "---\n"
	text := string(content)
	if !strings.HasPrefix(text, open) {
		return nil, false, eh.Errorf("no opening frontmatter delimiter")
	}
	stanza, remainder, found := strings.Cut(text[len(open):], "\n---")
	if !found {
		return nil, false, eh.Errorf("no closing frontmatter delimiter")
	}

	var doc yaml.Node
	if err = yaml.Unmarshal([]byte(stanza), &doc); err != nil {
		return nil, false, eh.Errorf("unable to parse frontmatter: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, false, eh.Errorf("frontmatter is not a mapping")
	}
	mapping := doc.Content[0]

	// Locate the existing pair. A mapping node's Content alternates key, value.
	at := -1
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == similarKey {
			at = i
			break
		}
	}
	switch {
	case len(entries) == 0 && at >= 0:
		mapping.Content = append(mapping.Content[:at], mapping.Content[at+2:]...)
	case len(entries) == 0:
		return content, false, nil
	case at >= 0:
		mapping.Content[at+1] = similarNode(entries)
	default:
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: similarKey},
			similarNode(entries))
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err = enc.Encode(&doc); err != nil {
		return nil, false, eh.Errorf("unable to render frontmatter: %w", err)
	}
	if err = enc.Close(); err != nil {
		return nil, false, eh.Errorf("unable to render frontmatter: %w", err)
	}

	var b strings.Builder
	b.Grow(len(open) + buf.Len() + 3 + len(remainder))
	b.WriteString(open)
	b.Write(buf.Bytes())
	b.WriteString("---")
	b.WriteString(remainder)
	out = []byte(b.String())
	return out, !bytes.Equal(out, content), nil
}

// similarNode renders entries as the sequence the parser's [similarEntry]
// reads: one mapping per entry, `ref` as a double-quoted wikilink — the
// brackets would otherwise be read as a flow sequence — and `ncd` to four
// decimals, which is the precision a distance in [0, 1] is worth stating at
// and what keeps two runs from producing a diff over the fifth digit.
func similarNode(entries []SimilarEntry) (seq *yaml.Node) {
	seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	seq.Content = make([]*yaml.Node, 0, len(entries))
	for _, e := range entries {
		target := e.Target
		if e.Qualified {
			target += markerSuffix
		}
		seq.Content = append(seq.Content, &yaml.Node{
			Kind: yaml.MappingNode, Tag: "!!map",
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "ref"},
				{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: wikilink(target)},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "ncd"},
				{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(e.Ncd, 'f', 4, 64)},
			},
		})
	}
	return seq
}

// DirectoryBacked reports whether the competence is defined in a
// `{slug}/capability.md` marker file — the form a competence with children
// takes — as opposed to a plain `{slug}.md`. It decides how a link to it is
// spelled for Obsidian (see [SimilarEntry.Qualified]).
func (inst Competence) DirectoryBacked() (ok bool) {
	return strings.HasSuffix(inst.VaultPath, markerFileName) &&
		(len(inst.VaultPath) == len(markerFileName) || inst.VaultPath[len(inst.VaultPath)-len(markerFileName)-1] == '/')
}
