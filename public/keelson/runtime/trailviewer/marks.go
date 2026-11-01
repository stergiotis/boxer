package trailviewer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Review marks (ADR-0350 §SD7, proposed): a person's mark on a trail row,
// written back to the trail as an audit event in the review domain through
// the host's recorder, so it sits on the same sealed record as what it
// reviews and dm_trail_reviews lists it. A mark is the person's act: the
// command is consequential and not offered to agents, and refuses a call
// an agent's task makes.

// MarkE is a review mark.
type MarkE uint8

const (
	MarkReviewed MarkE = iota + 1
	MarkFlagged
	MarkDismissed
)

// AllMarks lists the marks in the order the detail pane offers them.
var AllMarks = []MarkE{MarkReviewed, MarkFlagged, MarkDismissed}

func (inst MarkE) String() string {
	switch inst {
	case MarkReviewed:
		return trail.ReviewActionReviewed
	case MarkFlagged:
		return trail.ReviewActionFlagged
	case MarkDismissed:
		return trail.ReviewActionDismissed
	}
	return "unknown"
}

// ParseMark reads a mark's name.
func ParseMark(s string) (m MarkE, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, x := range AllMarks {
		if x.String() == s {
			return x, true
		}
	}
	return
}

// principalDomain separates the reviewer's reference from any other hash of
// the same inputs.
const principalDomain = "boxer/trailviewer/review-principal/v1"

// osPrincipal is the person's principal on a review mark: a pseudonymous
// reference to the OS user of the process on this host, never the user's
// name. It is the first 16 bytes, hex, of SHA-256 over the domain
// separator, the OS user name and the host name, each followed by a NUL;
// so a reader can tell two reviewers apart and match one reviewer's marks
// on one host, and cannot read who it is without the inputs. The tree has
// no helper yielding such a reference; this is the viewer's own.
var osPrincipal = sync.OnceValue(func() string {
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	if name == "" {
		name = os.Getenv("USER")
	}
	host, _ := os.Hostname()
	h := sha256.New()
	for _, part := range []string{principalDomain, name, host} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return "os-" + hex.EncodeToString(h.Sum(nil)[:16])
})

// MarkRowArgs marks one row.
type MarkRowArgs struct {
	Id     uint64 `desc:"the row's facts id"`
	Mark   string `desc:"reviewed, flagged or dismissed"`
	Reason string `desc:"why; required for dismissed"`
}

// MarkRowResult is what mark_row wrote.
type MarkRowResult struct {
	Id        uint64    `desc:"the marked row"`
	Mark      string    `desc:"the mark"`
	Rule      string    `desc:"the exception rules the mark clears, comma-separated; empty when the row had none open"`
	Principal string    `desc:"the reviewer's pseudonymous reference"`
	At        time.Time `desc:"the mark's time"`
}

// heldRow is a row the window holds: the table's, or the selected row.
func (inst *App) heldRow(id uint64) (r Row, ok bool) {
	for i := range inst.cache.rows {
		if inst.cache.rows[i].Id == id {
			return inst.cache.rows[i], true
		}
	}
	if inst.hasSelected && inst.selected.Id == id {
		return inst.selected, true
	}
	return
}

func (inst *App) appIdOr() string {
	if inst.appId != "" {
		return inst.appId
	}
	return string(inst.manifest.Id)
}

