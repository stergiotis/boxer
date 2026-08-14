package capmapcorpus

import (
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Obsidian's tag rules, as this corpus applies them to the `tags:` frontmatter
// key.
//
// The character rules are the ones the inline tag parser enforces
// ([github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/tag]):
// a tag starts with a letter, digit or underscore, continues with those plus
// `-` and `/` for nesting, does not end on a slash, and is never all digits —
// `#4` is the number four. They are restated here rather than imported because
// that package is a goldmark inline parser whose predicates are unexported and
// whose job is scanning prose, which this is not; the two are pinned to each
// other by [TestNormalizeTagMatchesTheInlineParsersRules].
//
// The leading `#` is optional on the way in and absent on the way out. Obsidian
// writes frontmatter tags without it and inline tags with it, and a vault
// commonly carries both spellings for the same tag.

// NormalizeTag renders one authored tag in the corpus's canonical form, and
// reports whether it is a tag at all.
//
// ok is false for anything that cannot be one — an empty string, a bare `#`, a
// number, a value carrying characters a tag body may not hold. Rejecting rather
// than repairing is deliberate: a "tag" containing a space is two tags or a
// typo, and guessing which would put a value in the corpus that the vault does
// not contain.
func NormalizeTag(raw string) (tag string, ok bool) {
	tag = strings.TrimSpace(raw)
	tag = strings.TrimPrefix(tag, "#")
	if tag == "" {
		return "", false
	}
	if !isTagStart(rune(tag[0])) {
		return "", false
	}
	digitsOnly := true
	for _, r := range tag {
		if !isTagBody(r) {
			return "", false
		}
		if !unicode.IsDigit(r) {
			digitsOnly = false
		}
	}
	if digitsOnly || strings.HasSuffix(tag, "/") {
		return "", false
	}
	return tag, true
}

// isTagStart reports whether r may begin a tag body.
func isTagStart(r rune) (ok bool) {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

// isTagBody reports whether r may appear inside a tag body.
func isTagBody(r rune) (ok bool) {
	return isTagStart(r) || r == '-' || r == '/'
}

// normalizeTags maps a frontmatter list to canonical tags, dropping what cannot
// be one and keeping the first spelling of a repeat.
//
// Authored order is kept rather than sorted: the vault is the source of truth
// and a dump has to be able to write back what it read, so reordering here
// would make every re-dump a diff.
func normalizeTags(raw []string) (tags []string) {
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(raw))
	for _, r := range raw {
		t, ok := NormalizeTag(r)
		if !ok {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		tags = append(tags, t)
	}
	return tags
}

// tagList decodes the frontmatter `tags:` key from either shape a vault writes
// it in: a YAML sequence, or one scalar holding several tags separated by
// commas or spaces. Obsidian accepts both, so a reader that took only the
// sequence would silently read a tagged note as untagged.
type tagList []string

func (inst *tagList) UnmarshalYAML(node *yaml.Node) (err error) {
	switch node.Kind {
	case yaml.SequenceNode:
		var v []string
		if err = node.Decode(&v); err != nil {
			return
		}
		*inst = v
	case yaml.ScalarNode:
		if node.Tag == "!!null" {
			*inst = nil
			return
		}
		var s string
		if err = node.Decode(&s); err != nil {
			return
		}
		*inst = strings.FieldsFunc(s, func(r rune) bool {
			return r == ',' || unicode.IsSpace(r)
		})
	}
	return
}
