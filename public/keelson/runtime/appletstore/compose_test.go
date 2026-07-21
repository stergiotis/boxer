package appletstore_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
)

func TestComposeAppletDocShape(t *testing.T) {
	doc, err := appletstore.ComposeAppletDoc("My applet", "🧩", "introspection", "SELECT 1")
	require.NoError(t, err)
	s := string(doc)
	assert.True(t, strings.HasPrefix(s, "---\n"), "frontmatter first")
	assert.Contains(t, s, `title: "My applet"`)
	assert.Contains(t, s, `icon: "🧩"`)
	assert.Contains(t, s, "type: reference")
	assert.Contains(t, s, "```sql\nSELECT 1\n```")
	assert.Contains(t, s, `endpoint: "introspection"`)

	// Icon is optional.
	doc, err = appletstore.ComposeAppletDoc("Plain", "", "", "SELECT 1")
	require.NoError(t, err)
	assert.NotContains(t, string(doc), "icon:")
	assert.NotContains(t, string(doc), "endpoint:")
}

func TestComposeAppletDocGuards(t *testing.T) {
	_, err := appletstore.ComposeAppletDoc("", "", "", "SELECT 1")
	require.Error(t, err, "title required")

	_, err = appletstore.ComposeAppletDoc("T", "", "", "   ")
	require.Error(t, err, "empty buffer")

	_, err = appletstore.ComposeAppletDoc("T", "", "", "SELECT 1\n``` evil\nSELECT 2")
	require.Error(t, err, "a fence line would break the document")
	assert.Contains(t, err.Error(), "fence")
}
