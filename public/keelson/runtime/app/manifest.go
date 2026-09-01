// Package app is the runtime's first-class app abstraction. An app is a
// program with a static Manifest declaring identity, surface, window
// chrome metadata (Title/Icon), declared subject-filter capabilities,
// and persistence keys, plus a behaviour described by the AppI interface
// (Mount/Frame/Unmount).
//
// **App↔window is 1:1.** A non-headless app has exactly one logical
// window, and the runtime creates and owns that window. Apps do not
// instantiate windows; they declare what they want via Manifest.Title /
// Manifest.Icon and fill the window body from Frame(). Placement of the
// window (docked tile, floating, fullscreen claim) is the host's
// decision — the same app source runs unchanged across hosts.
//
// This is phase M1 of ADR-0026: the AppI/Manifest/Registry foundation, with
// forward-declared BusI and StorageI interfaces filled in by no-op stubs.
// The cap broker and CH+leeway state layer arrive in M2; the dock host
// arrives in M3; NATS arrives in M4; CLI unification (SurfaceHeadless) lands
// in M5. Apps that need to ship before their migrating host exists wrap
// their existing func() error renderer in a LegacyFuncApp.
package app

import (
	"io/fs"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// AppIdT is a dotted application identifier, typically a Go import path.
// Stable across implementation churn; renaming an Id is a deprecation event.
type AppIdT string

// SubjectAlias returns the NATS-token-safe alias used to encode this AppId
// in subject patterns (e.g., runtime.persist.{alias}.{key}.{op}). Derivation:
// take the last '/'-separated segment of the Id; replace any character that
// is not [A-Za-z0-9_-] with '_'. The result is one NATS subject token.
//
// Examples:
//
//	"github.com/stergiotis/pebble2impl/.../apps/play" -> "play"
//	"github.com/.../apps/widgets/table"               -> "table"
//	"runtime.broker"                                  -> "runtime_broker"
//
// Hosts that register apps are responsible for ensuring distinct AppIds
// produce distinct aliases; collisions would route to the wrong app.
func (inst AppIdT) SubjectAlias() (alias string) {
	s := string(inst)
	if i := lastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' {
			b = append(b, c)
		} else {
			b = append(b, '_')
		}
	}
	alias = string(b)
	return
}

func lastIndexByte(s string, c byte) (i int) {
	for i = len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return
		}
	}
	return -1
}

// SurfaceE describes the app↔window relationship. The contract is 1:1 —
// a non-headless app has exactly one logical window, and the runtime
// (host) creates and owns that window. Placement of the window (docked
// tile, floating, fullscreen claim) is the host's decision, not the
// app's; the same app source runs unchanged across hosts.
//
// Sharpened 2026-05-12 (ADR-0026 amendment): the prior values
// SurfaceDockTile / SurfaceFullCentral / SurfaceModal conflated placement
// with the fact-of-having-a-window. They are removed in favour of
// SurfaceWindowed; placement moves into the host (egui_dock tile in M3,
// fullscreen claim in the M2 launcher, overlay panel for runtime modals
// — which are runtime services, not registered apps).
type SurfaceE uint8

const (
	SurfaceUnspecified SurfaceE = 0
	// SurfaceHeadless: no window. CLI / one-shot / background app.
	// Reserved by M1, activated in M5 when cli.Command migrates to AppI.
	SurfaceHeadless SurfaceE = 1
	// SurfaceWindowed: the app body lives inside exactly one logical
	// window. The runtime creates and owns the window chrome (title from
	// Manifest.Title, optional Manifest.Icon prefix, drag/resize/close
	// affordances). The app's Frame() draws into the window body — it
	// must NOT call c.Window(...) or c.PanelCentral() itself once the
	// host wires up M3.
	SurfaceWindowed SurfaceE = 2
)

var AllSurfaces = []SurfaceE{
	SurfaceHeadless,
	SurfaceWindowed,
}

func (inst SurfaceE) String() (s string) {
	switch inst {
	case SurfaceHeadless:
		s = "headless"
	case SurfaceWindowed:
		s = "windowed"
	default:
		s = "unspecified"
	}
	return
}

