package app

import (
	"slices"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/registration"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TopicT is one registered subject topic (ADR-0158 §SD1). Topics classify an
// app by *what it is about*, never by how it was built — provenance lives on
// [KindE] instead, precisely so that a subject stops being split by
// implementation technique.
//
// The vocabulary is closed: the constants below, and the topics a module
// adds with [RegisterTopic] at package initialisation (ADR-0396 §3).
// [Manifest.Validate] refuses anything else. The type exists rather than a
// bare string so an in-tree typo is a compile error instead of a
// registration refusal (§SD2, §SD9) — the same reason [SurfaceE] and
// [CapDirectionE] are named types. Topics arriving at runtime, from applet
// frontmatter, go through [ParseTopic].
//
// Adding a member is not an architecture decision (CODINGSTANDARDS
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
	// TopicSensing: instruments, radios and sensors — live measurements from
	// the physical world (ADR-0158 Update 2026-08-28; the first member is
	// a downstream repository's Micronet receiver).
	TopicSensing TopicT = "sensing"
	// TopicAbout: the project itself — provenance, licence, splash.
	TopicAbout TopicT = "about"
)

// builtinTopics is the vocabulary this module defines, in display order.
// The launcher sections the no-filter browse view in this order (ADR-0158
// §SD3), so it is deliberately hand-ordered rather than alphabetical: the
// topics a reader most often opens come first.
var builtinTopics = []TopicT{
	TopicRuntime,
	TopicCode,
	TopicTopology,
	TopicObservability,
	TopicData,
	TopicSql,
	TopicUi,
	TopicGeo,
	TopicSensing,
	TopicAbout,
}

// MaxTopics bounds the vocabulary: the launcher's topic filter is a
// bitmask over vocabulary positions.
const MaxTopics = 32

var (
	topicsMu sync.RWMutex
	topics   = slices.Clone(builtinTopics)
)

// Topics is the vocabulary in display order: this module's topics, then
// the ones registered with RegisterTopic in registration order. The slice
// is shared and must not be modified; a registration after the call does
// not reach it.
func Topics() (ts []TopicT) {
	topicsMu.RLock()
	ts = topics
	topicsMu.RUnlock()
	return
}

// RegisterTopic adds t to the vocabulary, from the package initialisation
// of the module that needs it. A malformed token, a topic already in the
// vocabulary, or one past MaxTopics is recorded as a registration failure,
// which stops a host from starting (ADR-0396 §2, §3). Register a topic
// before the manifests that declare it — in the same package's init, or a
// package they import.
func RegisterTopic(t TopicT) {
	registration.Fail("app.RegisterTopic", AddTopic(t))
}

// AddTopic is RegisterTopic returning the refusal instead of recording it.
func AddTopic(t TopicT) (err error) {
	s := string(t)
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		err = eb.Build().Str("topic", s).Errorf("topic %q is not a lower-case token", s) //boxer:lint disable=CS013 reason="a registration or manifest refusal is read as text when a host refuses to start: the name it refuses belongs in the message"
		return
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-') {
			err = eb.Build().Str("topic", s).Errorf("topic %q has a byte outside [a-z0-9-]", s) //boxer:lint disable=CS013 reason="a registration or manifest refusal is read as text when a host refuses to start: the name it refuses belongs in the message"
			return
		}
	}
	topicsMu.Lock()
	defer topicsMu.Unlock()
	if slices.Contains(topics, t) {
		err = eb.Build().Str("topic", s).Errorf("topic %q is already in the vocabulary", s) //boxer:lint disable=CS013 reason="a registration or manifest refusal is read as text when a host refuses to start: the name it refuses belongs in the message"
		return
	}
	if len(topics) >= MaxTopics {
		err = eb.Build().Str("topic", s).Errorf("the topic vocabulary holds %d topics already", MaxTopics) //boxer:lint disable=CS013 reason="a registration or manifest refusal is read as text when a host refuses to start: the name it refuses belongs in the message"
		return
	}
	next := make([]TopicT, len(topics), len(topics)+1)
	copy(next, topics)
	topics = append(next, t)
	return
}

// String renders the topic as its wire/display token.
func (inst TopicT) String() (s string) {
	s = string(inst)
	return
}

// IsRegistered reports whether inst is a member of [Topics]. The empty
// topic is not registered — a manifest declaring one is malformed, not
// uncategorised, because ADR-0158 removes the uncategorised bucket.
func (inst TopicT) IsRegistered() (ok bool) {
	ok = slices.Contains(Topics(), inst)
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
	// KindChatlet is an app minted from a committed chatlet document
	// (ADR-0364). Stamped by the minter, never hand-written.
	KindChatlet KindE = 3
	// KindWorkstatelet is a launcher entry minted from a workstatelet
	// document (ADR-0369). It has no window of its own: the window
	// host opens the windows its document lists. Stamped by the minter,
	// never hand-written.
	KindWorkstatelet KindE = 4
	// KindDashboardlet is an app minted from a committed dashboardlet
	// document (ADR-0378): one dashboard, opened in an adopter's dashboard
	// viewer. Stamped by the minter, never hand-written.
	KindDashboardlet KindE = 5
	// KindNotebooklet is an app minted from a committed notebook
	// (ADR-0380): the notebook read live, in a window that is its pane and
	// nothing else. Stamped by the minter, never hand-written.
	KindNotebooklet KindE = 6
)

var AllKinds = []KindE{
	KindApp,
	KindApplet,
	KindDemo,
	KindChatlet,
	KindWorkstatelet,
	KindDashboardlet,
	KindNotebooklet,
}

// String renders the kind as its wire/display token.
func (inst KindE) String() (s string) {
	switch inst {
	case KindApplet:
		s = "applet"
	case KindDemo:
		s = "demo"
	case KindChatlet:
		s = "chatlet"
	case KindWorkstatelet:
		s = "workstatelet"
	case KindDashboardlet:
		s = "dashboardlet"
	case KindNotebooklet:
		s = "notebooklet"
	default:
		s = "app"
	}
	return
}

// IsValid reports whether inst is a declared member. Unlike [TopicT] the zero
// value is valid — an app that declares no kind is an ordinary app, which is
// the common case and should not need a line in every manifest.
func (inst KindE) IsValid() (ok bool) {
	ok = inst == KindApp || inst == KindApplet || inst == KindDemo || inst == KindChatlet || inst == KindWorkstatelet || inst == KindDashboardlet || inst == KindNotebooklet
	return
}
