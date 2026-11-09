package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

const auditTrailPrompt = "You analyse this host's audit trail for the person. Read boxer.dm_trail_* and boxer.agg_trail_* in play, and cite the rows behind every number."

// auditTrailPreset is the worked example of ADR-0364: it has to run SQL in a
// play window it opens, and write its findings up in the artefact.
func auditTrailPreset() (p Preset) {
	m := BaseManifest()
	m.Id, m.Display, m.Title, m.Summary = "github.com/stergiotis/boxer/apps/chatlet/audit-trail", "Audit trail analytics", "Audit trail analytics", "Ask what happened on this host's audit trail"
	m.Kind = app.KindApplet
	return Preset{Manifest: m, Book: "chatlet/test", System: auditTrailPrompt, Apps: true, Questions: true, Artefact: true, Rounds: 48,
		Allow: "run", Changes: "apply", Reach: "data", Launch: true}
}

func TestPreset_auditTrailStartsAbleToRunAndNoFurther(t *testing.T) {
	a, err := NewPreset(auditTrailPreset())
	require.NoError(t, err)
	inst := a.(*App)
	assert.Equal(t, "github.com/stergiotis/boxer/apps/chatlet/audit-trail", string(inst.Manifest().Id))
	assert.Equal(t, string(inst.Manifest().Id), inst.owner())
	assert.Equal(t, float64(48), inst.rounds)
	assert.True(t, inst.apps && inst.questions && inst.artefact)

	ceil := inst.perms.ceiling(true)
	assert.Equal(t, agent.ModeAct, ceil.Mode)
	assert.Equal(t, app.OperationEffectRun, ceil.Effect, "play's run must be within the ceiling")
	assert.True(t, ceil.Launch, "it opens its own play window")
	assert.False(t, ceil.Desktop)
	assert.Equal(t, agent.ReachData, ceil.Reach)
	assert.False(t, ceil.Unpaced)

	inst.perms.allow, inst.perms.reach, inst.perms.desktop, inst.perms.unpaced = allowOutside, agent.ReachNetwork, true, true
	got := inst.perms.capped(inst.limitNow())
	assert.Equal(t, allowRun, got.allow, "the person cannot move it past the preset")
	assert.Equal(t, agent.ReachData, got.reach)
	assert.False(t, got.desktop)
	assert.False(t, got.unpaced)
	assert.Equal(t, agent.PixelsNone, got.pixels)

	got = permissions{allow: allowRead, changes: changesAsk, launch: true}.capped(inst.limitNow())
	assert.Equal(t, allowRead, got.allow, "lowering stays the person's")
	assert.Equal(t, changesAsk, got.changes)
}

func TestPreset_plainChatIsUncapped(t *testing.T) {
	inst := newApp()
	assert.Equal(t, string(ManifestId), inst.owner())
	assert.Equal(t, ManifestId, inst.Manifest().Id)
	perms := permissions{allow: allowOutside, changes: changesApply, launch: true, desktop: true, reach: agent.ReachNetwork, unpaced: true, pixels: agent.PixelsCaptures}
	assert.Equal(t, perms, perms.capped(inst.limitNow()))
}

func TestPreset_sectionIsJournaledFirstUnderItsOwner(t *testing.T) {
	p := auditTrailPreset()
	coord := newCoordinator(nil, nil, "c-preset")
	coord.owner, coord.preset = string(p.Manifest.Id), &p
	coord.offer(true, false)
	ctx := context.Background()
	w, err := coord.openJournal(ctx, true)
	require.NoError(t, err)
	assert.Equal(t, string(p.Manifest.Id), w.Run().OwnerApp)
	assert.Equal(t, runFingerprint(profileNow().Fingerprint(), coord.confinedTier, p.digest()), w.Run().Fingerprint)
	require.NoError(t, coord.syncSections(ctx, w))

	var found bool
	for _, e := range w.Entries() {
		if e.Kind == agentrun.KindSection && e.Name == presetSectionName {
			found = true
			assert.Equal(t, p.Book, e.Book)
		}
	}
	require.True(t, found)
	b := projection.Fold(w.Entries(), sectionOrder())
	require.NotEmpty(t, b.Messages)
	assert.Equal(t, projection.PartHead, b.Messages[0].Part)
	head := b.Messages[0].Content
	assert.Less(t, strings.Index(head, auditTrailPrompt), strings.Index(head, "untrusted"), "the harness's rules are read after the scenario's")

	before := len(w.Entries())
	require.NoError(t, coord.syncSections(ctx, w))
	assert.Len(t, w.Entries(), before, "an unchanged section is not written again")
}