// CapDirectionE indicates whether a SubjectFilter permits publish, subscribe,
// or both. Matches the NATS authorization shape (ADR-0026 §SD3).
type CapDirectionE uint8

const (
	CapDirectionUnspecified CapDirectionE = 0
	CapDirectionPub         CapDirectionE = 1
	CapDirectionSub         CapDirectionE = 2
	CapDirectionBoth        CapDirectionE = 3
)

var AllCapDirections = []CapDirectionE{
	CapDirectionPub,
	CapDirectionSub,
	CapDirectionBoth,
}

func (inst CapDirectionE) String() (s string) {
	switch inst {
	case CapDirectionPub:
		s = "pub"
	case CapDirectionSub:
		s = "sub"
	case CapDirectionBoth:
		s = "pub+sub"
	default:
		s = "unspecified"
	}
	return
}

// ParseCapDirection is the inverse of CapDirectionE.String. Unknown
// inputs map to CapDirectionUnspecified — the wire is
// forward-compatible with future directions a receiver did not
// anticipate.
func ParseCapDirection(s string) (d CapDirectionE) {
	switch s {
	case "pub":
		d = CapDirectionPub
	case "sub":
		d = CapDirectionSub
	case "pub+sub":
		d = CapDirectionBoth
	default:
		d = CapDirectionUnspecified
	}
	return
}

// SubjectFilter is a NATS-style subject permission. Forward-declared in M1;
// the cap broker that interprets these lands in M2. Pattern follows NATS
// wildcards: '*' matches one token, '>' matches the rest.
type SubjectFilter struct {
	Pattern   string
	Reason    string
	Direction CapDirectionE
	Sticky    bool
}

// ScreenshotFlagsE mirrors ADR-0057's DemoFlagsE so the screenshot tour
// registry folds into app.Registry without behavioural change.
type ScreenshotFlagsE uint32

const (
	ScreenshotFlagNone           ScreenshotFlagsE = 0
	ScreenshotFlagNeedsLargeArea ScreenshotFlagsE = 1 << iota
	ScreenshotFlagSkipInTour
	ScreenshotFlagNeedsNetwork
)

// SurfaceHints carries per-surface initial geometry and host-specific knobs.
// Only the fields relevant to the matching Surface are consulted; other
// hosts ignore unrelated fields. SurfaceHeadless apps ignore SurfaceHints
// entirely.
type SurfaceHints struct {
	// PreferredWidth and PreferredHeight: initial window size in egui
	// points. Zero means "let the host pick". Honoured for SurfaceWindowed
	// apps by the launcher/dock host when sizing the runtime-owned window.
	PreferredWidth  uint16
	PreferredHeight uint16
	// ScreenshotStage: canonical capture size for the screenshot driver.
	// Folded in from ADR-0057's Demo.Stage. Zero means "use driver default".
	ScreenshotStage [2]float32
	// ScreenshotFlags: per-app bitmask consumed by the screenshot host.
	// Folded in from ADR-0057's Demo.Flags.
	ScreenshotFlags ScreenshotFlagsE
}

