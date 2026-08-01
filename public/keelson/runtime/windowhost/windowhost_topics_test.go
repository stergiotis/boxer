package windowhost

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func mkTopicManifest(id string, display string, topics ...app.TopicT) (m app.Manifest) {
	m = app.Manifest{
		Id:      app.AppIdT(id),
		Version: "0.1.0",
		Display: display,
		Topics:  topics,
		Surface: app.SurfaceWindowed,
	}
	return
}

// --- groupByTopic (ADR-0158 §SD3) -------------------------------------------

func TestGroupByTopic_EmptyInputReturnsNil(t *testing.T) {
	groups := groupByTopic(nil, 0)
	assert.Empty(t, groups)
}

func TestGroupByTopic_SectionsFollowVocabularyOrder(t *testing.T) {
	// Scrambled input; sections must come back in app.AllTopics order.
	// Ordering is the vocabulary's, not a launcher-local list — closing the
	// vocabulary is what removed the need for one.
	in := []app.Manifest{
		mkTopicManifest("a", "A", app.TopicUi),
		mkTopicManifest("b", "B", app.TopicCode),
		mkTopicManifest("c", "C", app.TopicRuntime),
	}
	assert.Equal(t,
		[]app.TopicT{app.TopicRuntime, app.TopicCode, app.TopicUi},
		topicsOf(groupByTopic(in, 0)))
}

func TestGroupByTopic_ManifestAppearsInEverySectionItDeclares(t *testing.T) {
	// The load-bearing §SD3 behaviour: multi-placement, no primary topic.
	in := []app.Manifest{
		mkTopicManifest("multi", "Multi", app.TopicCode, app.TopicTopology),
		mkTopicManifest("solo", "Solo", app.TopicCode),
	}
	groups := groupByTopic(in, 0)
	require.Equal(t, []app.TopicT{app.TopicCode, app.TopicTopology}, topicsOf(groups))

	code := findGroup(t, groups, app.TopicCode)
	assert.Equal(t, []string{"Multi", "Solo"}, displaysOf(code.Manifests))

	topo := findGroup(t, groups, app.TopicTopology)
	assert.Equal(t, []string{"Multi"}, displaysOf(topo.Manifests),
		"the same manifest must also appear under its second topic")
}

func TestGroupByTopic_EmptySectionsOmitted(t *testing.T) {
	// A vocabulary member no app carries must not render as a blank section.
	groups := groupByTopic([]app.Manifest{mkTopicManifest("a", "A", app.TopicGeo)}, 0)
	assert.Equal(t, []app.TopicT{app.TopicGeo}, topicsOf(groups))
}

func TestGroupByTopic_ManifestWithNoTopicsAppearsNowhere(t *testing.T) {
	// Validate refuses this for windowed apps, so it can only arise for a
	// headless one — which has no window to open. Pin that it is dropped
	// rather than routed to a catch-all: ADR-0158 removes that bucket.
	in := []app.Manifest{
		mkTopicManifest("none", "None"),
		mkTopicManifest("has", "Has", app.TopicData),
	}
	groups := groupByTopic(in, 0)
	require.Len(t, groups, 1)
	assert.Equal(t, []string{"Has"}, displaysOf(groups[0].Manifests))
}

func TestGroupByTopic_WithinSectionSortsByDisplay(t *testing.T) {
	in := []app.Manifest{
		mkTopicManifest("c", "Charlie", app.TopicData),
		mkTopicManifest("a", "Alpha", app.TopicData),
		mkTopicManifest("b", "Bravo", app.TopicData),
	}
	groups := groupByTopic(in, 0)
	require.Len(t, groups, 1)
	assert.Equal(t, []string{"Alpha", "Bravo", "Charlie"}, displaysOf(groups[0].Manifests))
}

func TestGroupByTopic_TieBrokenByIdWhenDisplayIdentical(t *testing.T) {
	in := []app.Manifest{
		mkTopicManifest("zeta", "Same", app.TopicData),
		mkTopicManifest("alpha", "Same", app.TopicData),
	}
	groups := groupByTopic(in, 0)
	require.Len(t, groups, 1)
	assert.Equal(t, []app.AppIdT{"alpha", "zeta"},
		[]app.AppIdT{groups[0].Manifests[0].Id, groups[0].Manifests[1].Id})
}

// --- filterManifests / matchManifestSearch (ADR-0158 §SD6) ------------------

