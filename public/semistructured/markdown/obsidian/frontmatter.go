package obsidian

import (
	"fmt"
	"io"
	"sort"

	"github.com/yuin/goldmark/parser"
	meta "github.com/yuin/goldmark-meta"
)

// GetFrontmatter extracts YAML frontmatter metadata from a parser.Context
// after rendering. Returns nil if no frontmatter was present.
func GetFrontmatter(pc parser.Context) map[string]interface{} {
	return meta.Get(pc)
}

// TryGetFrontmatter extracts YAML frontmatter metadata from a parser.Context
// after rendering. Returns an error if the YAML was malformed.
func TryGetFrontmatter(pc parser.Context) (metadata map[string]interface{}, err error) {
	metadata, err = meta.TryGet(pc)
	return
}

// RenderFrontmatterHTML writes the frontmatter metadata as a collapsible
// <details> element containing a <dl> definition list. Nested maps and
// arrays are rendered recursively.
//
// If metadata is nil or empty, nothing is written.
func RenderFrontmatterHTML(w io.Writer, metadata map[string]interface{}, open bool) (err error) {
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

func renderDL(w io.Writer, m map[string]interface{}) (err error) {
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

func renderDD(w io.Writer, val interface{}) (err error) {
	switch v := val.(type) {
	case map[string]interface{}:
		_, err = fmt.Fprint(w, "<dd>")
		if err != nil {
			return
		}
		err = renderDL(w, v)
		if err != nil {
			return
		}
		_, err = fmt.Fprint(w, "</dd>")
	case map[interface{}]interface{}:
		// yaml.v2 sometimes produces map[interface{}]interface{}
		normalized := make(map[string]interface{}, len(v))
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
	case []interface{}:
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

func renderUL(w io.Writer, items []interface{}) (err error) {
	_, err = fmt.Fprint(w, "<ul>\n")
	if err != nil {
		return
	}
	for _, item := range items {
		switch v := item.(type) {
		case map[string]interface{}:
			_, err = fmt.Fprint(w, "<li>")
			if err != nil {
				return
			}
			err = renderDL(w, v)
			if err != nil {
				return
			}
			_, err = fmt.Fprint(w, "</li>\n")
		case map[interface{}]interface{}:
			normalized := make(map[string]interface{}, len(v))
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
