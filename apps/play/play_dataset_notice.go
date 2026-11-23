package play

// play_dataset_notice.go — what a launched window says it waits for, over
// its empty panes (ADR-0134 §SD4, ADR-0240 §SD7). Two things wait: the
// bundle the window follows (play_bundle.go) and the ad-hoc dataset aliases
// its follower keeps bound — those the launch config declared and those a
// task asked for with bind_dataset or a query reading keelson('<alias>').
// Each keeps its own text here, and one writer composes them into play's
// single notice strip, so neither wipes the other's, and play reparses the
// markdown only when the composed text changes.
//
// The text says why an alias waits as the follower last heard it — nothing
// live, withdrawn, or no answer — rather than a guess, and what happens when
// it binds: the window runs again for a launch alias, and nothing for one a
// task asked for, whose run is the task's. An alias the service has not been
// asked about yet is left out, so an alias that binds on its first answer
// does not flash a notice. play cannot know what produces an alias; a
// launcher that wants to say so seeds a comment in its SQL.

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
)

// datasetNotices is the launcher's two sources of the notice and the text
// last handed to play.
type datasetNotices struct {
	bundle   []byte
	datasets []byte
	shown    []byte
}

// show hands p the composed notice when it differs from the one shown.
func (inst *datasetNotices) show(p *PlayApp) {
	var parts [][]byte
	for _, part := range [][]byte{inst.bundle, inst.datasets} {
		if len(part) > 0 {
			parts = append(parts, part)
		}
	}
	md := bytes.Join(parts, []byte("\n\n"))
	if bytes.Equal(md, inst.shown) {
		return
	}
	inst.shown = md
	p.SetDatasetNotice(md)
}

// syncDatasets is the frame's dataset step: the bundle, then the follower
// — binding what arrived, and running the buffer when a launch alias binds
// — then the notice both leave.
func (inst *PlayLauncher) syncDatasets() {
	bundleWaits := inst.syncBundle()
	if inst.follower != nil {
		before := inst.launchBindings()
		bound, pendingChanged := inst.follower.Sync(inst.inner)
		if pendingChanged {
			inst.notices.datasets = inst.followerNotice()
		}
		if bound && inst.launchBindings() != before {
			// AutoRun already fired against the unbound buffer at open, so
			// a launch alias bound since — or bound to the dataset that
			// replaced a withdrawn one — needs its own run to become
			// visible. An alias a task asked for runs when the task asks
			// for it, under its limits.
			inst.inner.RequestRun()
		}
	}
	inst.notices.show(inst.inner)
	if inst.inner != nil {
		inst.inner.dataWait = bundleWaits || (inst.follower != nil && inst.follower.HasPending())
	}
}

// followerNotice is the follower's part of the notice.
func (inst *PlayLauncher) followerNotice() (md []byte) {
	if inst.follower == nil {
		return nil
	}
	return datasetWaitNotice(inst.follower.Waiting(), inst.follower.LocalName, func(local string) bool {
		return slices.Contains(inst.launchAliases, local)
	})
}

// datasetWaitNotice says which aliases the window waits for and why. waiting
// maps an alias to why it waits, localName gives the name the buffer reads
// it by, and launch tells a launch config's alias by that name.
func datasetWaitNotice(waiting map[string]string, localName func(alias string) string, launch func(local string) bool) (md []byte) {
	type line struct{ name, text string }
	var lines []line
	for _, alias := range slices.Sorted(maps.Keys(waiting)) {
		why := waiting[alias]
		if why == adhocdata.WaitNotAsked {
			continue
		}
		local := localName(alias)
		name := "`" + alias + "`"
		if local != alias {
			name += " (read as `" + local + "`)"
		}
		then := "The window runs again once it is bound."
		if !launch(local) {
			then = "A task asked for it here; binding it runs nothing."
		}
		lines = append(lines, line{name: name, text: capitalize(why) + ". " + then})
	}
	switch len(lines) {
	case 0:
		return nil
	case 1:
		return fmt.Appendf(nil, "**Waiting for dataset %s.** %s", lines[0].name, lines[0].text)
	}
	var b strings.Builder
	b.WriteString("**Waiting for datasets.**\n")
	for _, l := range lines {
		b.WriteString("\n- " + l.name + ": " + l.text)
	}
	return []byte(b.String())
}

// capitalize upper-cases s's first ASCII letter.
func capitalize(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}

// launchBindings is the handles the launch config's aliases are bound to,
// in their order, "" for one unbound: it moves when one binds, and when one
// is bound to the dataset that replaced its withdrawn one.
func (inst *PlayLauncher) launchBindings() (digest string) {
	if len(inst.launchAliases) == 0 || inst.inner == nil || inst.inner.client == nil {
		return
	}
	handles := make([]string, len(inst.launchAliases))
	for i, local := range inst.launchAliases {
		handles[i] = inst.inner.client.boundHandle(local)
	}
	return strings.Join(handles, ",")
}