// markRow writes a review mark for a held row through the source's
// recorder, under the person's principal, and holds it as pending until a
// refill reads it back from dm_trail_reviews.
func (inst *App) markRow(call app.OperationCall, in MarkRowArgs) (out MarkRowResult, err error) {
	inst.markReached = true
	if call.OnBehalfOf != nil || strings.HasPrefix(call.Writer, "task:") {
		err = app.RefuseOperation("a review mark is the person's act; an agent's task does not mark rows (ADR-0350 §SD7)")
		return
	}
	mark, ok := ParseMark(in.Mark)
	if !ok {
		err = app.RefuseOperation("mark is one of reviewed, flagged, dismissed")
		return
	}
	reason := strings.TrimSpace(in.Reason)
	if mark == MarkDismissed && reason == "" {
		err = app.RefuseOperation("a dismissal says why: reason is required for dismissed")
		return
	}
	row, ok := inst.heldRow(in.Id)
	if !ok || in.Id == 0 {
		err = app.RefuseOperation(rowGoneReason)
		return
	}
	src := sourceRef.Load()
	switch {
	case src == nil || src.Trail == nil:
		err = app.RefuseOperation("the host registered no recorder, so a mark would not be recorded")
		return
	case src.Trail.NotDurable() != "":
		err = app.RefuseOperation("the trail is not durable on this host (" + src.Trail.NotDurable() + "), so a mark would not be recorded")
		return
	}
	rules := ruleNames(inst.exceptions().byRow[row.Id])
	ev := trail.AuditEvent{Domain: trail.ReviewDomain, Action: mark.String(), Outcome: trail.OutcomeOk, PrincipalBy: trail.PrincipalByOs,
		Retention: trail.RetentionTrail, RefTypes: []string{trail.ReviewRefTrailRow}, RefValues: []string{strconv.FormatUint(row.Id, 10)}}
	if reason != "" {
		ev.AttrKeys, ev.AttrValues = append(ev.AttrKeys, trail.ReviewAttrReason), append(ev.AttrValues, trail.Bounded(reason))
	}
	if rules != "" {
		ev.AttrKeys, ev.AttrValues = append(ev.AttrKeys, trail.ReviewAttrRule), append(ev.AttrValues, trail.Bounded(rules))
	}
	appId := inst.appIdOr()
	principal := osPrincipal()
	ctx := callident.WithCallIdentity(context.Background(), callident.CallIdentity{
		Origin: callident.Origin{Run: src.run(), App: appId, Instance: inst.instance},
		Claims: callident.Claims{Principal: principal, Purpose: Purpose},
	})
	at := time.Now().UTC()
	if err = src.Trail.Event(ctx, at, trail.Context{Origin: src.Trail.OriginOf(app.AppIdT(appId), inst.instance)}, ev); err != nil {
		err = eh.Errorf("trailviewer: write the review mark: %w", err)
		return
	}
	src.Trail.FlushSoon()
	if inst.pending == nil {
		inst.pending = map[uint64]Review{}
	}
	inst.pending[row.Id] = Review{Id: row.Id, TsNs: at.UnixNano(), Mark: mark.String(), Reason: reason, Rule: rules, Principal: principal, Marks: 1}
	inst.pendingVer++
	inst.cache.refreshNow = true
	out = MarkRowResult{Id: row.Id, Mark: mark.String(), Rule: rules, Principal: principal, At: at}
	return
}

// marks is the held rows' latest marks: the reviews a refill read, and
// the marks written since that it has not read back yet.
func (inst *App) marks() (m map[uint64]Review) {
	if len(inst.pending) == 0 {
		return inst.cache.reviews
	}
	m = make(map[uint64]Review, len(inst.cache.reviews)+len(inst.pending))
	for k, v := range inst.cache.reviews {
		m[k] = v
	}
	for k, v := range inst.pending {
		if cur, ok := m[k]; !ok || cur.TsNs < v.TsNs {
			m[k] = v
		}
	}
	return
}

// settlePending drops the pending marks a refill has read back.
func (inst *App) settlePending() {
	for k, v := range inst.pending {
		if cur, ok := inst.cache.reviews[k]; ok && cur.TsNs >= v.TsNs {
			delete(inst.pending, k)
			inst.pendingVer++
		}
	}
}

// exceptions is the rules' reading of the held rows with the marks
// applied, recomputed when the rows, the verdict lines or the marks move.
func (inst *App) exceptions() exceptionSet {
	key := excKey{gen: inst.cache.gen, lines: inst.verdicts.at, pending: inst.pendingVer}
	if inst.excBuilt && inst.excKey == key {
		return inst.exc
	}
	inst.exc = evaluateExceptions(inst.cache.rows, inst.cache.exceptions, inst.cache.claims, inst.verdicts.lines, inst.marks())
	inst.excKey, inst.excBuilt = key, true
	inst.excVer++
	return inst.exc
}

type excKey struct {
	gen     uint64
	lines   time.Time
	pending uint64
}
