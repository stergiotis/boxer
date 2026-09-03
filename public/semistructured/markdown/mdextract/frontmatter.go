package mdextract

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yuin/goldmark/parser"

	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
)

// extractFrontmatter reads the parsed YAML block off the parser context. The
// raw map is returned beside the exploded form for the Obsidian-property
// readers, which want the tree rather than the leaves.
func extractFrontmatter(pc parser.Context) (fm *Frontmatter, raw map[string]any) {
	raw, err := obsidian.TryGetFrontmatter(pc)
	if err != nil {
		fm = &Frontmatter{Err: err.Error()}
		return fm, nil
	}
	if raw == nil {
		return nil, nil
	}
	fm = &Frontmatter{}
	s := shredder{}
	s.walkMap("", nil, raw)
	fm.Leaves = s.out
	fm.Dropped = s.dropped
	fm.Aliases = stringList(raw, "aliases", "alias")
	return
}

// shredder explodes a decoded YAML tree into leaves the way the canonical
// leeway JSON mapping does — sorted keys for a deterministic order, array
// positions elided into params.
type shredder struct {
	out     []Leaf
	dropped uint64
}

func (inst *shredder) walkMap(prefix string, params []uint64, m map[string]any) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		inst.walk(prefix+"/"+escapeKey(k), params, m[k])
	}
}

func (inst *shredder) walk(path string, params []uint64, v any) {
	switch t := v.(type) {
	case nil:
		inst.emit(path, params, Leaf{Kind: LeafKindNull})
	case bool:
		inst.emit(path, params, Leaf{Kind: LeafKindBool, B: t})
	case string:
		if ts, ok := ParseTimestamp(t); ok {
			inst.emit(path, params, Leaf{Kind: LeafKindTime, S: t, T: ts})
			return
		}
		inst.emit(path, params, Leaf{Kind: LeafKindString, S: t})
	case time.Time:
		inst.emit(path, params, Leaf{Kind: LeafKindTime, S: t.Format(time.RFC3339Nano), T: t.UTC()})
	case int:
		inst.emit(path, params, Leaf{Kind: LeafKindInt, I: int64(t)})
	case int64:
		inst.emit(path, params, Leaf{Kind: LeafKindInt, I: t})
	case uint64:
		// yaml.v2 hands out uint64 only for values past the int64 range;
		// they do not fit the integer section and are carried as floats,
		// which is the lossy branch YAML itself takes for such literals.
		inst.emit(path, params, Leaf{Kind: LeafKindFloat, F: float64(t)})
	case float64:
		inst.emit(path, params, Leaf{Kind: LeafKindFloat, F: t})
	case map[string]any:
		if len(t) == 0 {
			inst.emit(path, params, Leaf{Kind: LeafKindEmptyObject})
			return
		}
		inst.walkMap(path, params, t)
	case map[any]any:
		if len(t) == 0 {
			inst.emit(path, params, Leaf{Kind: LeafKindEmptyObject})
			return
		}
		inst.walkMap(path, params, stringKeyed(t))
	case []any:
		if len(t) == 0 {
			inst.emit(path, params, Leaf{Kind: LeafKindEmptyArray})
			return
		}
		for i, e := range t {
			if uint64(i) > membership.MaxParamsIndex {
				inst.dropped++
				continue
			}
			inst.walk(path+"/_", append(params, uint64(i)), e)
		}
	default:
		// yaml.v2 decodes nothing else into an any; anything that does
		// arrive keeps its text rather than being lost.
		inst.emit(path, params, Leaf{Kind: LeafKindString, S: fmt.Sprint(t)})
	}
}

func (inst *shredder) emit(path string, params []uint64, l Leaf) {
	l.Path = path
	if len(params) > 0 {
		l.Params = make([]uint64, len(params))
		copy(l.Params, params)
	}
	inst.out = append(inst.out, l)
}

// stringKeyed renders yaml.v2's nested-map shape with string keys.
func stringKeyed(m map[any]any) (out map[string]any) {
	out = make(map[string]any, len(m))
	for k, v := range m {
		out[fmt.Sprint(k)] = v
	}
	return
}

// escapeKey applies RFC 6901 escaping so a key containing "/" or "~" stays
// one path segment.
func escapeKey(k string) string {
	if !strings.ContainsAny(k, "~/") {
		return k
	}
	return strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
}

// stringList reads an Obsidian list property under the first of keys that is
// present: a YAML list of scalars, or one string split on commas — both
// spellings Obsidian accepts for `tags` and `aliases`.
func stringList(raw map[string]any, keys ...string) (out []string) {
	for _, key := range keys {
		v, ok := raw[key]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case string:
			for part := range strings.SplitSeq(t, ",") {
				if p := strings.TrimSpace(part); p != "" {
					out = append(out, p)
				}
			}
		case []any:
			for _, e := range t {
				if e == nil {
					continue
				}
				if p := strings.TrimSpace(fmt.Sprint(e)); p != "" {
					out = append(out, p)
				}
			}
		default:
			if t != nil {
				out = append(out, strings.TrimSpace(fmt.Sprint(t)))
			}
		}
		return
	}
	return
}

// frontmatterTags reads the `tags` (or `tag`) property into Tag entries. An
// entry that is not a well-formed tag is skipped — Obsidian shows such a
// property value but does not resolve it as a tag either.
func frontmatterTags(raw map[string]any, firstOrdinal uint64) (tags []Tag) {
	if raw == nil {
		return
	}
	for _, s := range stringList(raw, "tags", "tag") {
		// A space-separated string is also accepted by Obsidian.
		for _, word := range strings.Fields(s) {
			name, ok := NormalizeTag(word)
			if !ok {
				continue
			}
			tags = append(tags, Tag{
				Ordinal: firstOrdinal + uint64(len(tags)),
				Section: -1,
				Source:  TagSourceFrontmatter,
				Tag:     name,
			})
		}
	}
	return
}

// NormalizeTag applies the inline tag parser's rules to a property value: an
// optional leading "#", then a body of ASCII letters, digits, "_", "-" and
// "/" that starts with a letter, digit or "_", does not end in "/" and is not
// all digits. The name is returned without the "#".
func NormalizeTag(raw string) (tag string, ok bool) {
	tag = strings.TrimPrefix(strings.TrimSpace(raw), "#")
	tag = strings.TrimSuffix(tag, "/")
	if tag == "" || !isTagStart(tag[0]) {
		return "", false
	}
	allDigits := true
	for i := 0; i < len(tag); i++ {
		c := tag[i]
		if !isTagStart(c) && c != '-' && c != '/' {
			return "", false
		}
		if c < '0' || c > '9' {
			allDigits = false
		}
	}
	if allDigits {
		return "", false
	}
	return tag, true
}

func isTagStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}