func TestFilterManifests_EmptyQueryReturnsInput(t *testing.T) {
	in := []app.Manifest{mkTopicManifest("a", "A", app.TopicData)}
	assert.Equal(t, in, filterManifests(in, launcherFilter{query: ""}))
	assert.Equal(t, in, filterManifests(in, launcherFilter{query: "   \t "}))
}

func TestFilterManifests_CaseInsensitiveDisplayMatch(t *testing.T) {
	in := []app.Manifest{
		mkTopicManifest("a", "SQL Playground", app.TopicSql),
		mkTopicManifest("b", "Log viewer", app.TopicRuntime),
	}
	assert.Equal(t, []string{"SQL Playground"}, displaysOf(filterManifests(in, launcherFilter{query: "playGROUND"})))
}

func TestFilterManifests_TopicMatchSurfacesEveryAppOnThatSubject(t *testing.T) {
	// The ADR's §4 diagnosis as a test: entries sharing a subject but not an
	// implementation technique must come back together.
	in := []app.Manifest{
		mkTopicManifest("explorer", "Go dependency explorer", app.TopicCode),
		mkTopicManifest("applet", "Go packages", app.TopicCode),
		mkTopicManifest("other", "Terrain scope", app.TopicGeo),
	}
	assert.Equal(t,
		[]string{"Go dependency explorer", "Go packages"},
		displaysOf(filterManifests(in, launcherFilter{query: "code"})))
}

func TestFilterManifests_KeywordMatchesWhatTheNameDoesNot(t *testing.T) {
	// The §SD4 payoff: "cpu" reaches a process monitor whose display name
	// contains no such word.
	m := mkTopicManifest("imztop", "imztop", app.TopicObservability)
	m.Keywords = []string{"top", "htop", "process", "cpu", "memory"}
	in := []app.Manifest{m, mkTopicManifest("other", "Fibscope", app.TopicData)}

	for _, q := range []string{"cpu", "htop", "process"} {
		assert.Equal(t, []string{"imztop"}, displaysOf(filterManifests(in, launcherFilter{query: q})),
			"keyword %q must reach the app", q)
	}
}

func TestFilterManifests_SubsequenceMatchOnDisplay(t *testing.T) {
	in := []app.Manifest{
		mkTopicManifest("a", "Go dependency explorer", app.TopicCode),
		mkTopicManifest("b", "Terrain scope", app.TopicGeo),
	}
	assert.Equal(t, []string{"Go dependency explorer"}, displaysOf(filterManifests(in, launcherFilter{query: "gdep"})))
}

func TestFilterManifests_NoMatchReturnsEmpty(t *testing.T) {
	in := []app.Manifest{mkTopicManifest("a", "Alpha", app.TopicData)}
	assert.Empty(t, filterManifests(in, launcherFilter{query: "zzzz"}))
}

func TestFilterManifests_PreservesInputOrder(t *testing.T) {
	in := []app.Manifest{
		mkTopicManifest("c", "Charlie", app.TopicData),
		mkTopicManifest("a", "Alpha", app.TopicData),
	}
	assert.Equal(t, []string{"Charlie", "Alpha"}, displaysOf(filterManifests(in, launcherFilter{query: "a"})))
}

func TestFilterManifests_IdNotMatched(t *testing.T) {
	// Every id contains "github", so matching on it would return the whole
	// registry for a common substring.
	in := []app.Manifest{mkTopicManifest("github.com/stergiotis/boxer/apps/x", "Alpha", app.TopicData)}
	assert.Empty(t, filterManifests(in, launcherFilter{query: "stergiotis"}))
}

func TestMatchesSubsequence(t *testing.T) {
	cases := []struct {
		haystack string
		needle   string
		want     bool
	}{
		{"go dependency explorer", "gdep", true},
		{"go dependency explorer", "god", true},
		{"go dependency explorer", "gz", false},
		// Right letters, wrong order — subsequence is order-sensitive.
		{"go dependency explorer", "xg", false},
		{"abc", "", true},
		{"abc", "abcd", false},
		{"abc", "cba", false},
		// Rune-wise, so a multi-byte glyph is never matched by half of it.
		{"sträße", "stäe", true},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, matchesSubsequence(tc.haystack, tc.needle),
			"matchesSubsequence(%q, %q)", tc.haystack, tc.needle)
	}
}

