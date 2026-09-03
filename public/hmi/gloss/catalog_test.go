package gloss

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The '@' gate and the slash gate, moved verbatim from ADR-0123's suite. The
// slash is what separates a declaration from a column that merely has an '@'
// in its name.
func TestParseColumnGates(t *testing.T) {
	c := Default()
	tests := []struct {
		name      string
		declared  bool
		label     string
		mediaType string
	}{
		// Declarations.
		{"notes@text/markdown", true, "notes", MediaTypeMarkdown},
		// keelson('adrcontent')'s source column (ADR-0092): a name that
		// arrives declared from a *table*, reaching this parser via a bare
		// `SELECT *`.
		{"content@text/markdown", true, "content", MediaTypeMarkdown},
		{"shot@image/png", true, "shot", MediaTypePNG},
		{"req@application/json", true, "req", MediaTypeJSON},
		{"q@application/sql", true, "q", MediaTypeSQL},
		{"src@text/x-go", true, "src", MediaTypeGo},
		{"stack@text/plain", true, "stack", MediaTypePlain},
		// The ADR-0186 presentation family.
		{"x@gloss/raw", true, "x", MediaTypeRaw},

		// Not declarations: no '@' at all.
		{"lane", false, "", ""},
		{"title", false, "", ""},
		{"cond_0", false, "", ""},
		// A '/' with no '@' is not a declaration either.
		{"a/b", false, "", ""},

		// Not declarations: an '@' but no '/' after it. These are the names
		// the slash gate exists to protect — ADR-0122's dot vocabulary above
		// all, which shares the separator but not the meaning.
		{"dot_done@success", false, "", ""},
		{"dot_cited@warning", false, "", ""},
		{"user@example.com", false, "", ""},
		{"weird@", false, "", ""},

		// Leeway physical names carry many colons and no '@' — untouched.
		{"tv:symbol:value:val:u64:g:1d0DV72:0:0::", false, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, declared := c.ParseColumn(tc.name)
			require.Equal(t, tc.declared, declared)
			if !declared {
				return
			}
			assert.Equal(t, tc.label, d.Label)
			assert.Equal(t, tc.mediaType, d.MediaType)
			assert.Empty(t, d.Reason, "a known type carries no reason")
			require.NotNil(t, d.Instance)
			assert.Equal(t, tc.mediaType, d.Instance.Gloss().MediaType())
		})
	}
}

// mime.ParseMediaType does the case-folding and parameter-splitting, so the
// parser does not have to.
func TestParseColumnCanonicalises(t *testing.T) {
	c := Default()
	d, declared := c.ParseColumn("notes@TEXT/Markdown")
	require.True(t, declared)
	assert.Equal(t, MediaTypeMarkdown, d.MediaType, "the media type is case-insensitive")
	require.NotNil(t, d.Instance)

	d, declared = c.ParseColumn("notes@text/markdown; charset=utf-8")
	require.True(t, declared)
	assert.Equal(t, MediaTypeMarkdown, d.MediaType, "parameters are split off the type")
	assert.Equal(t, "utf-8", d.Params[ParamCharset])
	assert.Empty(t, d.Reason, "charset is declared (and ignored) on the text types")
	require.NotNil(t, d.Instance)
}

