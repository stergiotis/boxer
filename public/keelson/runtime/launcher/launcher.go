package launcher

import (
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/regexedit"
)

// HostI is what the launcher needs of the window host (ADR-0214 §SD3). Kept
// to app-level types so this package does not import windowhost, which
// renders the launcher component for its empty-state pane and must therefore
// be the importing side. windowhost supplies the adapter.
type HostI interface {
	// OpenOrRaiseApp opens appId, or raises its window when one is already
	// open. Raise-rather-than-open is the launcher's default action (§SD10):
	// a second click on a visible app used to open a silent duplicate.
	OpenOrRaiseApp(appId app.AppIdT) (err error)
	// OpenAppIds reports which apps currently hold a window, for the row's
	// "open" badge. Order is not meaningful; the launcher builds a set.
	OpenAppIds() (ids []app.AppIdT)
}

// listPanelDefaultWidth is the list pane's initial width in egui points.
//
// The budget it has to clear is a ~60-character summary at the default
// density — what Manifest.Summary is authored against — plus, on each side,
// the row's chrome (rows.rowInset) and the pane's own padding. Sized a little
// past that rather than onto it: a row whose summary ends exactly at the
// outline reads as truncated whether or not it is, and the first line carries
// an icon, a name and up to two badges that the character budget says nothing
// about. Resizable, and egui remembers where the user put it.
const listPanelDefaultWidth = 420

// Inst is the launcher component. One value backs every mount point (§SD2),
// so the query, the facet filters and the selection survive moving between
// the empty-state pane and the launcher window.
//
// Not safe for concurrent use, and does not need to be: every field is
// touched from the render thread only. The host's async paths (audit writes,
// facts reads) hand their results in through SetRank, which is called from
// the same thread.
type Inst struct {
	registry *app.Registry
	host     HostI
	lib      help.LibraryI
	logger   zerolog.Logger

	// density is re-resolved per frame — the preset is switchable at runtime
	// from the shell's Layout ▸ Density menu.
	density styletokens.DensityE

	// searchText is the raw query, and searchHl the regexedit state that
	// colours it. Held here rather than per mount point so typing a query in
	// the pane and reopening in the window shows the same result set.
	searchText string
	searchHl   regexedit.Edit

	// kindShown mirrors the provenance toggles as a []bool because
	// Checkbox.SendRespVal needs a stable address and writes into it at
	// StateManager.Sync, after the frame body has run. kindFilter() derives
	// the mask each frame.
	kindShown []bool
	// topicFilter is held as the mask itself: its chips are SelectableLabels,
	// whose click response is read within the frame.
	topicFilter topicFilterT

	// selected is the app whose detail the right pane shows. Written from the
	// cursor once per frame (see renderRows) rather than assigned by each
	// gesture, so the highlighted row and the described app cannot diverge.
	// Empty means nothing is selectable — an empty list — which the pane
	// describes rather than leaving blank.
	selected app.AppIdT
	// wantFocus asks the next Render to put the caret in the query field. Set
	// when a window mounts (§SD9's contract starts with the field focused, so
	// F2-then-type works without a click) and cleared once spent — a standing
	// request would fight every other focusable widget on the pane.
	wantFocus bool

	// cursor is the keyboard cursor's index into the *currently rendered*
	// row list. Row lists change under it as the query changes, so it is
	// clamped at render rather than trusted between frames.
	cursor int

	// helpAppId is the app the "Help" action raises, injected rather than
	// imported: a launcher that names one app in its own imports cannot be
	// built without it, and the launcher's job is not to know which app reads
	// help. Empty hides the action.
	helpAppId app.AppIdT

	// rank supplies §SD8's frecency bonus. nil until a history source is
	// wired, which is the state a run without ClickHouse stays in.
	rank rankFn
	// recentFn supplies the menu's recents, most recent first. Separate from
	// rank because the two answer different questions off the same trail:
	// rank is a per-app weight for ordering a set someone asked for, recents
	// is an ordered short list nobody asked for.
	recentFn func() (ids []app.AppIdT)
}

// New constructs a launcher over registry. host may be nil — the component
// renders, and its open actions report that they cannot act, which is what
// the screenshot-tour path needs. lib may be nil; the detail pane then omits
// the help section rather than special-casing every read.
func New(registry *app.Registry, host HostI, lib help.LibraryI, logger zerolog.Logger) (inst *Inst) {
	inst = &Inst{
		registry: registry,
		host:     host,
		lib:      lib,
		logger:   logger,
	}
	return
}