func TestPreset_digestPinsTheWords(t *testing.T) {
	var none *Preset
	assert.Empty(t, none.digest())
	a, b := auditTrailPreset(), auditTrailPreset()
	b.System += " Be brief."
	assert.NotEqual(t, a.digest(), b.digest())
	assert.NotEqual(t, runFingerprint("p", agentrun.TierRed, a.digest()), runFingerprint("p", agentrun.TierRed, b.digest()))
	assert.NotEqual(t, runFingerprint("p", agentrun.TierRed, ""), runFingerprint("p", agentrun.TierRed, a.digest()))
}

func TestPreset_refusesWhatItCannotRead(t *testing.T) {
	for name, mutate := range map[string]func(*Preset){
		"allow":   func(p *Preset) { p.Allow = "everything" },
		"changes": func(p *Preset) { p.Changes = "" },
		"reach":   func(p *Preset) { p.Reach = "lan" },
		"system":  func(p *Preset) { p.System = "" },
		"book":    func(p *Preset) { p.Book = "" },
		"rounds":  func(p *Preset) { p.Rounds = -1 },
		"id":      func(p *Preset) { p.Manifest.Id = "" },
		"size":    func(p *Preset) { p.System = strings.Repeat("x", 1<<20) },
	} {
		t.Run(name, func(t *testing.T) {
			p := auditTrailPreset()
			mutate(&p)
			_, err := NewPreset(p)
			require.Error(t, err)
		})
	}
}

func TestBaseManifest_isACopy(t *testing.T) {
	m := BaseManifest()
	require.NotEmpty(t, m.Caps)
	m.Caps[0].Reason = "changed"
	assert.NotEqual(t, "changed", manifest.Caps[0].Reason)
}

func TestPreset_startersAreBounded(t *testing.T) {
	long := strings.Repeat("x", maxStarterLabel+1)
	for name, starters := range map[string][]Starter{
		"no label": {{Text: "t"}},
		"no text":  {{Label: "l"}},
		"long":     {{Label: long, Text: "t"}},
		"twice":    {{Label: "l", Text: "t"}, {Label: "l", Text: "u"}},
		"many":     make([]Starter, maxStarters+1),
	} {
		t.Run(name, func(t *testing.T) {
			p := auditTrailPreset()
			p.Starters = starters
			require.Error(t, p.Validate())
		})
	}
	p := auditTrailPreset()
	p.Starters = []Starter{{Label: "a", Text: "t"}, {Label: "b", Text: "t"}}
	require.NoError(t, p.Validate())
}

func TestPreset_definitionOnlyWithADocument(t *testing.T) {
	assert.Nil(t, newApp().definition, "the plain chat has no Definition tab")
	a, err := NewPreset(auditTrailPreset())
	require.NoError(t, err)
	assert.Nil(t, a.(*App).definition)
	p := auditTrailPreset()
	p.Source = "---\ntitle: x\n---\n\nbody\n"
	a, err = NewPreset(p)
	require.NoError(t, err)
	assert.NotNil(t, a.(*App).definition)
}

// What the window shows is not in what a run is pinned to.
func TestPreset_digestIgnoresWhatTheModelDoesNotRead(t *testing.T) {
	a, b := auditTrailPreset(), auditTrailPreset()
	b.Greeting, b.Source = "hello", "doc"
	b.Starters = []Starter{{Label: "l", Text: "t"}}
	assert.Equal(t, a.digest(), b.digest())
}

// The reasoning is the window's to show or not, and a preset only sets where
// that starts: a plain window shows it, and what the step keeps is untouched.
func TestThinking_startsWhereThePresetSaysAndOnlyHidesTheStream(t *testing.T) {
	reasoning := turnStep{kind: stepModel, reasoning: "weighing it"}
	assert.True(t, newApp().showsReasoning(&reasoning), "a plain window shows it")

	p := auditTrailPreset()
	p.HideThinking = true
	a, err := NewPreset(p)
	require.NoError(t, err)
	inst := a.(*App)
	assert.False(t, inst.showsReasoning(&reasoning))
	assert.True(t, reasoning.hasDetail(), "the finished step still has it to open")

	inst.thinking = true
	assert.True(t, inst.showsReasoning(&reasoning), "the person can turn it on")
	answering := turnStep{kind: stepModel, reasoning: "weighing it", content: "the answer"}
	assert.False(t, inst.showsReasoning(&answering), "the answer takes over once it starts")
	assert.False(t, inst.showsReasoning(&turnStep{kind: stepTool, reasoning: "x"}))
}

