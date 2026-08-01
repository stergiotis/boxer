package app

import "slices"

// TopicT is one registered subject topic (ADR-0158 §SD1). Topics classify an
// app by *what it is about*, never by how it was built — provenance lives on
// [KindE] instead, precisely so that a subject stops being split by
// implementation technique.
//
// The vocabulary is closed: only the constants below are valid, and
// [Manifest.Validate] refuses anything else. The type exists rather than a
// bare string so an in-tree typo is a compile error instead of a
// registration-time drop (§SD2, §SD9) — the same reason [SurfaceE] and
// [CapDirectionE] are named types. Topics arriving at runtime, from applet
// frontmatter, go through [ParseTopic].
//
// Adding a member here is not an architecture decision (CODINGSTANDARDS
// § What triggers an ADR); changing what the vocabulary *means* — its axis —
// would be.
type TopicT string

const (
	// TopicRuntime: the app runtime itself — capabilities, config, help,
	// logs, the registry.
	TopicRuntime TopicT = "runtime"
	// TopicCode: source, packages, dependencies, and the shape of the repo.
	TopicCode TopicT = "code"
	// TopicTopology: the appliance topology tables (ADR-0126) — components,
	// processes, sockets, drift.
	TopicTopology TopicT = "topology"
	// TopicObservability: what the process is doing right now — profiles,
	// metrics, render telemetry, process state.
	TopicObservability TopicT = "observability"
	// TopicData: datasets, columnar modelling, and data-shaped widgets.
	TopicData TopicT = "data"
	// TopicSql: authoring, running, and explaining queries.
	TopicSql TopicT = "sql"
	// TopicUi: the widget layer and the design system.
	TopicUi TopicT = "ui"
	// TopicGeo: terrain, maps, and geospatial work.
	TopicGeo TopicT = "geo"
	// TopicAbout: the project itself — provenance, licence, splash.
	TopicAbout TopicT = "about"
)

// AllTopics is the registered vocabulary in display order. The launcher
// sections the no-filter browse view in this order (ADR-0158 §SD3), so it is
// deliberately hand-ordered rather than alphabetical: the topics a reader
// most often opens come first.
var AllTopics = []TopicT{
	TopicRuntime,
	TopicCode,
	TopicTopology,
	TopicObservability,
	TopicData,
	TopicSql,
	TopicUi,
	TopicGeo,
	TopicAbout,
}

// String renders the topic as its wire/display token.
func (inst TopicT) String() (s string) {
	s = string(inst)
	return
}

// IsRegistered reports whether inst is a member of [AllTopics]. The empty
// topic is not registered — a manifest declaring one is malformed, not
// uncategorised, because ADR-0158 removes the uncategorised bucket.
func (inst TopicT) IsRegistered() (ok bool) {
	ok = slices.Contains(AllTopics, inst)
	return
}

// ParseTopic resolves a runtime-supplied token (applet frontmatter) against
// the vocabulary. Unknown tokens return ok=false rather than a synthesised
// topic: the caller decides whether that is a hard error (the applet minter
// refuses the document) or a skip, and neither should be able to invent a
// vocabulary member by accident.
func ParseTopic(s string) (t TopicT, ok bool) {
	cand := TopicT(s)
	if cand.IsRegistered() {
		t = cand
		ok = true
	}
	return
}

// KindE records how an app came to exist — its provenance (ADR-0158 §SD5).
// It is a filter and an introspection column, never a browse section: the
// launcher may offer "show demos", but no app *lives* under "Demos". Keeping
// provenance off the subject axis is the whole point of the split, so
// resist the temptation to render this as a heading.
type KindE uint8

const (
	// KindApp is the zero value: an ordinary Go-implemented app.
	KindApp KindE = 0
	// KindApplet is an app minted from a committed SQL-applet document
	// (ADR-0132). Stamped by the minter, never hand-written.
	KindApplet KindE = 1
	// KindDemo is an app that exists to demonstrate a widget or a runtime
	// facility rather than to do a user's work.
	KindDemo KindE = 2
)

var AllKinds = []KindE{
	KindApp,
	KindApplet,
	KindDemo,
}

// String renders the kind as its wire/display token.
func (inst KindE) String() (s string) {
	switch inst {
	case KindApplet:
		s = "applet"
	case KindDemo:
		s = "demo"
	default:
		s = "app"
	}
	return
}

// IsValid reports whether inst is a declared member. Unlike [TopicT] the zero
// value is valid — an app that declares no kind is an ordinary app, which is
// the common case and should not need a line in every manifest.
func (inst KindE) IsValid() (ok bool) {
	ok = inst == KindApp || inst == KindApplet || inst == KindDemo
	return
}
