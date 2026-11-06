package obsidian

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// fullParseFrontmatter is the reference ParseFrontmatter reproduces: a
// complete goldmark parse with the frontmatter extension on.
func fullParseFrontmatter(src []byte) (metadata map[string]any, err error) {
	gm := New(Options{Features: FeatureAll})
	pc := NewParserContext()
	gm.Parser().Parse(text.NewReader(src), parser.WithContext(pc))
	metadata, err = TryGetFrontmatter(pc)
	return
}

func TestParseFrontmatter_AgreesWithAFullParse(t *testing.T) {
	cases := map[string]string{
		"none":             "# Title\n\nBody.\n",
		"empty source":     "",
		"plain":            "---\ntitle: T\nsummary: \"S\"\n---\n\n# H\n",
		"empty block":      "---\n---\nbody\n",
		"crlf":             "---\r\ntitle: T\r\n---\r\nbody\r\n",
		"long separators":  "-----\ntitle: T\n-----\nbody\n",
		"indented opener":  "  ---\ntitle: T\n---\n",
		"too indented":     "    ---\ntitle: T\n---\n",
		"spaced closer":    "---\ntitle: T\n  ---  \nbody\n",
		"blank line kept":  "---\ntitle: T\n\nsummary: S\n---\n",
		"unclosed":         "---\ntitle: T\nsummary: S\n",
		"not first line":   "\n---\ntitle: T\n---\n",
		"list not opener":  "- a\n- b\n",
		"nested values":    "---\ntopics: [runtime, data]\ntabs:\n  - table\n  - \"detail@side\"\nmap:\n  k: 1\n  2: two\n---\n",
		"scalars":          "---\nn: 3\nf: 1.5\nb: true\nnil: ~\nd: 2026-11-06\n---\n",
		"dashes in body":   "---\ntitle: T\n---\n\n---\nsecond: block\n---\n",
		"malformed yaml":   "---\ntitle: [unclosed\n---\nbody\n",
		"closer at eof":    "---\ntitle: T\n---",
		"opener only":      "---",
		"thematic in body": "Para\n\n---\n\nMore\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			want, wantErr := fullParseFrontmatter([]byte(src))
			got, found, gotErr := ParseFrontmatter([]byte(src))
			if wantErr != nil {
				require.Error(t, gotErr, "the full parse rejects this YAML")
				assert.Nil(t, got)
				assert.True(t, found)
				return
			}
			require.NoError(t, gotErr)
			if want == nil {
				assert.Empty(t, got, "no frontmatter in a full parse")
				return
			}
			assert.True(t, found)
			assert.Equal(t, want, got)
		})
	}
}