// Manifest is the static description of an app. Returned by AppI.Manifest()
// once at registration time; never mutated. The registry validates manifests
// at Register and rejects malformed ones.
type Manifest struct {
	Id      AppIdT
	Version string
	// Display is the human label used in launcher menus, dock tab strips,
	// and crash banners. Required.
	Display string
	// Title is the text the runtime puts in the window's title bar.
	// Optional; defaults to Display when empty. Use Title when the window
	// label should differ from the menu label (e.g., Display="HN" with
	// Title="Hacker News Explorer").
	Title string
	// Summary is the one-line description the launcher renders beneath
	// Display (ADR-0214 §SD4): what the app does, for whom. Required
	// non-empty for SurfaceWindowed apps and validated at Register —
	// headless apps have no launcher presence and are exempt.
	//
	// The job is discrimination, not introduction: a reader looking at a
	// screen of entries filed under one topic needs the line that tells
	// this one from its siblings. So it is authored rather than derived
	// from the app's help corpus, whose lead paragraph is written to open
	// a document (§Alternatives).
	//
	// Style, enforced by review and the tree-wide test rather than by
	// Validate — a rejected manifest is dropped with a Warn, and a length
	// cap would silently cost an app its place in the launcher over a
	// house-style matter (ADR-0158 §SD9):
	//
	//   - one clause, verb-first, no trailing period
	//   - never repeat Display ("Terrain scope — a scope for terrain")
	//   - roughly 60 characters; the row dims and truncates past that
	//   - no marketing register (AGENTS § Writing style applies here too)
	Summary string
	// Icon is an optional glyph prepended to the window title.
	// Convention: one Unicode codepoint (emoji or material-design glyph).
	// The runtime renders it as "Icon Title" in the title bar.
	Icon string
	// Topics classify the app by subject (ADR-0158 §SD1/§SD2): what it is
	// about, never how it was built. Multi-valued and unordered, drawn from
	// the registered vocabulary in topics.go; required non-empty for
	// SurfaceWindowed apps and validated at Register.
	//
	// The authoring rule is freedesktop's: list every topic that clearly
	// applies, none that only vaguely applies. An app carrying several
	// browses under each of them (§SD3) — there is no primary topic, so
	// order carries no meaning.
	//
	// Replaced the single free-form Category field, whose axis had drifted
	// to provenance; that job is Kind's now.
	Topics []TopicT
	// Keywords are free retrieval text: what someone might type looking for
	// this app when its Display name is not what came to mind ("cpu",
	// "htop", "deps"). Ungoverned by design (ADR-0158 §SD4) — no vocabulary,
	// duplicates and near-synonyms harmless — because they are never
	// rendered as structure, only matched against. Governance is owed on
	// terms a reader navigates, and these are not.
	Keywords []string
	// Kind is the app's provenance (ADR-0158 §SD5): a filter and an
	// introspection column, not a browse section. Zero value KindApp covers
	// ordinary Go apps, so most manifests leave it unset.
	Kind KindE

	Surface      SurfaceE
	SurfaceHints SurfaceHints

	// Caps is the set of NATS-style SubjectFilters the app declares it needs.
	// Forward-declared in M1 (the broker arrives in M2); hosts may store
	// declared caps for later audit but do not gate behaviour on them yet.
	Caps             []SubjectFilter
	BackgroundTickHz uint8

	// PersistedKeys is the set of cold-state keys the runtime will auto-manage
	// via the M2 storage layer. Forward-declared in M1.
	PersistedKeys []string

	// LaunchKind names the launch-config vocabulary kind this app accepts
	// when opened via `windowhost.open` (ADR-0135 §SD3), e.g. "playLaunch".
	// Kinds are boxer.facts vocabulary names whose codec the app owns
	// (§SD2); the app decodes MountContextI.LaunchConfig() with that
	// codec's generated Unmarshal in Mount. Empty means the app accepts no
	// launch config — an argument-carrying open targeting it is refused at
	// the host boundary. Plain opens are unaffected either way.
	LaunchKind string

	// Workingset marks this app as a workingset participant (ADR-0148
	// §SD7): the host pulls a record out of the closing window via
	// [WorkingsetComposerI] and, at a later plain open, routes the stored
	// record back in through OpenWithConfig. Requires a non-empty
	// LaunchKind — the record IS an instance of that DTO (§SD2), which
	// Validate enforces — and factory registration, which Registry.Register
	// refuses this field outright to enforce. (A factory whose ctor hands
	// out one shared instance is indistinguishable from here; the host
	// refuses to deliver a config to such an instance at open time.)
	//
	// Orthogonal to PersistedKeys: raw keys stay the right declaration for
	// content too large or complex for the record, which then carries only
	// a reference to it (§SD1).
	Workingset bool

	// Help is the optional inline-help corpus for this app. When non-nil,
	// the keelson/runtime/help package's DefaultLibrary will lazily index
	// every `*.md` file under the fs.FS (any depth) on first access and
	// expose them via a per-app BookI. Apps typically populate this with
	// an embed.FS:
	//
	//	//go:embed help
	//	var helpFS embed.FS
	//	var manifest = app.Manifest{ ..., Help: helpFS }
	//
	// Development hosts can swap in an os.DirFS for live-reload editing.
	// nil means "no help docs ship with this app"; the help library
	// silently skips such manifests.
	//
	// The field carries the fs.FS only — the help package owns parsing
	// and indexing. Adding this field does not pull the markdown widget
	// or goldmark into the app package's transitive imports; consumers
	// of [Manifest] that don't read .Help see no dependency change.
	Help fs.FS
}