// A declaration the catalog cannot honour is declared-with-a-reason, never
// silently plain. This is ADR-0123 §SD2's rule: the typo mode of a
// convention must not be a wrong-but-plausible render.
func TestParseColumnDiagnostics(t *testing.T) {
	c := Default()
	// A typo in a known type.
	d, declared := c.ParseColumn("notes@text/markdwn")
	require.True(t, declared, "it has a slash, so it meant to declare something")
	assert.Nil(t, d.Instance)
	assert.Contains(t, d.Reason, "unknown media type")
	assert.Contains(t, d.Reason, "text/markdwn")
	assert.Contains(t, d.Reason, MediaTypeMarkdown, "the reason lists the catalog")
	assert.Contains(t, d.Reason, MediaTypeRaw, "…all of it")

	// A type we deliberately do not carry a decoder for.
	d, declared = c.ParseColumn("logo@image/svg+xml")
	require.True(t, declared)
	assert.Nil(t, d.Instance)
	assert.Contains(t, d.Reason, "unknown media type")

	// A gloss typo is the same failure — the slash gate does not care which
	// family the token was aiming at.
	d, declared = c.ParseColumn("t@gloss/temperatur")
	require.True(t, declared)
	assert.Nil(t, d.Instance)
	assert.Contains(t, d.Reason, "unknown media type")

	// Malformed past ParseMediaType's tolerance.
	d, declared = c.ParseColumn("x@a/b/c")
	require.True(t, declared)
	assert.Nil(t, d.Instance)
	assert.Contains(t, d.Reason, "not a media type")

	// ';base64' is not a media parameter — a parameter is key=value, and the
	// data-URI spelling is a data-URI-ism. Recorded as a diagnostic rather
	// than quietly accepted.
	d, declared = c.ParseColumn("logo@image/png;base64")
	require.True(t, declared)
	assert.Nil(t, d.Instance)
	assert.Contains(t, d.Reason, "not a media type")
}

// Parameters are validated (ADR-0186 §SD2): an undeclared name is as loud as
// an unknown type, a reserved one is refused with the reason.
func TestParseColumnParameters(t *testing.T) {
	c := Default()
	d, declared := c.ParseColumn("notes@text/markdown;flavour=gfm")
	require.True(t, declared)
	assert.Nil(t, d.Instance)
	assert.Contains(t, d.Reason, "unknown parameter")
	assert.Contains(t, d.Reason, `"flavour"`)
	assert.Contains(t, d.Reason, ParamCharset, "the reason names what is declared")

	d, declared = c.ParseColumn("x@gloss/raw;anything=1")
	require.True(t, declared)
	assert.Nil(t, d.Instance)
	assert.Contains(t, d.Reason, "takes no parameters")

	d, declared = c.ParseColumn("shot@image/png;encoding=base64")
	require.True(t, declared)
	assert.Nil(t, d.Instance)
	assert.Contains(t, d.Reason, "reserved")
	assert.Contains(t, d.Reason, "base64")
}

// A declaration with nothing before the '@' still needs a caption.
func TestParseColumnEmptyLabel(t *testing.T) {
	d, declared := Default().ParseColumn("@text/markdown")
	require.True(t, declared)
	assert.Equal(t, "@text/markdown", d.Label, "an empty caption reads as a fault")
	assert.Equal(t, MediaTypeMarkdown, d.MediaType)
}

func TestCatalogRegisterRejectsDuplicates(t *testing.T) {
	c := NewCatalog()
	require.NoError(t, c.Register(rawGloss()))
	assert.Error(t, c.Register(rawGloss()), "a second registration of the same type is an error, not a replacement")
	g, ok := c.Lookup(MediaTypeRaw)
	require.True(t, ok)
	assert.Equal(t, MediaTypeRaw, g.MediaType())
	assert.Equal(t, 1, c.Len())
}

// Registration order is what the reject message and the affinity match walk,
// so it is pinned: content family first, in ADR-0123's order, then gloss/*.
func TestDefaultOrder(t *testing.T) {
	var order []string
	for g := range Default().All() {
		order = append(order, g.MediaType())
	}
	assert.Equal(t, []string{
		MediaTypeMarkdown, MediaTypePlain, MediaTypeJSON, MediaTypeSQL, MediaTypeGo,
		MediaTypePNG, MediaTypeJPEG, MediaTypeGIF,
		// Past ADR-0123's eight; a later content type appends here.
		MediaTypeCBOR,
	}, order[:9])
	assert.Contains(t, order, MediaTypeRaw)
}
