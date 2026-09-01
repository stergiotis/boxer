package launcher

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
		Summary: "fixture summary",
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

// --- filterManifests (ADR-0158 §SD6, matching via ADR-0164 §SD2) ------------

func TestFilterManifests_EmptyQueryReturnsInput(t *testing.T) {
	in := []app.Manifest{mkTopicManifest("a", "A", app.TopicData)}
	assert.Equal(t, in, filterManifests(in, filterT{query: ""}, nil))
	assert.Equal(t, in, filterManifests(in, filterT{query: "   \t "}, nil))
}

func TestFilterManifests_CaseInsensitiveDisplayMatch(t *testing.T) {
	in := []app.Manifest{
		mkTopicManifest("a", "SQL Playground", app.TopicSql),
		mkTopicManifest("b", "Log viewer", app.TopicRuntime),
	}
	assert.Equal(t, []string{"SQL Playground"}, displaysOf(filterManifests(in, filterT{query: "playGROUND"}, nil)))
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
		displaysOf(filterManifests(in, filterT{query: "code"}, nil)))
}

func TestFilterManifests_KeywordMatchesWhatTheNameDoesNot(t *testing.T) {
	// The §SD4 payoff: "cpu" reaches a process monitor whose display name
	// contains no such word.
	m := mkTopicManifest("imztop", "imztop", app.TopicObservability)
	m.Keywords = []string{"top", "htop", "process", "cpu", "memory"}
	in := []app.Manifest{m, mkTopicManifest("other", "Fibscope", app.TopicData)}

	for _, q := range []string{"cpu", "htop", "process"} {
		assert.Equal(t, []string{"imztop"}, displaysOf(filterManifests(in, filterT{query: q}, nil)),
			"keyword %q must reach the app", q)
	}
}

func TestFilterManifests_NoMatchReturnsEmpty(t *testing.T) {
	in := []app.Manifest{mkTopicManifest("a", "Alpha", app.TopicData)}
	assert.Empty(t, filterManifests(in, filterT{query: "zzzz"}, nil))
}

func TestFilterManifests_IdNotMatched(t *testing.T) {
	// Every id contains "github", so matching on it would return the whole
	// registry for a common substring.
	in := []app.Manifest{mkTopicManifest("github.com/stergiotis/boxer/apps/x", "Alpha", app.TopicData)}
	assert.Empty(t, filterManifests(in, filterT{query: "stergiotis"}, nil))
}

// --- the battery query model (ADR-0164 §SD2) --------------------------------

func TestFilterManifests_RegexTokenMatches(t *testing.T) {
	in := []app.Manifest{
		mkTopicManifest("a", "Go dependency explorer", app.TopicCode),
		mkTopicManifest("b", "Terrain scope", app.TopicGeo),
		mkTopicManifest("c", "Log viewer", app.TopicRuntime),
	}
	// Anchors, alternation, and wildcards are the point of the battery: the
	// elision the retired subsequence tier guessed at is now written down.
	assert.Equal(t, []string{"Go dependency explorer"},
		displaysOf(filterManifests(in, filterT{query: `g.*dep`}, nil)))
	assert.Equal(t, []string{"Go dependency explorer", "Terrain scope"},
		displaysOf(filterManifests(in, filterT{query: `explorer|scope`}, nil)))
	assert.Equal(t, []string{"Log viewer"},
		displaysOf(filterManifests(in, filterT{query: `^log`}, nil)))
}

func TestFilterManifests_SubsequenceNoLongerMatches(t *testing.T) {
	// The behaviour ADR-0158 §SD6 shipped and its 2026-08-06 Update retired:
	// a plain token is a substring pattern, never an elision. Guarding it so
	// a future "helpful" fuzzy tier has to argue with a test first.
	in := []app.Manifest{mkTopicManifest("a", "Go dependency explorer", app.TopicCode)}
	assert.Empty(t, filterManifests(in, filterT{query: "gdep"}, nil))
}