// SetRank installs the frecency bonus provider (§SD8). Passing nil restores
// authored-metadata ordering, which is what a host does when its facts store
// turns out to have no history capability.
func (inst *Inst) SetRank(fn rankFn) {
	inst.rank = fn
}

// SetHost installs the host after construction, for boots that build the
// launcher before the window host exists.
func (inst *Inst) SetHost(host HostI) {
	inst.host = host
}

// FocusQueryField asks the next Render to put the caret in the query field.
// Called when a launcher window mounts: the point of a keyboard shortcut is
// that pressing it and typing works, which needs no intervening click.
func (inst *Inst) FocusQueryField() {
	inst.wantFocus = true
}

// SetHelpApp names the app the detail pane's "Help" action raises. Unset
// means no such action, which is the right state for a host that registers no
// help reader.
func (inst *Inst) SetHelpApp(id app.AppIdT) {
	inst.helpAppId = id
}

// Render draws the whole launcher: the list pane beside the detail pane. The
// caller supplies the id stack, so the same Inst can be drawn from the
// windowhost pane and from the launcher window without deriving the same
// widget ids (§SD2).
//
// Emits panels, so it must run inside a Ui scope — a host-created window body
// or a PanelCentral. It does not open a window of its own.
func (inst *Inst) Render(ids *c.WidgetIdStack) {
	inst.density = styletokens.ActiveDensity()
	for range c.PanelLeftInside(ids.PrepareStr("launcher-list")).
		DefaultSize(listPanelDefaultWidth).
		Resizable(true).
		KeepIter() {
		inst.renderListPane(ids)
	}
	for range c.PanelCentralInside().KeepIter() {
		inst.renderDetailPane(ids)
	}
}

// renderListPane draws the query field, the facet controls, and the rows.
//
// Order is load-bearing. The field is emitted first so its captured keys are
// readable this frame; the rows are built next so the keys act on the list the
// user is looking at; and a key that changed the query rebuilds the rows
// before they are drawn, so Escape-to-clear does not leave a frame of stale
// hits on screen.
func (inst *Inst) renderListPane(ids *c.WidgetIdStack) {
	if len(inst.registry.AllManifests()) == 0 {
		c.Label("No apps registered.").Send()
		return
	}
	fieldId := inst.renderSearchBox(ids)
	inst.renderKindToggles(ids, "pane")
	inst.renderTopicChips(ids)
	c.Separator().Horizontal().Send()

	visible := inst.visibleManifests()
	rows := inst.buildRows(visible)
	queryBefore := inst.query()
	openIdx := inst.applyKeys(rows, fieldId)
	if inst.query() != queryBefore {
		visible = inst.visibleManifests()
		rows = inst.buildRows(visible)
	}
	inst.renderRows(ids, visible, rows)
	// Acted on after the rows are drawn: opening a window mutates the host,
	// and the row that asked for it should already be on screen when it does.
	if openIdx >= 0 && openIdx < len(rows) && rows[openIdx].heading == "" {
		inst.open(rows[openIdx].m.Id)
	}
}

// renderSearchBox draws the query field and its clear button.
//
// The placeholder names what the box searches, not how (§SD10). The syntax is
// still the ADR-0164 battery — space-separated regexes, ANDed — and a reader
// who types one gets it; what changed is that the box no longer opens by
// announcing a regex to someone who wanted to find the log viewer. The
// diagnostic below the rows still appears, but only once a token has actually
// failed to compile.
func (inst *Inst) renderSearchBox(ids *c.WidgetIdStack) (fieldId uint64) {
	pad := styletokens.PaddingTight(inst.density)
	for range c.Frame(ids.PrepareStr("launcher-search-frame")).
		InnerMarginSides(0, 0, pad, pad).
		KeepIter() {
		for range c.Horizontal().KeepIter() {
			// CaptureKeys on the field itself, not on a wrapping Frame:
			// capture is gated on the capturing widget having focus, and the
			// widget with focus while someone types is this one (§SD9, keys.go).
			edit := inst.searchHl.Prepare(ids.PrepareStr("launcher-search"), inst.searchText, false, regexedit.ModeTokens).
				HintText("Search apps").
				CaptureKeys(uint64(launcherKeyMask))
			fieldId = edit.Id()
			edit.SendRespVal(&inst.searchText)
			if inst.wantFocus {
				// One shot. RequestFocus is an op, not a state, so a standing
				// request would re-take focus from whatever the user clicked
				// next, every frame.
				c.RequestFocus(fieldId)
				inst.wantFocus = false
			}
			if inst.searchText != "" {
				if c.Button(ids.PrepareStr("launcher-search-clear"), c.Atoms().Text(icons.PhX).Keep()).
					SendResp().HasPrimaryClicked() {
					inst.searchText = ""
					inst.cursor = 0
				}
			}
		}
	}
	return
}

