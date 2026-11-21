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
// The contract a host promises an app (ADR-0026, ADR-0396):
//
//   - Identity. Manifest.Id names the app; Manifest.SubjectAlias, the
//     explicit Alias or the one derived from the id, keys its bus subjects
//     and stored state. Registration refuses a duplicate id or alias.
//   - Registration. Apps register from init() with RegisterFactory or
//     Register. A refusal is recorded with package registration, and a host
//     refuses to start while any is recorded.
//   - Context. Mount and Unmount receive a MountContextI, Frame a
//     FrameContextI. Every capability a host offers is a method of these,
//     with a documented result for a host that lacks it; ContextOf is the
//     context an instance does its I/O under.
//   - Subjects. Manifest.Caps declares the subject filters the app's bus
//     client may use; a service package exports one helper per capability.
//
// What a host discovers by type assertion is what an app optionally
// implements:
//
//	PausableAppI          stops background work while every window is paused
//	OperationsAppI        serves the manifest's operations catalog
//	OperationsAsyncI      settles a command that runs past its frame
//	WorkingsetComposerI   hands back the record a workingset stores at close
//
// and, on a bus client the host minted, BusCloserI, BusInstanceI and
// BusInboxI.
package app

import (
	"io/fs"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// AppIdT is a dotted application identifier, typically a Go import path.
// Stable across implementation churn; renaming an Id is a deprecation event.
type AppIdT string

// DerivedAlias is the alias an app whose manifest sets no Alias is known
// by: the last '/'-separated segment of the id, with every byte outside
// [A-Za-z0-9_-] replaced by '_', so the result is one subject token. It keys
// the app's persisted state (runtime.persist.{alias}.>), its workstatelet
// entries and its operation subjects, so the derivation does not change.
//
// Examples:
//
//	"github.com/example/downstream/.../apps/play"  -> "play"
//	"github.com/.../apps/widgets/table"               -> "table"
//	"runtime.broker"                                  -> "runtime_broker"
//
// A host names a registered app by [Manifest.SubjectAlias] or [AliasOf],
// which honour an explicit Alias; DerivedAlias is for an id no manifest
// stands behind.
func (inst AppIdT) DerivedAlias() (alias string) {
	s := string(inst)
	if i := lastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isAliasByte(c) {
			b = append(b, c)
		} else {
			b = append(b, '_')
		}
	}
	alias = string(b)
	return
}

func isAliasByte(c byte) (ok bool) {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '_' || c == '-'
}