func TestTopicSuffix(t *testing.T) {
	assert.Equal(t, "", topicSuffix(mkTopicManifest("a", "A")))
	assert.Equal(t, "code", topicSuffix(mkTopicManifest("a", "A", app.TopicCode)))
	assert.Equal(t, "code · topology",
		topicSuffix(mkTopicManifest("a", "A", app.TopicCode, app.TopicTopology)))
}

func TestSortManifestsByDisplay_DisplayThenId(t *testing.T) {
	in := []app.Manifest{
		mkTopicManifest("z", "Same", app.TopicData),
		mkTopicManifest("b", "Alpha", app.TopicData),
		mkTopicManifest("a", "Same", app.TopicData),
	}
	sortManifestsByDisplay(in)
	assert.Equal(t, []string{"Alpha", "Same", "Same"}, displaysOf(in))
	assert.Equal(t, app.AppIdT("a"), in[1].Id)
	assert.Equal(t, app.AppIdT("z"), in[2].Id)
}

// --- helpers ----------------------------------------------------------------

func topicsOf(groups []manifestGroup) (topics []app.TopicT) {
	topics = make([]app.TopicT, 0, len(groups))
	for _, g := range groups {
		topics = append(topics, g.Topic)
	}
	return
}

func displaysOf(manifests []app.Manifest) (displays []string) {
	displays = make([]string, 0, len(manifests))
	for _, m := range manifests {
		displays = append(displays, m.Display)
	}
	return
}

func findGroup(t *testing.T, groups []manifestGroup, topic app.TopicT) (g manifestGroup) {
	t.Helper()
	for _, x := range groups {
		if x.Topic == topic {
			g = x
			return
		}
	}
	t.Fatalf("no section for topic %q", topic)
	return
}

// --- kindFilterT (ADR-0158 §SD5/§SD6) ---------------------------------------

func mkKindManifest(id string, display string, kind app.KindE) (m app.Manifest) {
	m = mkTopicManifest(id, display, app.TopicRuntime)
	m.Kind = kind
	return
}

func TestKindFilter_ZeroValueShowsEverything(t *testing.T) {
	// The zero value must be "hide nothing", so a host that never touches
	// the filter needs no initialisation.
	var f kindFilterT
	for _, k := range app.AllKinds {
		assert.True(t, f.shows(k), "the zero filter must show %s", k)
	}
	assert.False(t, f.hidesAnything())
}

func TestKindFilter_ToggleHidesAndRestores(t *testing.T) {
	f := kindFilterT(0).toggled(app.KindDemo)
	assert.False(t, f.shows(app.KindDemo))
	assert.True(t, f.shows(app.KindApp), "hiding one kind must not touch the others")
	assert.True(t, f.shows(app.KindApplet))
	assert.True(t, f.hidesAnything())

	assert.False(t, f.toggled(app.KindDemo).hidesAnything(), "toggling back must return to inert")
}

func TestKindFilter_EverythingHiddenIsNotEverythingShown(t *testing.T) {
	// The reason the mask stores hidden rather than shown kinds: with a
	// shown-set mask under a zero-means-all convention, hiding all three
	// would wrap around to showing all three.
	f := kindFilterT(0)
	for _, k := range app.AllKinds {
		f = f.toggled(k)
	}
	for _, k := range app.AllKinds {
		assert.False(t, f.shows(k), "%s must stay hidden", k)
	}
	assert.True(t, f.hidesAnything())
}

func TestFilterManifests_KindFilterAppliesWithoutAQuery(t *testing.T) {
	// The toggles govern the sectioned browse view too, not just search
	// hits — otherwise hiding demos would only work while typing.
	in := []app.Manifest{
		mkKindManifest("a", "Plain app", app.KindApp),
		mkKindManifest("b", "An applet", app.KindApplet),
		mkKindManifest("c", "A demo", app.KindDemo),
	}
	hidden := kindFilterT(0).toggled(app.KindDemo)
	assert.Equal(t, []string{"Plain app", "An applet"},
		displaysOf(filterManifests(in, launcherFilter{kinds: hidden})))
}

func TestFilterManifests_KindAndQueryCompose(t *testing.T) {
	in := []app.Manifest{
		mkKindManifest("a", "Widget gallery", app.KindDemo),
		mkKindManifest("b", "Widget inspector", app.KindApp),
	}
	hidden := kindFilterT(0).toggled(app.KindDemo)
	assert.Equal(t, []string{"Widget inspector"},
		displaysOf(filterManifests(in, launcherFilter{query: "widget", kinds: hidden})),
		"the query matches both; the kind filter must still remove one")
}

