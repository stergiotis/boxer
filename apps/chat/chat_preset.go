package chat

import (
	"encoding/hex"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// A preset is the chat specialised for one scenario: an app of its own, one
// more section of the system prompt, and the settings the window starts at
// (ADR-0364, proposed). It is what a chatlet is minted into; the document
// format and the minter are not this package's.
//
// A preset can only narrow. Its permissions are where a window starts and
// the most the person can move it to, the ceiling stays the one the host
// enforces, and a preset grants nothing: the model still asks, and the
// person still decides (ADR-0310 §SD3).

// presetSectionName is the journal's name for a preset's section. It stands
// first in sectionOrder, so the harness's own sections, the rule about
// untrusted content among them, are read after it.
const presetSectionName = "chatlet"

// Preset is one scenario's specialisation of the chat.
type Preset struct {
	// Manifest is the app the window is: BaseManifest with the identity and
	// the catalogue fields replaced. Runs, rosters, rations and trail rows
	// are keyed by its id.
	Manifest app.Manifest
	// Book is the journal's name for where System came from.
	Book string
	// System is the scenario's part of the system prompt.
	System string
	// Apps, Questions and Artefact are what the conversation offers, as the
	// person would tick them in Settings before the first send.
	Apps      bool
	Questions bool
	Artefact  bool
	// Rounds is where a turn's rounds start, clamped to the profile's range;
	// zero is BOXER_CHAT_ROUNDS.
	Rounds int
	// Allow is read, view, edit, run or outside; Changes ask or apply; Reach
	// host, data or network. Each is the window's start and its most, as are
	// Launch and Desktop. A preset's window is paced and shows the model no
	// screenshots; neither is a setting here until a scenario needs it.
	Allow   string
	Changes string
	Reach   string
	Launch  bool
	Desktop bool
	// Greeting is what the empty transcript says; Starters the ready-made
	// first messages it offers. A starter fills the composer and the person
	// sends it. Neither is the model's to read.
	Greeting string
	Starters []Starter
	// Followups are ready-made next messages, under the latest answer. As
	// with a starter, a click fills the composer and nothing is sent.
	Followups []Starter
	// Purpose is one sentence the host's access dialog shows beside what
	// the model asks for, as the app's own description of itself. It asks
	// for nothing and approves nothing.
	Purpose string
	// Watch starts a conversation watched, so a turn that ends while the
	// person is elsewhere is reported; they can stop watching.
	Watch bool
	// Simple folds what a person only sometimes needs under Details. What
	// holds a task, what it read and Stop stay in view.
	Simple bool
	// HideThinking starts the window without the reasoning drawn while the
	// model writes it. It is a start and no limit: only the window's look
	// changes, and the person can show it from Settings.
	HideThinking bool
	// Source is the document the preset was made from, shown in the
	// Definition tab so the person can read what the model is told. Empty, the
	// tab is not offered.
	Source string
	// Stops are the scenario's stops (ADR-0370, proposed): in force in every
	// conversation of the window, and not the person's to remove or weaken.
	// They are not the model's to read, so they are outside the run's
	// fingerprint, as the greeting is.
	Stops []Stop
}

// Starter is one ready-made first message.
type Starter struct {
	Label string
	Text  string
}

// The empty state has room for a short list of short buttons.
const (
	maxStarters     = 6
	maxStarterLabel = 40
	maxPurpose      = 200
)

// presetWindowWidth leaves the column room for its five titles with
// Definition among them, a little longer than the Analytics it stands in for.
const presetWindowWidth = chatWindowWidth + 10

// BaseManifest is the chat's manifest for a preset to build on: the
// capabilities, the operations and the help are the chat's, so a preset
// cannot widen what the host's review of the chat saw.
func BaseManifest() (m app.Manifest) {
	m = manifest
	m.Caps = slices.Clone(manifest.Caps)
	m.Topics = slices.Clone(manifest.Topics)
	m.Keywords = slices.Clone(manifest.Keywords)
	m.PersistedKeys = slices.Clone(manifest.PersistedKeys)
	m.SurfaceHints.PreferredWidth = presetWindowWidth
	return
}

// NewPreset is a window of the chat specialised by p; a preset that does
// not validate is refused.
func NewPreset(p Preset) (a app.AppI, err error) {
	if err = p.Validate(); err != nil {
		return
	}
	limit, err := p.limit()
	if err != nil {
		return
	}
	stops, err := compileStops(p.Stops, stopOriginPreset)
	if err != nil {
		return
	}
	inst := newApp()
	inst.stops.preset = stops
	// A seeded stop does not take a name the scenario uses.
	inst.stops.own = slices.DeleteFunc(inst.stops.own, func(s stop) bool { _, taken := stops.find(s.spec.Name); return taken })
	inst.preset, inst.limit = &p, limit
	inst.apps, inst.questions, inst.artefact, inst.perms = p.Apps, p.Questions, p.Artefact, limit
	inst.rounds = float64(clampRounds(p.Rounds))
	inst.thinking = !p.HideThinking
	inst.simple = p.Simple
	if p.Source != "" {
		inst.definition = markdown.Parse([]byte(p.Source))
	}
	return inst, nil
}

// Validate reports what a window could not be made from. A minter calls it
// to refuse a document without building a window.
func (inst Preset) Validate() (err error) {
	if _, err = inst.limit(); err != nil {
		return
	}
	if inst.Book == "" || inst.System == "" {
		return eb.Build().Str("id", string(inst.Manifest.Id)).Errorf("a preset needs a book and a system prompt")
	}
	if inst.Rounds < 0 {
		return eb.Build().Str("id", string(inst.Manifest.Id)).Int("rounds", inst.Rounds).Errorf("a preset's rounds are negative")
	}
	if err = inst.validateButtons("starter", inst.Starters); err != nil {
		return
	}
	if err = inst.validateButtons("follow-up", inst.Followups); err != nil {
		return
	}
	if utf8.RuneCountInString(inst.Purpose) > maxPurpose || strings.Contains(inst.Purpose, "\n") {
		return eb.Build().Str("id", string(inst.Manifest.Id)).Int("max", maxPurpose).Errorf("a preset's purpose is one short sentence")
	}
	if _, err = compileStops(inst.Stops, stopOriginPreset); err != nil {
		return eb.Build().Str("id", string(inst.Manifest.Id)).Errorf("the preset's stops: %w", err)
	}
	if err = inst.Manifest.Validate(); err != nil {
		return eb.Build().Str("id", string(inst.Manifest.Id)).Errorf("the preset's manifest: %w", err)
	}
	// The section joins the head compaction has to leave room for.
	if err = profileNow().validateHead(len(inst.System)); err != nil {
		return eb.Build().Str("id", string(inst.Manifest.Id)).Errorf("the preset's system prompt: %w", err)
	}
	return
}

// validateButtons bounds a list of ready-made messages; what names them says
// which list a refusal is about.
func (inst Preset) validateButtons(what string, list []Starter) (err error) {
	id := string(inst.Manifest.Id)
	if len(list) > maxStarters {
		return eb.Build().Str("id", id).Int("count", len(list)).Int("max", maxStarters).Errorf("a preset has too many %ss", what)
	}
	seen := make(map[string]struct{}, len(list))
	for _, s := range list {
		if s.Label == "" || s.Text == "" {
			return eb.Build().Str("id", id).Str("label", s.Label).Errorf("a %s needs a label and a text", what)
		}
		if utf8.RuneCountInString(s.Label) > maxStarterLabel {
			return eb.Build().Str("id", id).Str("label", s.Label).Int("max", maxStarterLabel).Errorf("a %s's label is too long", what)
		}
		if _, dup := seen[s.Label]; dup {
			return eb.Build().Str("id", id).Str("label", s.Label).Errorf("two %ss share a label", what)
		}
		seen[s.Label] = struct{}{}
	}
	return
}

// limit reads the preset's permissions, refusing a name it does not know:
// reading an unknown one as a default would let a typo widen a limit.
func (inst Preset) limit() (p permissions, err error) {
	var ok bool
	if p.allow, ok = presetAllows[inst.Allow]; !ok {
		return p, eb.Build().Str("id", string(inst.Manifest.Id)).Str("allow", inst.Allow).Errorf("no such allow level")
	}
	if p.changes, ok = presetChanges[inst.Changes]; !ok {
		return p, eb.Build().Str("id", string(inst.Manifest.Id)).Str("changes", inst.Changes).Errorf("no such changes setting")
	}
	ok = false
	for _, r := range agent.AllReaches {
		if r.String() == inst.Reach {
			p.reach, ok = r, true
		}
	}
	if !ok {
		return p, eb.Build().Str("id", string(inst.Manifest.Id)).Str("reach", inst.Reach).Errorf("no such reach")
	}
	p.launch, p.desktop = inst.Launch, inst.Desktop
	p.pixels = agent.PixelsNone
	return
}

var (
	presetAllows  = map[string]allowE{"read": allowRead, "view": allowView, "edit": allowEdit, "run": allowRun, "outside": allowOutside}
	presetChanges = map[string]changesE{"ask": changesAsk, "apply": changesApply}
)

// digest is what a run is pinned to for the preset's words, empty without a
// preset: a changed prompt leaves the runs made under the old one read-only,
// as a changed prompt book does (ADR-0308 §SD5).
func (inst *Preset) digest() string {
	if inst == nil {
		return ""
	}
	h := blake3.New(32, nil)
	for _, p := range []string{string(inst.Manifest.Id), inst.Book, inst.System} {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// unlimited is the limit of a window without a preset: every setting's
// highest, so capping changes nothing and Settings offers every option.
func unlimited() permissions {
	return permissions{allow: allowOutside, changes: changesApply, launch: true, desktop: true, reach: agent.ReachNetwork,
		unpaced: true, pixels: agent.PixelsCaptures}
}

// capped is the settings within limit. Only-local is a restriction, so it
// stays as the person set it.
func (inst permissions) capped(limit permissions) permissions {
	inst.allow = min(inst.allow, limit.allow)
	inst.changes = min(inst.changes, limit.changes)
	inst.launch = inst.launch && limit.launch
	inst.desktop = inst.desktop && limit.desktop
	inst.reach = min(inst.reach, limit.reach)
	inst.unpaced = inst.unpaced && limit.unpaced
	inst.pixels = min(inst.pixels, limit.pixels)
	return inst
}

// serverDestination is how a grant names the server a play window opens on,
// empty when that is not a URL with a host.
func serverDestination() string { return destinationOfURL(clickhouseenv.URL.Get()) }

func destinationOfURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return "clickhouse:" + u.Host
}

// owner is the app this window's runs belong to.
func (inst *App) owner() string {
	if inst.embed != nil {
		// The host is the app the bus and the run journal know.
		return string(inst.embed.host)
	}
	if inst.preset != nil {
		return string(inst.preset.Manifest.Id)
	}
	return string(ManifestId)
}

// limitNow is the most the window's settings may reach.
func (inst *App) limitNow() permissions {
	if inst.preset != nil {
		return inst.limit
	}
	return unlimited()
}

// ownerApp is the app this conversation's run belongs to.
func (inst *coordinator) ownerApp() string {
	if inst.owner != "" {
		return inst.owner
	}
	return string(ManifestId)
}