func TestFilterManifests_SpaceMeansAnd(t *testing.T) {
	in := []app.Manifest{
		mkTopicManifest("a", "Go dependency explorer", app.TopicCode),
		mkTopicManifest("b", "Go packages", app.TopicCode),
	}
	// Every token must hit some field — and the fields differ per token, so
	// "code" (a topic) AND "explorer" (the display name) is satisfiable.
	assert.Equal(t, []string{"Go dependency explorer"},
		displaysOf(filterManifests(in, filterT{query: "code explorer"}, nil)))
	// Order across tokens carries no meaning, unlike a single substring.
	assert.Equal(t, []string{"Go dependency explorer"},
		displaysOf(filterManifests(in, filterT{query: "dependency go"}, nil)))
	assert.Empty(t, filterManifests(in, filterT{query: "go zzzz"}, nil))
}

func TestFilterManifests_UncompilablePatternDegradesToLiteral(t *testing.T) {
	// A half-typed pattern must keep matching as text rather than erroring
	// or matching nothing mid-keystroke (ADR-0164 §SD2).
	m := mkTopicManifest("a", "quantile(0.99) inspector", app.TopicData)
	in := []app.Manifest{m, mkTopicManifest("b", "Log viewer", app.TopicRuntime)}
	assert.Equal(t, []string{"quantile(0.99) inspector"},
		displaysOf(filterManifests(in, filterT{query: "quantile("}, nil)))

	b := launcherBattery("quantile(")
	require.Len(t, b.Patterns, 1)
	assert.True(t, b.Patterns[0].Literal, "the degradation must be reported, not silent")
}

// --- ranking (ADR-0158 §SD6; frecency stays deferred under §SD10) -----------

func TestFilterManifests_RanksDisplayOverTopicOverKeyword(t *testing.T) {
	byKeyword := mkTopicManifest("kw", "Repo explorer", app.TopicUi)
	byKeyword.Keywords = []string{"code"}
	in := []app.Manifest{
		byKeyword,
		mkTopicManifest("topic", "Go packages", app.TopicCode),
		mkTopicManifest("display", "Code volume", app.TopicUi),
	}
	assert.Equal(t,
		[]string{"Code volume", "Go packages", "Repo explorer"},
		displaysOf(filterManifests(in, filterT{query: "code"}, nil)))
}

func TestFilterManifests_TiesBreakByDisplayThenId(t *testing.T) {
	// Equal scores fall back to exactly where the unranked browse sections
	// would have put them.
	in := []app.Manifest{
		mkTopicManifest("z", "Same", app.TopicData),
		mkTopicManifest("c", "Charlie", app.TopicData),
		mkTopicManifest("a", "Same", app.TopicData),
	}
	hits := filterManifests(in, filterT{query: "a"}, nil)
	assert.Equal(t, []string{"Charlie", "Same", "Same"}, displaysOf(hits))
	assert.Equal(t, []app.AppIdT{"c", "a", "z"},
		[]app.AppIdT{hits[0].Id, hits[1].Id, hits[2].Id})
}

func TestScoreManifest_StrongestTierWinsAndTiersDoNotAdd(t *testing.T) {
	// One pattern hitting several fields still counts once, at the strongest
	// tier — mirroring help/search's per-pattern scoring so the two
	// executors cannot drift into different answers.
	m := mkTopicManifest("a", "code volume", app.TopicCode)
	m.Keywords = []string{"code"}
	b := launcherBattery("code")
	score, ok := scoreManifest(m, &b)
	require.True(t, ok)
	assert.Equal(t, weightManifestDisplay, score)

	// Two tokens, two tiers, and now they do add — per pattern, not per field.
	b2 := launcherBattery("volume code")
	score2, ok2 := scoreManifest(m, &b2)
	require.True(t, ok2)
	assert.Equal(t, 2*weightManifestDisplay, score2)
}

