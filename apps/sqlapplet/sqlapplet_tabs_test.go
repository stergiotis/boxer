package sqlapplet

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/apps/play"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The ADR-0132 §SD3/§SD4 tab surface, exercised against a real (client-less)
// PlayApp rather than against the id lists alone — the lists are only right if
// every id in them actually names a registered tab.

func attenuated(t *testing.T, def *AppletDef) (ids map[string]struct{}) {
	t.Helper()
	inner := play.NewLivePlayApp(nil, "", appletMaxHistory)
	require.NoError(t, attenuateTabs(inner, def, zerolog.Nop()))
	ids = make(map[string]struct{}, len(inner.Tabs().Specs()))
	for _, spec := range inner.Tabs().Specs() {
		ids[spec.ID] = struct{}{}
	}
	return
}

// Every tab play registers must be classified: chrome removed wholesale, or a
// result panel an explicit `tabs:` list can prune. A tab in NEITHER list
// survives attenuation unconditionally — it rides along on every applet window
// and no `tabs:` list can get rid of it.
//
// This is the test the Sankey panel needed and did not have: adding a body tab
// to play silently widened every applet's surface, and nothing failed.
func TestTabPolicyCoversEveryRegisteredTab(t *testing.T) {
	classified := make(map[string]struct{}, len(chromeTabIDs)+len(orderedResultTabIDs))
	for _, id := range chromeTabIDs {
		classified[id] = struct{}{}
	}
	for _, id := range orderedResultTabIDs {
		_, dup := classified[id]
		assert.False(t, dup, "tab %q is both chrome and a result panel", id)
		classified[id] = struct{}{}
	}
	for _, spec := range play.NewLivePlayApp(nil, "", appletMaxHistory).Tabs().Specs() {
		_, known := classified[spec.ID]
		assert.Truef(t, known, "play registers tab %q, which sqlapplet classifies as neither chrome nor a "+
			"result panel — it would survive attenuation on every applet and no `tabs:` list could prune it", spec.ID)
	}
}

// The result-panel allow-list and the ordered removal list are two spellings of
// one set; a slug in only one of them is either unprunable or unnameable.
func TestResultTabListsAgree(t *testing.T) {
	assert.Len(t, orderedResultTabIDs, len(resultTabIDs))
	for _, id := range orderedResultTabIDs {
		_, ok := resultTabIDs[id]
		assert.Truef(t, ok, "%q is removable but not nameable in `tabs:`", id)
	}
}

// The default-off panels are still result panels: nameable, prunable, and
// merely absent unless asked for.
func TestAutoOffPanelsAreStillResultPanels(t *testing.T) {
	require.NotEmpty(t, autoOffResultTabIDs)
	for _, id := range autoOffResultTabIDs {
		_, nameable := resultTabIDs[id]
		assert.Truef(t, nameable, "%q is default-off but cannot be asked for", id)
		assert.Containsf(t, orderedResultTabIDs, id, "%q is default-off but not prunable", id)
	}
}

// `tabs: auto` — chrome gone, the default-off panels gone, the rest left to
// negotiate their channels per frame.
func TestAttenuateTabsAutoDropsOnlyChromeAndDefaultOff(t *testing.T) {
	ids := attenuated(t, &AppletDef{Slug: "auto"})

	for _, id := range chromeTabIDs {
		assert.NotContainsf(t, ids, id, "chrome tab %q survived", id)
	}
	assert.NotContains(t, ids, "sankey", "Sankey is default-off: no applet's shape carries a `flows` CTE")
	assert.NotContains(t, ids, "dist", "Distribution is default-off: its columns come from the distsql macros")
	for _, id := range []string{"table", "detail", "network", "kanban", "timeline", "world", "projection", "schema", "icicle"} {
		assert.Containsf(t, ids, id, "auto must keep %q for the accept/reject contract to answer", id)
	}
}

// An explicit list pins the set: everything unlisted goes, including panels
// auto would have kept.
func TestAttenuateTabsExplicitListPrunes(t *testing.T) {
	ids := attenuated(t, &AppletDef{Slug: "pinned", Tabs: []TabSel{{ID: "table"}, {ID: "detail"}}})

	assert.Contains(t, ids, "table")
	assert.Contains(t, ids, "detail")
	for _, id := range []string{"sankey", "network", "kanban", "timeline", "world", "projection", "schema", "dist", "icicle"} {
		assert.NotContainsf(t, ids, id, "unlisted result panel %q survived an explicit `tabs:`", id)
	}
}

// Default-off is a default, not a ban: an applet whose buffer does carry the
// flow contract asks for the panel by name and gets it.
func TestAttenuateTabsExplicitListCanAskForADefaultOffPanel(t *testing.T) {
	ids := attenuated(t, &AppletDef{Slug: "flows", Tabs: []TabSel{{ID: "table"}, {ID: "sankey"}}})

	assert.Contains(t, ids, "sankey", "a named default-off panel must be kept")
	assert.Contains(t, ids, "table")
	assert.NotContains(t, ids, "network")
}