// ValidateAlias reports an alias that is not one subject token of
// [A-Za-z0-9_-].
func ValidateAlias(alias string) (err error) {
	if alias == "" {
		err = eh.Errorf("alias is empty")
		return
	}
	for i := 0; i < len(alias); i++ {
		if !isAliasByte(alias[i]) {
			err = eb.Build().Str("alias", alias).Errorf("alias %q has a byte outside [A-Za-z0-9_-]", alias)
			return
		}
	}
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
// Placement is never a surface value: a docked tile, a floating window and a
// fullscreen claim are all SurfaceWindowed (ADR-0026, the 2026-05-12
// amendment).
type SurfaceE uint8

const (
	SurfaceUnspecified SurfaceE = 0
	// SurfaceHeadless: no window — a service the service host mounts at
	// boot (ADR-0352).
	SurfaceHeadless SurfaceE = 1
	// SurfaceWindowed: the app body lives inside exactly one logical
	// window. The runtime creates and owns the window chrome (title from
	// Manifest.Title, optional Manifest.Icon prefix, drag/resize/close
	// affordances). The app's Frame() draws into the window body — it
	// must NOT call c.Window(...) or c.PanelCentral() itself.
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

// SubjectFilter is a NATS-style subject permission. Pattern follows NATS
// wildcards: '*' matches one token, '>' matches the rest. The in-process bus
// enforces it; the capability broker grants further filters at runtime.
type SubjectFilter struct {
	Pattern   string
	Reason    string
	Direction CapDirectionE
	Sticky    bool
}

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
}

// Manifest is the static description of an app. Returned by AppI.Manifest()
// once at registration time; never mutated. The registry validates manifests
// at Register and rejects malformed ones.
type Manifest struct {
	Id AppIdT
	// Alias is the one subject token the app is known by on the bus and in
	// stored state: its persist subjects (runtime.persist.{alias}.>), its
	// workstatelet entries, its operation subjects (ADR-0396 §1). Empty
	// means the alias derived from Id ([AppIdT.DerivedAlias]), which is
	// what every app had before the field existed. Set it when two apps'
	// derived aliases collide — a module outside this one prefixes a token
	// of its own, "acme_play" — or when the app's directory is renamed, to
	// the old alias, so its users' stored state stays reachable. Changing
	// an alias orphans that state.
	Alias   string
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
	// Maturity is what the app promises those who depend on it (ADR-0375):
	// a filter, a badge and two introspection columns, `maturity` and
	// `maturity_rank`, which the predicates over keelson.apps can name. The
	// zero value is unstated and ranks below every stated grade, so most
	// manifests may leave it unset and none is refused for leaving it out.
	Maturity MaturityE
	// Shell marks an app the host's own chrome opens — the launcher behind
	// the Apps menu and its key, help behind F1. It labels rather than
	// exempts: a launch limit (ADR-0272) applies to shell apps like any
	// other, and `shell` on keelson.apps is how a predicate names them.
	Shell bool

	Surface      SurfaceE
	SurfaceHints SurfaceHints

	// Caps is the set of NATS-style SubjectFilters the app declares it
	// needs. The host mints the app's bus client with them, and the
	// in-process client refuses a publish or subscribe no filter covers
	// (inprocbus, ErrPermissionViolation); a NATS client treats them as
	// advisory, the server enforcing its own. A service package exports one
	// helper per capability taking the reason (windowopen.OpenCap).
	Caps []SubjectFilter

	// PersistedKeys are the keys the app keeps in its storage
	// (MountContextI.Storage). Declaring any makes the host add the
	// capability on the app's persist subjects, runtime.persist.{alias}.>.
	PersistedKeys []string

	// LaunchKind names the launch-config vocabulary kind this app accepts
	// when opened via `runtime.windowhost.open` (ADR-0135 §SD3), e.g. "playLaunch".
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

	// Operations is the optional catalog of commands and queries the app
	// offers to callers on the bus, agents among them (ADR-0269 §SD2). It
	// requires factory registration, and an instance implementing
	// [OperationsAppI]. A catalog that fails validation refuses the
	// registration (ADR-0396 §2).
	Operations *OperationsCatalog

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

// SubjectAlias is the alias hosts key the app by: Alias when set, else the
// alias derived from Id.
func (inst Manifest) SubjectAlias() (alias string) {
	alias = inst.Alias
	if alias == "" {
		alias = inst.Id.DerivedAlias()
	}
	return
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
	if inst.Alias != "" {
		if aErr := ValidateAlias(inst.Alias); aErr != nil {
			err = eb.Build().Str("id", string(inst.Id)).Errorf("manifest: %w", aErr)
			return
		}
	}
	if inst.Surface == SurfaceUnspecified {
		err = eb.Build().Str("id", string(inst.Id)).Errorf("manifest: Surface must be set")
		return
	}
	// ADR-0158 §SD1/§SD2: topics are the launcher's only subject axis, so a
	// windowed app that declares none cannot be sectioned or filtered — it
	// would exist in the registry and be reachable from nowhere. A headless
	// app (ADR-0352 §SD1) is selected to run by a predicate over
	// keelson.apps and shown in the app center on the same terms, so it
	// declares them too.
	if len(inst.Topics) == 0 {
		err = eb.Build().Str("id", string(inst.Id)).Str("surface", inst.Surface.String()).
			Errorf("manifest: app declares no Topics")
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
	// Topics; a headless app's row in keelson('services') and its app
	// center page read it the same way (ADR-0352 §SD1).
	//
	// Non-empty only. The style budget (length, not repeating Display) is a
	// tree-wide test, not a Validate check: RegisterFactory answers a
	// rejection by dropping the app with a Warn (ADR-0158 §SD9), so a length
	// rule here would silently cost an app its place in the launcher.
	if inst.Summary == "" {
		err = eb.Build().Str("id", string(inst.Id)).Str("surface", inst.Surface.String()).
			Errorf("manifest: app declares no Summary")
		return
	}
	if !inst.Kind.IsValid() {
		err = eb.Build().Str("id", string(inst.Id)).
			Uint8("kind", uint8(inst.Kind)).Errorf("manifest: invalid Kind")
		return
	}
	// Out of range only. A manifest that states no grade is valid: a
	// rejection drops the app with a Warn (ADR-0158 §SD9), and a required
	// grade would cost every ungraded app its place in the launcher.
	if !inst.Maturity.IsValid() {
		err = eb.Build().Str("id", string(inst.Id)).
			Uint8("maturity", uint8(inst.Maturity)).Errorf("manifest: invalid Maturity")
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
	// ADR-0352 §SD1: a workingset is composed at a closing edge a
	// person caused, and a headless app has no window to close.
	if inst.Workingset && inst.Surface == SurfaceHeadless {
		err = eb.Build().Str("id", string(inst.Id)).
			Errorf("manifest: a headless app cannot declare Workingset")
		return
	}
	return
}
