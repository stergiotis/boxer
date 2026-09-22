package persiststore

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestKeysDoNotCollideAcrossNestedAppIds: app ids are import paths and
// nest, so the unescaped spelling could not tell (app "a/b", key "c") from
// (app "a", key "b/c"). Escaping every non-final segment keeps them apart.
func TestKeysDoNotCollideAcrossNestedAppIds(t *testing.T) {
	assert.NotEqual(t, StateKey("a/b", "c"), StateKey("a", "b/c"))
	assert.NotEqual(t, WorkingsetKey("a/b", "c"), WorkingsetKey("a", "b/c"))
	assert.NotEqual(t,
		ColumnWidthKey("a", "instance", "t/x", "k"),
		ColumnWidthKey("a", "instance", "t", "x/k"))
	// "%" is escaped first, so an id that already contains an escape
	// sequence stays distinct from the one it would otherwise mimic.
	assert.NotEqual(t, StateKey("a%2Fb", "c"), StateKey("a/b", "c"))
}

// TestAppPrefixSelectsOneApp: the sqlapplet app's prefix must not take in
// the applets nested under its id, and a prefix must not match a longer
// sibling id either.
func TestAppPrefixSelectsOneApp(t *testing.T) {
	const parent = "github.com/stergiotis/boxer/apps/sqlapplet"
	const child = parent + "/probe"
	for _, c := range []struct {
		prefix func(string) string
		key    func(string) string
	}{
		{StateAppPrefix, func(app string) string { return StateKey(app, "k") }},
		{WorkingsetAppPrefix, func(app string) string { return WorkingsetKey(app, "default") }},
		{ColumnWidthAppPrefix, func(app string) string { return ColumnWidthKey(app, "column", "", "abc") }},
	} {
		assert.True(t, strings.HasPrefix(c.key(parent), c.prefix(parent)))
		assert.True(t, strings.HasPrefix(c.key(child), c.prefix(child)))
		assert.False(t, strings.HasPrefix(c.key(child), c.prefix(parent)), "a nested app id must not fall under its parent's prefix")
		assert.False(t, strings.HasPrefix(c.key("a.bc"), c.prefix("a.b")), "a prefix must not match a longer sibling id")
	}
}

// TestKindPrefixesAreDisjoint: one key space, three kinds — no key of one
// kind may fall under another kind's prefix.
func TestKindPrefixesAreDisjoint(t *testing.T) {
	keys := map[string]string{
		StateKeyPrefix:       StateKey("app", "ws/x"),
		WorkingsetKeyPrefix:  WorkingsetKey("app", "state/x"),
		ColumnWidthKeyPrefix: ColumnWidthKey("app", "column", "", "k"),
	}
	for kind, key := range keys {
		for other := range keys {
			assert.Equal(t, kind == other, strings.HasPrefix(key, other), "%s under %s", key, other)
		}
	}
}