// WindowTitle returns the composed window title hosts use when building
// the runtime-owned window chrome. Format: "{Icon} {Title}" — Icon is
// omitted when empty; Title falls back to Display when empty. Returns
// the empty string only when both Title and Display are empty (which
// Validate rejects for SurfaceWindowed apps).
func (inst Manifest) WindowTitle() (title string) {
	title = inst.Title
	if title == "" {
		title = inst.Display
	}
	if inst.Icon != "" {
		if title == "" {
			title = inst.Icon
			return
		}
		title = inst.Icon + " " + title
	}
	return
}

// Validate returns an error when the manifest is structurally broken.
// Uniqueness is enforced by Registry, not here.
func (inst Manifest) Validate() (err error) {
	if inst.Id == "" {
		err = eh.Errorf("manifest: empty Id")
		return
	}
	if inst.Display == "" {
		err = eb.Build().Str("id", string(inst.Id)).Errorf("manifest: empty Display")
		return
	}
	if inst.Surface == SurfaceUnspecified {
		err = eb.Build().Str("id", string(inst.Id)).Errorf("manifest: Surface must be set")
		return
	}
	// ADR-0158 §SD1/§SD2: topics are the launcher's only subject axis, so a
	// windowed app that declares none cannot be sectioned or filtered — it
	// would exist in the registry and be reachable from nowhere. Headless
	// apps have no launcher presence and are exempt.
	if inst.Surface == SurfaceWindowed && len(inst.Topics) == 0 {
		err = eb.Build().Str("id", string(inst.Id)).
			Errorf("manifest: windowed app declares no Topics")
		return
	}
	for _, t := range inst.Topics {
		if !t.IsRegistered() {
			err = eb.Build().Str("id", string(inst.Id)).Str("topic", string(t)).
				Errorf("manifest: unregistered topic")
			return
		}
	}
	// ADR-0214 §SD4: the launcher row is icon + Display + Summary, and a
	// windowed app without the third is an entry a reader cannot tell from
	// its siblings. Required for the same reason and on the same terms as
	// Topics; headless apps have no launcher presence and are exempt.
	//
	// Non-empty only. The style budget (length, not repeating Display) is a
	// tree-wide test, not a Validate check: RegisterFactory answers a
	// rejection by dropping the app with a Warn (ADR-0158 §SD9), so a length
	// rule here would silently cost an app its place in the launcher.
	if inst.Surface == SurfaceWindowed && inst.Summary == "" {
		err = eb.Build().Str("id", string(inst.Id)).
			Errorf("manifest: windowed app declares no Summary")
		return
	}
	if !inst.Kind.IsValid() {
		err = eb.Build().Str("id", string(inst.Id)).
			Uint8("kind", uint8(inst.Kind)).Errorf("manifest: invalid Kind")
		return
	}
	// ADR-0148 §SD7: a workingset record is an instance of the app's
	// launch-config DTO and restore is OpenWithConfig, so declaring
	// participation without a launch kind describes something that cannot
	// be stored or restored. Refuse at registration rather than at the
	// first close.
	if inst.Workingset && inst.LaunchKind == "" {
		err = eb.Build().Str("id", string(inst.Id)).
			Errorf("manifest: Workingset requires a non-empty LaunchKind")
		return
	}
	return
}