func TestFilterManifests_FacetOnlyFilterKeepsInputOrder(t *testing.T) {
	// Ranking is the query path's alone: with no query there are no scores
	// to rank by, and the caller (groupByTopic) owns the sort.
	ci, _ := topicIndex(app.TopicCode)
	in := []app.Manifest{
		mkTopicManifest("c", "Charlie", app.TopicCode),
		mkTopicManifest("a", "Alpha", app.TopicCode),
		mkTopicManifest("g", "Gamma", app.TopicGeo),
	}
	assert.Equal(t, []string{"Charlie", "Alpha"},
		displaysOf(filterManifests(in, filterT{topics: topicFilterT(0).toggledAt(ci)}, nil)))
}

func TestScoreManifest_EmptyBatteryQualifiesNothing(t *testing.T) {
	// IsZero is the "no query" state: the browse view, never an all-corpus
	// dump under a zero score.
	m := mkTopicManifest("a", "Alpha", app.TopicData)
	b := launcherBattery("   ")
	require.True(t, b.IsZero())
	_, ok := scoreManifest(m, &b)
	assert.False(t, ok)
}

// TestTopicLabel_RendersWithoutChangingTheVocabulary replaces the old
// topicSuffix test. That function appended an app's topics to its row label in
// the flattened search view, because the section header no longer carried them
// — a job ADR-0214's summary line does better, so it is gone.
//
// What remains worth pinning is §SD10's split: the label is presentation, the
// token is the contract. A label that leaked back into the wire would break
// `--launch has(topics, 'observability')` and the introspection column with
// it, so the test asserts both halves rather than the spelling of any one
// label.
func TestTopicLabel_RendersWithoutChangingTheVocabulary(t *testing.T) {
	for _, tp := range app.AllTopics {
		label := topicLabel(tp)
		assert.NotEmpty(t, label, "%s: every registered topic needs a label", tp)
		assert.Equal(t, string(tp), tp.String(),
			"%s: the token is the wire value and must not follow the label", tp)
	}
	// An unregistered topic cannot reach a rendered row through a validated
	// manifest; falling back to the token is what keeps it debuggable if one
	// ever does.
	assert.Equal(t, "not-a-topic", topicLabel(app.TopicT("not-a-topic")))
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
		displaysOf(filterManifests(in, filterT{kinds: hidden}, nil)))
}

func TestFilterManifests_KindAndQueryCompose(t *testing.T) {
	in := []app.Manifest{
		mkKindManifest("a", "Widget gallery", app.KindDemo),
		mkKindManifest("b", "Widget inspector", app.KindApp),
	}
	hidden := kindFilterT(0).toggled(app.KindDemo)
	assert.Equal(t, []string{"Widget inspector"},
		displaysOf(filterManifests(in, filterT{query: "widget", kinds: hidden}, nil)),
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
	f := filterT{topics: topicFilterT(0).toggledAt(ci)}
	assert.Equal(t, []string{"Go packages"}, displaysOf(filterManifests(in, f, nil)))
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
	f := filterT{
		query:  "go",
		kinds:  kindFilterT(0).toggled(app.KindDemo),
		topics: topicFilterT(0).toggledAt(ci),
	}
	// "go" matches all three; the demo is hidden by kind, the geo entry by
	// topic. Only the applet survives.
	assert.Equal(t, []string{"Go packages"},
		displaysOf(filterManifests([]app.Manifest{code, demo, geo}, f, nil)))
}

func TestLauncherFilter_IsInert(t *testing.T) {
	assert.True(t, filterT{}.isInert())
	assert.True(t, filterT{query: "  \t "}.isInert(), "a whitespace query restricts nothing")
	assert.False(t, filterT{query: "x"}.isInert())
	assert.False(t, filterT{kinds: kindFilterT(0).toggled(app.KindDemo)}.isInert())
	assert.False(t, filterT{topics: topicFilterT(1)}.isInert())
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