// What the empty window says follows from the settings, so it moves when
// they do.
func TestPlainAbilities_followTheSettings(t *testing.T) {
	limit, err := auditTrailPreset().limit()
	require.NoError(t, err)
	lines := limit.plainAbilities(true, true, true)
	require.Len(t, lines, 3)
	assert.Equal(t, "It can open app windows, read, edit and run things in the windows you share, reach this host and its data servers, ask you questions and write a document for you.", lines[0])
	assert.Equal(t, "It cannot publish or export, reach the network or arrange your desktop.", lines[1])

	lowered := limit
	lowered.allow, lowered.changes = allowRead, changesAsk
	lines = lowered.plainAbilities(true, false, true)
	assert.Contains(t, lines[0], "read the windows you share")
	assert.NotContains(t, lines[0], "document", "a read-only window cannot write the artefact")
	assert.NotContains(t, lines[2], "waits for your OK", "reading changes nothing")

	lowered.allow = allowEdit
	assert.Contains(t, lowered.plainAbilities(true, false, false)[2], "Each change waits for your OK.")

	outside := unlimited()
	lines = outside.plainAbilities(true, false, false)
	assert.Contains(t, lines[0], "publish or export once you confirm")
	assert.Len(t, lines, 2, "nothing it cannot do, so no such line")

	assert.Equal(t, []string{"It can talk with you.", "It cannot open or work in windows."}, limit.plainAbilities(false, false, false))
}

func TestTurnNote_aPresetCountsStepsNotAgainstItsLimit(t *testing.T) {
	assert.Equal(t, "round 3 of 48 · asking", turnNote(false, 2, 48, "asking"))
	assert.Equal(t, "step 3 · asking", turnNote(true, 2, 48, "asking"))
	assert.Equal(t, "step 1", turnNote(true, 0, 48, ""))
}

func TestArtefactShown_comesForwardOncePerConversation(t *testing.T) {
	var a artefactShown
	assert.False(t, a.advance("one", false))
	assert.False(t, a.advance("one", false))
	assert.True(t, a.advance("one", true), "the first write")
	assert.False(t, a.advance("one", true), "once")
	assert.False(t, a.advance("two", true), "a resumed conversation stays where it was left")
	assert.False(t, a.advance("two", true))
	assert.False(t, a.advance("three", false))
	assert.True(t, a.advance("three", true), "the next conversation's own first write")
}

func TestPreset_followupsAndPurposeAreBounded(t *testing.T) {
	p := auditTrailPreset()
	p.Followups = []Starter{{Label: "Same", Text: "a"}, {Label: "Same", Text: "b"}}
	err := p.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "two follow-ups share a label")

	p = auditTrailPreset()
	p.Purpose = strings.Repeat("x", maxPurpose+1)
	assert.Error(t, p.Validate())
	p.Purpose = "one line\nand another"
	assert.Error(t, p.Validate())
	p.Purpose = "One short sentence."
	assert.NoError(t, p.Validate())
	base := auditTrailPreset()
	assert.Equal(t, base.digest(), p.digest(), "the model does not read it")
}

// The model is told the server before its first request, so one dialog can
// cover it; a plain chat's note is unchanged, and a window that cannot reach
// the server is not told of one.
func TestCeilingNote_namesTheServerForAPresetThatCanReachIt(t *testing.T) {
	p := auditTrailPreset()
	note := func(preset *Preset, reach agent.ReachE) string {
		coord := newCoordinatorIn(nil, nil, nil, "conv")
		coord.apps, coord.preset, coord.rounds = true, preset, 24
		coord.ceiling = agent.Ceiling{Reach: reach}
		return coord.ceilingNote()
	}
	assert.Contains(t, note(&p, agent.ReachData), "destination "+serverDestination())
	assert.NotContains(t, note(&p, agent.ReachHost), "clickhouse:")
	assert.NotContains(t, note(nil, agent.ReachData), "clickhouse:")
	assert.Equal(t, "clickhouse:ch.example:8123", destinationOfURL("http://ch.example:8123/"))
	assert.Empty(t, destinationOfURL("not a url"))
}

func TestJoinDraft(t *testing.T) {
	assert.Equal(t, "t", joinDraft("", "t"))
	assert.Equal(t, "t", joinDraft("  \n", "t"))
	assert.Equal(t, "d\n\nt", joinDraft("d\n", "t"))
}