func TestKindLabel_PluralBecauseItGovernsASet(t *testing.T) {
	assert.Equal(t, "Apps", kindLabel(app.KindApp))
	assert.Equal(t, "Applets", kindLabel(app.KindApplet))
	assert.Equal(t, "Demos", kindLabel(app.KindDemo))
	// An unknown kind still renders something rather than an empty toggle.
	assert.NotEmpty(t, kindLabel(app.KindE(99)))
}

func TestEmptyResultHint_NamesTheFilterWhenItIsTheCause(t *testing.T) {
	// A bare "(no matches)" while a toggle is off reads as "the app is
	// gone" rather than "you hid it".
	inst := &Inst{}
	assert.Equal(t, "(no matches)", inst.emptyResultHint())
	inst.ensureKindShown()
	inst.kindShown[indexOfKind(t, app.KindDemo)] = false
	assert.Contains(t, inst.emptyResultHint(), "hidden")
}

func TestKindFilter_ZeroInstShowsEverything(t *testing.T) {
	// The uninitialised host must not start out hiding every app — the
	// failure mode a zero-valued "shown" slice would produce.
	inst := &Inst{}
	assert.False(t, inst.kindFilter().hidesAnything())
	for _, k := range app.AllKinds {
		assert.True(t, inst.kindFilter().shows(k))
	}
}

func TestKindFilter_DerivesFromToggleState(t *testing.T) {
	// The toggles are stored as a []bool because Checkbox.SendRespVal is
	// deferred to StateManager.Sync and needs a stable address; this pins
	// that the mask the filter uses actually tracks that slice.
	inst := &Inst{}
	inst.ensureKindShown()
	for _, k := range app.AllKinds {
		assert.True(t, inst.kindFilter().shows(k), "%s starts shown", k)
	}
	inst.kindShown[indexOfKind(t, app.KindApplet)] = false
	assert.False(t, inst.kindFilter().shows(app.KindApplet))
	assert.True(t, inst.kindFilter().shows(app.KindApp))
	assert.True(t, inst.kindFilter().shows(app.KindDemo))
}

func TestEnsureKindShown_Idempotent(t *testing.T) {
	inst := &Inst{}
	inst.ensureKindShown()
	inst.kindShown[0] = false
	inst.ensureKindShown()
	assert.False(t, inst.kindShown[0], "a second call must not wipe user state")
}

// indexOfKind maps a kind to its position in app.AllKinds — the same
// positional convention kindShown uses, so the tests do not assume KindE
// values are contiguous from zero either.
func indexOfKind(t *testing.T, k app.KindE) (idx int) {
	t.Helper()
	for i, x := range app.AllKinds {
		if x == k {
			idx = i
			return
		}
	}
	t.Fatalf("kind %s not in AllKinds", k)
	return
}

// --- topicFilterT (ADR-0158 §SD6) -------------------------------------------

func TestTopicFilter_ZeroValueMeansNoRestriction(t *testing.T) {
	// The opposite polarity to kindFilterT — nothing selected must mean
	// "show everything", not "show nothing".
	var f topicFilterT
	assert.True(t, f.isInert())
	for _, tp := range app.AllTopics {
		assert.True(t, f.shows(tp), "an inert topic filter must show %s", tp)
	}
	assert.True(t, f.showsAny(nil), "even a manifest with no topics passes an inert filter")
}

func TestTopicFilter_SelectionRestricts(t *testing.T) {
	idx, ok := topicIndex(app.TopicCode)
	require.True(t, ok)
	f := topicFilterT(0).toggledAt(idx)

	assert.False(t, f.isInert())
	assert.True(t, f.shows(app.TopicCode))
	assert.False(t, f.shows(app.TopicGeo))
	assert.False(t, f.showsAny(nil), "a manifest with no topics cannot satisfy a restriction")
}

func TestTopicFilter_SeveralSelectionsUnion(t *testing.T) {
	ci, _ := topicIndex(app.TopicCode)
	gi, _ := topicIndex(app.TopicGeo)
	f := topicFilterT(0).toggledAt(ci).toggledAt(gi)

	assert.True(t, f.shows(app.TopicCode))
	assert.True(t, f.shows(app.TopicGeo))
	assert.False(t, f.shows(app.TopicSql))
}