// query returns the trimmed query string.
func (inst *Inst) query() (q string) {
	q = strings.TrimSpace(inst.searchText)
	return
}

// visibleManifests applies the facet filters — the set the browse sections and
// the search both draw from.
func (inst *Inst) visibleManifests() (out []app.Manifest) {
	out = filterManifests(inst.registry.AllManifests(),
		filterT{kinds: inst.kindFilter(), topics: inst.topicFilter}, inst.rank)
	return
}

// hits resolves the current query against the visible set, ranked.
func (inst *Inst) hits(visible []app.Manifest) (out []app.Manifest) {
	out = filterManifests(visible, filterT{query: inst.query()}, inst.rank)
	return
}

// kindFilter derives the provenance mask from the per-kind toggle state. A
// component whose toggles were never initialised yields the inert filter, so
// "uninitialised" shows everything rather than hiding everything.
func (inst *Inst) kindFilter() (f kindFilterT) {
	if len(inst.kindShown) != len(app.AllKinds) {
		return
	}
	for i, k := range app.AllKinds {
		if !inst.kindShown[i] {
			f = f.toggled(k)
		}
	}
	return
}

// ensureKindShown lazily initialises the toggle state to "everything shown".
// Lazy rather than done in New so a zero Inst cannot start out hiding every
// app.
func (inst *Inst) ensureKindShown() {
	if len(inst.kindShown) == len(app.AllKinds) {
		return
	}
	inst.kindShown = make([]bool, len(app.AllKinds))
	for i := range inst.kindShown {
		inst.kindShown[i] = true
	}
}

// renderKindToggles draws the provenance filter (ADR-0158 §SD5): one checkbox
// per kind, all on by default. These exist because §SD3 retired "Applets" and
// "Demos" as browse sections — provenance is a filter over a subject-organised
// list, not a place an app lives — and without them that retirement would
// simply delete two views people use.
//
// scope keys the widget ids so several surfaces can draw the same toggles.
func (inst *Inst) renderKindToggles(ids *c.WidgetIdStack, scope string) {
	inst.ensureKindShown()
	for range c.Horizontal().KeepIter() {
		c.Label("Show:").Send()
		for i, k := range app.AllKinds {
			c.Checkbox(ids.PrepareStr("kind-"+scope+"-"+k.String()), inst.kindShown[i], kindLabel(k)).
				SendRespVal(&inst.kindShown[i])
		}
	}
}

// renderTopicChips draws the topic filter (ADR-0158 §SD6): one selectable
// chip per vocabulary member some registered app carries, wrapping, with none
// selected meaning no restriction.
//
// Chips read as reader-facing labels rather than vocabulary tokens (§SD10):
// the registry's spelling is `observability`, and a person browsing is not
// looking for a token.
func (inst *Inst) renderTopicChips(ids *c.WidgetIdStack) {
	manifests := inst.registry.AllManifests()
	present := make(map[app.TopicT]struct{}, len(app.AllTopics))
	for _, m := range manifests {
		for _, t := range m.Topics {
			present[t] = struct{}{}
		}
	}
	for range c.HorizontalWrapped().KeepIter() {
		c.Label("Topics:").Send()
		for i, t := range app.AllTopics {
			if _, has := present[t]; !has {
				continue
			}
			if c.SelectableLabel(ids.PrepareStr("topic-chip-"+t.String()),
				inst.topicFilter.selectedAt(i), topicLabel(t)).
				SendResp().HasPrimaryClicked() {
				inst.topicFilter = inst.topicFilter.toggledAt(i)
				inst.cursor = 0
			}
		}
		if !inst.topicFilter.isInert() {
			if c.Button(ids.PrepareStr("topic-chip-clear"), c.Atoms().Text(icons.PhX+" Clear").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.topicFilter = 0
				inst.cursor = 0
			}
		}
	}
}

