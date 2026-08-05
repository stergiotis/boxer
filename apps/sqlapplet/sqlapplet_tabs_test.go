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

// landingTab reports which tab attenuation raised, via the launch composer —
// the only reader play exports for "the tab play itself raised".
func landingTab(t *testing.T, def *AppletDef) (id string) {
	t.Helper()
	inner := play.NewLivePlayApp(nil, "", appletMaxHistory)
	require.NoError(t, attenuateTabs(inner, def, zerolog.Nop()))
	return inner.ComposeLaunch().Tab
}

// The first tab a document lists is the one it opens on (ADR-0132 Update
// 2026-08-05). Without this the dock activates its leaf's own first tab, which
// is play's registration order and starts at `table` — so `bookcapmap/cap-map`
// opened on a grid of the columns feeding its treemap, and `component-deps`,
// `topology-map` and `profile-flame` each opened on something other than the
// panel their prose is about.
func TestAttenuateTabsOpensTheFirstListedTab(t *testing.T) {
	assert.Equal(t, "treemap", landingTab(t, &AppletDef{Slug: "map", Tabs: []TabSel{{ID: "treemap"}, {ID: "table"}}}))
	assert.Equal(t, "network", landingTab(t, &AppletDef{Slug: "graph", Tabs: []TabSel{{ID: "network"}, {ID: "table"}}}))
	// A side-zone tab is a legitimate landing: the zones each activate their
	// own first tab, so naming one raises it in its own leaf.
	assert.Equal(t, "detail", landingTab(t, &AppletDef{Slug: "card", Tabs: []TabSel{{ID: "detail"}, {ID: "table"}}}))
	// The common case is unchanged — a document that lists `table` first still
	// gets `table`, which is what every applet did before.
	assert.Equal(t, "table", landingTab(t, &AppletDef{Slug: "rows", Tabs: []TabSel{{ID: "table"}, {ID: "detail"}}}))
}

// `tabs: auto` declares no order, so there is nothing to honour and nothing is
// raised — inventing an opinion the document did not express would be worse
// than the dock's own default.
func TestAttenuateTabsAutoRaisesNothing(t *testing.T) {
	assert.Empty(t, landingTab(t, &AppletDef{Slug: "auto"}))
}
