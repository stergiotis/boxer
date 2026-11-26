package linkhost

import (
	"context"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
)

// Followed is one follow, as the trail records it (ADR-0409 §SD9).
type Followed struct {
	At      time.Time
	Link    string
	Surface app.AppIdT
	Target  app.AppIdT
	// Record is the link resolved; zero when it did not resolve.
	Record applink.Record
	Reply  applink.FollowReply
}

// TrailRecorder writes each follow as an audit event (ADR-0296) on the
// trail: the call's digest rather than the link, which may carry text the
// trail should not.
type TrailRecorder struct {
	Rec *trail.Recorder
	Log zerolog.Logger
}

// Followed implements RecorderI.
func (inst TrailRecorder) Followed(ctx context.Context, f Followed) {
	if inst.Rec == nil || !(inst.Rec.Durable() || inst.Rec.Held()) {
		return
	}
	row := EventOf(f)
	c := trail.Context{Origin: inst.Rec.OriginOf(f.Surface, 0)}
	if err := inst.Rec.Event(ctx, f.At.UTC(), c, row); err != nil {
		inst.Log.Warn().Err(err).Str("decision", f.Reply.Decision).Msg("linkhost: write the follow's audit event")
	}
	inst.Rec.FlushSoon()
}

// EventOf is a follow's audit event: the surface it was clicked on, the
// app and window it reached, the call by digest, operation and version, the
// author's label, the task whose grant it named, and the decision.
func EventOf(f Followed) (row trail.AuditEvent) {
	outcome := trail.OutcomeOk
	if f.Reply.Decision == applink.DecisionRefused || f.Reply.Decision == applink.DecisionConflict {
		outcome = trail.OutcomeDenied
	}
	form := "inline"
	if f.Record.Task != "" || f.Record.Window != 0 {
		form = "handle"
	}
	row = trail.AuditEvent{Domain: "link", Action: f.Reply.Decision, Outcome: outcome, Retention: trail.RetentionTrail,
		RefTypes:  []string{"surface", "app", "window"},
		RefValues: []string{trail.Bounded(string(f.Surface)), trail.Bounded(string(f.Target)), strconv.FormatUint(f.Reply.Window, 10)},
		AttrKeys:  []string{"digest", "form", "operation", "version", "author-level", "author-tier", "authorless"},
		AttrValues: []string{f.Record.Digest(), form, trail.Bounded(f.Record.Operation), strconv.Itoa(int(f.Record.Version)),
			strconv.Itoa(int(f.Record.AuthorLevel)), strconv.Itoa(int(f.Record.AuthorTier)), strconv.FormatBool(f.Reply.Authorless)}}
	if f.Record.Task != "" {
		row.AttrKeys, row.AttrValues = append(row.AttrKeys, "task"), append(row.AttrValues, trail.Bounded(f.Record.Task))
	}
	if f.Reply.Reason != "" {
		row.AttrKeys, row.AttrValues = append(row.AttrKeys, "reason"), append(row.AttrValues, trail.Bounded(f.Reply.Reason))
	}
	return
}