// renderSearchNotes draws the two lines that describe what the battery did:
// how selective it was, and whether any token silently stopped being a regex.
//
// The selectivity readout is a bare count, not the byte-share progress bar the
// help and snippet boxes carry. There the sections a battery selects vary
// hugely in size, so a count would misreport how much corpus a query admits;
// here every hit is one app-sized thing and the count *is* the honest number.
func renderSearchNotes(b search.Battery, nHits int, nTotal int) {
	for rt := range c.RichTextLabel(strconv.Itoa(nHits) + " of " + strconv.Itoa(nTotal) + " apps") {
		rt.Small().Weak()
	}
	for pi := range b.Patterns {
		if b.Patterns[pi].Literal {
			// Surfaced rather than silent (ADR-0164 §SD2): a half-typed
			// `quantile(` keeps matching as text, and the user is told that is
			// what happened. Shown only when it actually happened, which is
			// the §SD10 change — it used to stand in the hint text.
			for rt := range c.RichTextLabel("some tokens are not valid regexps and match literally") {
				rt.Small().Weak()
			}
			break
		}
	}
}

// emptyResultHint distinguishes "nothing matched what you typed" from "your
// own toggles hid it". Without the second wording a user who has switched
// Demos off sees a bare "(no matches)" and reasonably concludes the app is
// gone rather than filtered — the failure mode that makes hide-toggles
// annoying elsewhere.
func (inst *Inst) emptyResultHint() (s string) {
	switch {
	case !inst.topicFilter.isInert() && inst.kindFilter().hidesAnything():
		s = "(no matches — a topic filter and hidden kinds are both active)"
	case !inst.topicFilter.isInert():
		s = "(no matches — filtered to selected topics)"
	case inst.kindFilter().hidesAnything():
		s = "(no matches — some kinds are hidden)"
	default:
		s = "(no matches)"
	}
	return
}

// open runs the launcher's default action for one app (§SD10): raise the
// window when one exists, open one when it does not.
func (inst *Inst) open(id app.AppIdT) {
	if inst.host == nil {
		inst.logger.Warn().Str("id", string(id)).
			Msg("launcher: no host wired; open ignored")
		return
	}
	inst.logger.Info().Str("id", string(id)).Msg("launcher: open-or-raise")
	if err := inst.host.OpenOrRaiseApp(id); err != nil {
		inst.logger.Warn().Err(err).Str("id", string(id)).Msg("launcher: open failed")
	}
}

// openAppSet is the set of apps holding a window this frame, for the row
// badge. Built per frame: a window opened or closed since the last one must
// change the badge, and the set is a few entries.
func (inst *Inst) openAppSet() (set map[app.AppIdT]struct{}) {
	set = map[app.AppIdT]struct{}{}
	if inst.host == nil {
		return
	}
	for _, id := range inst.host.OpenAppIds() {
		set[id] = struct{}{}
	}
	return
}

// kindLabel is a toggle's user-facing label: the plural of the kind, since the
// toggle governs a set. Kept out of app.KindE because it is presentation — the
// introspection column wants the singular wire form KindE.String gives, and
// the two should not drift into one another.
func kindLabel(k app.KindE) (s string) {
	switch k {
	case app.KindApp:
		s = "Apps"
	case app.KindApplet:
		s = "Applets"
	case app.KindDemo:
		s = "Demos"
	default:
		s = k.String()
	}
	return
}

// topicLabel is a topic's reader-facing label (§SD10). A rendering function
// over the closed vocabulary, not a change to it: the token stays the wire
// value, the introspection column and the `--launch` predicate keep seeing
// `observability`, and only the chip and the section heading read as English.
//
// An unregistered topic cannot reach here through a validated manifest, so the
// default returns the token rather than inventing a label for it.
func topicLabel(t app.TopicT) (s string) {
	switch t {
	case app.TopicRuntime:
		s = "Runtime"
	case app.TopicCode:
		s = "Code"
	case app.TopicTopology:
		s = "Topology"
	case app.TopicObservability:
		s = "Observability"
	case app.TopicData:
		s = "Data"
	case app.TopicSql:
		s = "SQL"
	case app.TopicUi:
		s = "Widgets & UI"
	case app.TopicGeo:
		s = "Maps & terrain"
	case app.TopicSensing:
		s = "Sensors"
	case app.TopicAbout:
		s = "About"
	default:
		s = t.String()
	}
	return
}
