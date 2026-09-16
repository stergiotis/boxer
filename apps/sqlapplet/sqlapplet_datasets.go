// sqlapplet_datasets.go is the applet host's side of dataset following
// (ADR-0240 §SD6). The follower itself — events as hints, resolve as truth,
// replay in order, the reconcile tick — is adhocdata.Follower; what stays
// here is the applet's knobs and the one thing only the applet can write:
// the notice it shows over empty panes while an alias has no live dataset.

package sqlapplet

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// newDatasetFollower builds the follower for a standalone applet's declared
// aliases under the applet's fault-injection and interval knobs. nil (and
// nil bindings) when nothing is declared or there is no bus.
func newDatasetFollower(bus app.BusI, logger zerolog.Logger, aliases []string) (f *adhocdata.Follower, bindings map[string]string) {
	return adhocdata.NewFollower(adhocdata.FollowerConfig{
		Bus:       bus,
		Log:       logger,
		Aliases:   aliases,
		Reconcile: DatasetReconcile.Get(),
		Events:    adhocdata.ParseEventsMode(DatasetEvents.Get()),
	})
}

// renderDatasetNotice builds the markdown play shows over the empty result
// panes. It names the aliases, because that is the identifier the failing
// query reports and the catalog lists, and appends the author's
// `datasets_hint` — the only part that can say how to produce the data. An
// empty alias set renders nothing: the condition has cleared.
func renderDatasetNotice(aliases []string, hint string) (md []byte) {
	if len(aliases) == 0 {
		return nil
	}
	quoted := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		quoted = append(quoted, "`"+alias+"`")
	}
	noun, verb := "dataset", "is"
	if len(aliases) > 1 {
		noun, verb = "datasets", "are"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**Waiting for %s %s.** No dataset %s live under that alias — none published yet, or its producer withdrew it — so this applet has nothing to query.",
		noun, strings.Join(quoted, ", "), verb)
	if hint != "" {
		b.WriteString(" ")
		b.WriteString(hint)
	}
	b.WriteString(" The window binds it and re-runs by itself once it appears — no need to reopen.")
	return []byte(b.String())
}
