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

func TestJoinDraft(t *testing.T) {
	assert.Equal(t, "t", joinDraft("", "t"))
	assert.Equal(t, "t", joinDraft("  \n", "t"))
	assert.Equal(t, "d\n\nt", joinDraft("d\n", "t"))
}
