package mdspan

import (
	"bytes"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SetFrontmatter sets and deletes top-level properties of src's frontmatter
// and leaves the other properties, their order and their comments as they
// were. A document without frontmatter gains a block when set is not empty;
// a block left with no property is removed. changed is the lines the new
// block occupies (the first line when it was removed).
func SetFrontmatter(src []byte, set map[string]any, del []string) (out []byte, changed LineSpan, err error) {
	all, body := frontmatterSpans(src)
	var doc yaml.Node
	if !all.IsEmpty() {
		if err = yaml.Unmarshal(src[body.Start:body.End], &doc); err != nil {
			err = eh.Errorf("frontmatter is not valid YAML; rewrite the block whole: %w", err)
			return
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		err = eb.Build().Int("kind", int(m.Kind)).Errorf("frontmatter is not a mapping of properties")
		return
	}
	for _, k := range del {
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == k {
				m.Content = slices.Delete(m.Content, i, i+2)
				break
			}
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		var v yaml.Node
		if err = v.Encode(set[k]); err != nil {
			err = eb.Build().Str("key", k).Errorf("encode property value: %w", err)
			return
		}
		replaced := false
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == k {
				v.HeadComment, v.LineComment = m.Content[i+1].HeadComment, m.Content[i+1].LineComment
				m.Content[i+1] = &v
				replaced = true
				break
			}
		}
		if !replaced {
			m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}, &v)
		}
	}
	var block []byte
	if len(m.Content) > 0 {
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err = enc.Encode(&doc); err != nil {
			err = eh.Errorf("encode frontmatter: %w", err)
			return
		}
		_ = enc.Close()
		block = make([]byte, 0, buf.Len()+8)
		block = append(block, "---\n"...)
		block = append(block, buf.Bytes()...)
		block = append(block, "---\n"...)
	}
	return Splice(src, all, block)
}