func TestTopicFilter_ShowsAnyIsManifestLevel(t *testing.T) {
	// A manifest passes if *any* of its topics is selected — otherwise a
	// two-topic app would vanish whenever you filtered to one of them.
	ci, _ := topicIndex(app.TopicCode)
	f := topicFilterT(0).toggledAt(ci)
	assert.True(t, f.showsAny([]app.TopicT{app.TopicTopology, app.TopicCode}))
	assert.False(t, f.showsAny([]app.TopicT{app.TopicTopology, app.TopicGeo}))
}

func TestTopicFilter_UnregisteredTopicNeverPassesARestriction(t *testing.T) {
	ci, _ := topicIndex(app.TopicCode)
	f := topicFilterT(0).toggledAt(ci)
	assert.False(t, f.shows(app.TopicT("nonsense")), "an unknown topic has no position to select")
}

func TestTopicFilter_MaskWidthCoversTheVocabulary(t *testing.T) {
	// topicFilterT is a uint32 mask over app.AllTopics positions. If the
	// vocabulary ever outgrows it, selections past the boundary would be
	// silently dropped rather than failing loudly.
	assert.LessOrEqual(t, len(app.AllTopics), 32,
		"topicFilterT must widen before the vocabulary passes 32 members")
}

func TestGroupByTopic_ChipsDropWholeSections(t *testing.T) {
	// The subtle one: a manifest under two topics passes a manifest-level
	// filter, so without section-level filtering it would still render
	// under its *unselected* topic.
	in := []app.Manifest{mkTopicManifest("multi", "Multi", app.TopicCode, app.TopicTopology)}
	ci, _ := topicIndex(app.TopicCode)
	only := topicFilterT(0).toggledAt(ci)

	groups := groupByTopic(in, only)
	require.Equal(t, []app.TopicT{app.TopicCode}, topicsOf(groups),
		"the topology section must not render while only code is selected")
	assert.Equal(t, []string{"Multi"}, displaysOf(groups[0].Manifests))
}

func TestFilterManifests_TopicChipsNarrowWithoutAQuery(t *testing.T) {
	in := []app.Manifest{
		mkTopicManifest("a", "Go packages", app.TopicCode),
		mkTopicManifest("b", "Terrain scope", app.TopicGeo),
	}
	ci, _ := topicIndex(app.TopicCode)
	f := launcherFilter{topics: topicFilterT(0).toggledAt(ci)}
	assert.Equal(t, []string{"Go packages"}, displaysOf(filterManifests(in, f)))
}

func TestFilterManifests_AllThreeAxesCompose(t *testing.T) {
	// Query, kind, and topic together — §SD6's "one filter state" claim as
	// a test.
	code := mkTopicManifest("a", "Go packages", app.TopicCode)
	code.Kind = app.KindApplet
	demo := mkTopicManifest("b", "Go playground demo", app.TopicCode)
	demo.Kind = app.KindDemo
	geo := mkTopicManifest("c", "Go terrain", app.TopicGeo)

	ci, _ := topicIndex(app.TopicCode)
	f := launcherFilter{
		query:  "go",
		kinds:  kindFilterT(0).toggled(app.KindDemo),
		topics: topicFilterT(0).toggledAt(ci),
	}
	// "go" matches all three; the demo is hidden by kind, the geo entry by
	// topic. Only the applet survives.
	assert.Equal(t, []string{"Go packages"},
		displaysOf(filterManifests([]app.Manifest{code, demo, geo}, f)))
}

func TestLauncherFilter_IsInert(t *testing.T) {
	assert.True(t, launcherFilter{}.isInert())
	assert.True(t, launcherFilter{query: "  \t "}.isInert(), "a whitespace query restricts nothing")
	assert.False(t, launcherFilter{query: "x"}.isInert())
	assert.False(t, launcherFilter{kinds: kindFilterT(0).toggled(app.KindDemo)}.isInert())
	assert.False(t, launcherFilter{topics: topicFilterT(1)}.isInert())
}

func TestEmptyResultHint_NamesWhichFilterIsResponsible(t *testing.T) {
	inst := &Inst{}
	assert.Equal(t, "(no matches)", inst.emptyResultHint())

	inst.topicFilter = topicFilterT(1)
	assert.Contains(t, inst.emptyResultHint(), "topics")

	inst.ensureKindShown()
	inst.kindShown[indexOfKind(t, app.KindDemo)] = false
	assert.Contains(t, inst.emptyResultHint(), "both")
}
