package obsidian

import (
	"bytes"
	"fmt"
	"io"
	"sort"

	"github.com/stergiotis/boxer/public/observability/eh"
	meta "github.com/yuin/goldmark-meta"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/util"
	"gopkg.in/yaml.v2"
)

// GetFrontmatter extracts YAML frontmatter metadata from a parser.Context
// after rendering. Returns nil if no frontmatter was present.
func GetFrontmatter(pc parser.Context) map[string]any {
	return meta.Get(pc)
}

// TryGetFrontmatter extracts YAML frontmatter metadata from a parser.Context
// after rendering. Returns an error if the YAML was malformed.
func TryGetFrontmatter(pc parser.Context) (metadata map[string]any, err error) {
	metadata, err = meta.TryGet(pc)
	return
}

// ParseFrontmatter reads the YAML frontmatter of src without parsing the
// markdown around it: what [TryGetFrontmatter] returns after a full parse
// with [FeatureFrontmatter], for a caller that wants nothing else. found
// reports whether src opens a frontmatter block at all; metadata is nil when
// it does not, and when the YAML is malformed, in which case err says why.
//
// The block rules are goldmark-meta's, reproduced: the first line of src
// consists of dashes only (at least one, indented less than four columns),
// and the block ends at the next non-blank line of dashes only, or at the end
// of src if there is none. The lines between are decoded with the YAML
// package goldmark-meta uses, so the values have the same Go types. A test
// holds the two to agreement.
func ParseFrontmatter(src []byte) (metadata map[string]any, found bool, err error) {
	first, rest, _ := bytes.Cut(src, []byte{'\n'})
	if indent, _ := util.IndentWidth(first, 0); util.IsBlank(first) || indent >= 4 || !isMetaSeparator(first) {
		return
	}
	found = true
	body := rest
	for off := 0; off < len(rest); {
		end := bytes.IndexByte(rest[off:], '\n')
		next := len(rest)
		if end >= 0 {
			next = off + end + 1
		}
		line := rest[off:next]
		if isMetaSeparator(line) && !util.IsBlank(line) {
			body = rest[:off]
			break
		}
		off = next
	}
	metadata = map[string]any{}
	if err = yaml.Unmarshal(body, &metadata); err != nil {
		metadata = nil
		err = eh.Errorf("frontmatter is not valid YAML: %w", err)
	}
	return
}

// isMetaSeparator is goldmark-meta's separator test: after trimming spaces,
// nothing but dashes (an all-space line passes; callers rule that out).
func isMetaSeparator(line []byte) (ok bool) {
	line = util.TrimRightSpace(util.TrimLeftSpace(line))
	for _, c := range line {
		if c != '-' {
			return false
		}
	}
	return true
}

// RenderFrontmatterHTML writes the frontmatter metadata as a collapsible
// <details> element containing a <dl> definition list. Nested maps and
// arrays are rendered recursively.
//
// If metadata is nil or empty, nothing is written.
func RenderFrontmatterHTML(w io.Writer, metadata map[string]any, open bool) (err error) {
	if len(metadata) == 0 {
		return
	}

	if open {
		_, err = fmt.Fprint(w, "<details class=\"frontmatter\" open>\n")
	} else {
		_, err = fmt.Fprint(w, "<details class=\"frontmatter\">\n")
	}
	if err != nil {
		return
	}
	_, err = fmt.Fprint(w, "<summary>Properties</summary>\n")
	if err != nil {
		return
	}
	err = renderDL(w, metadata)
	if err != nil {
		return
	}
	_, err = fmt.Fprint(w, "</details>\n")
	return
}

func renderDL(w io.Writer, m map[string]any) (err error) {
	_, err = fmt.Fprint(w, "<dl>\n")
	if err != nil {
		return
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		_, err = fmt.Fprintf(w, "<dt>%s</dt>", htmlEscape(k))
		if err != nil {
			return
		}
		err = renderDD(w, m[k])
		if err != nil {
			return
		}
		_, err = fmt.Fprint(w, "\n")
		if err != nil {
			return
		}
	}

	_, err = fmt.Fprint(w, "</dl>\n")
	return
}

func renderDD(w io.Writer, val any) (err error) {
	switch v := val.(type) {
	case map[string]any:
		_, err = fmt.Fprint(w, "<dd>")
		if err != nil {
			return
		}
		err = renderDL(w, v)
		if err != nil {
			return
		}
		_, err = fmt.Fprint(w, "</dd>")
	case map[any]any:
		// yaml.v2 sometimes produces map[interface{}]interface{}
		normalized := make(map[string]any, len(v))
		for mk, mv := range v {
			normalized[fmt.Sprintf("%v", mk)] = mv
		}
		_, err = fmt.Fprint(w, "<dd>")
		if err != nil {
			return
		}
		err = renderDL(w, normalized)
		if err != nil {
			return
		}
		_, err = fmt.Fprint(w, "</dd>")
	case []any:
		_, err = fmt.Fprint(w, "<dd>")
		if err != nil {
			return
		}
		err = renderUL(w, v)
		if err != nil {
			return
		}
		_, err = fmt.Fprint(w, "</dd>")
	case nil:
		_, err = fmt.Fprint(w, "<dd></dd>")
	default:
		_, err = fmt.Fprintf(w, "<dd>%s</dd>", htmlEscape(fmt.Sprintf("%v", v)))
	}
	return
}

func renderUL(w io.Writer, items []any) (err error) {
	_, err = fmt.Fprint(w, "<ul>\n")
	if err != nil {
		return
	}
	for _, item := range items {
		switch v := item.(type) {
		case map[string]any:
			_, err = fmt.Fprint(w, "<li>")
			if err != nil {
				return
			}
			err = renderDL(w, v)
			if err != nil {
				return
			}
			_, err = fmt.Fprint(w, "</li>\n")
		case map[any]any:
			normalized := make(map[string]any, len(v))
			for mk, mv := range v {
				normalized[fmt.Sprintf("%v", mk)] = mv
			}
			_, err = fmt.Fprint(w, "<li>")
			if err != nil {
				return
			}
			err = renderDL(w, normalized)
			if err != nil {
				return
			}
			_, err = fmt.Fprint(w, "</li>\n")
		default:
			_, err = fmt.Fprintf(w, "<li>%s</li>\n", htmlEscape(fmt.Sprintf("%v", item)))
		}
		if err != nil {
			return
		}
	}
	_, err = fmt.Fprint(w, "</ul>\n")
	return
}

func htmlEscape(s string) string {
	var buf []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			buf = append(buf, "&amp;"...)
		case '<':
			buf = append(buf, "&lt;"...)
		case '>':
			buf = append(buf, "&gt;"...)
		case '"':
			buf = append(buf, "&quot;"...)
		default:
			buf = append(buf, s[i])
		}
	}
	return string(buf)
}
