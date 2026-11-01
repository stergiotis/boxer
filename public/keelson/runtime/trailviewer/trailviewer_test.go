package trailviewer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func auditAt(id uint64, at time.Time, keys []string, values []string) AuditDetail {
	a := AuditDetail{Id: id, TsNs: at.UnixNano(), Domain: "attention", Action: "level", Entity: "attention-level:k", Run: "run-1",
		AttrKeys: keys, AttrValues: values}
	if v, ok := a.Attr(audit.AttrFrom); ok {
		a.From = v
	}
	if v, ok := a.Attr(audit.AttrFromBy); ok {
		a.FromBy = v
	}
	return a
}

func gapRow(id uint64, written, from, to time.Time) AuditDetail {
	return AuditDetail{Id: id, TsNs: written.UnixNano(), Domain: audit.TrailDomain, Action: audit.ActionAuditGap, Run: "run-1",
		AttrKeys: []string{"rows", "from", "to"}, AttrValues: []string{"4", from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano)}}
}

// SD5's four verdicts on the from claim, on fixture rows, and the row that
// makes no claim.
func TestThePreviousStateVerdictYieldsTheFourStates(t *testing.T) {
	look := 30 * 24 * time.Hour
	prev := auditAt(1, t0, []string{"kind", "level"}, []string{"agent", "ask"})
	cur := auditAt(2, t0.Add(time.Hour), []string{"kind", "level", "from", "from-by"}, []string{"agent", "deny", "ask", "person"})

	p := judgeAudit(&cur, &prev, nil, look)
	assert.Equal(t, ClaimMatches, p.Claim)
	assert.Equal(t, "matches the previous event", p.Verdict)
	assert.True(t, p.Has)
	assert.Equal(t, uint64(1), p.PrevId)
	assert.Equal(t, []FieldDiff{
		{Field: "kind", Before: "agent", After: "agent", Change: "same"},
		{Field: "level", Before: "ask", After: "deny", Change: "changed"},
	}, p.Fields, "the claim's own keys are not diffed")

	lying := auditAt(3, t0.Add(time.Hour), []string{"kind", "level", "from"}, []string{"agent", "deny", "allow"})
	p = judgeAudit(&lying, &prev, nil, look)
	assert.Equal(t, ClaimDiffers, p.Claim)
	assert.Equal(t, `differs from the previous event: the row claims from "allow", the previous event had level=ask`, p.Verdict)

	first := auditAt(4, t0, []string{"level", "from-by"}, []string{"ask", "default"})
	p = judgeAudit(&first, nil, nil, look)
	assert.Equal(t, ClaimNoPrevious, p.Claim)
	assert.Equal(t, "no previous event within the look-back (30 days); the row says its state before came from default", p.Verdict)
	assert.Equal(t, []FieldDiff{{Field: "level", After: "ask", Change: "added"}}, p.Fields)

	gaps := []AuditDetail{
		gapRow(8, t0.Add(-time.Hour), t0.Add(-2*time.Hour), t0.Add(-90*time.Minute)), // before the previous event
		gapRow(9, t0.Add(50*time.Minute), t0.Add(10*time.Minute), t0.Add(20*time.Minute)),
	}
	p = judgeAudit(&cur, &prev, gaps, look)
	assert.Equal(t, ClaimGapBetween, p.Claim)
	assert.Equal(t, uint64(9), p.Gap.Id)
	assert.Contains(t, p.Verdict, "a gap row lies between: 4 rows dropped from 2026-10-31T12:10:00Z")
	p = judgeAudit(&cur, &prev, gaps[:1], look)
	assert.Equal(t, ClaimMatches, p.Claim, "a gap before the previous event is not between")

	silent := auditAt(5, t0.Add(time.Hour), []string{"kind", "level"}, []string{"agent", "deny"})
	p = judgeAudit(&silent, &prev, nil, look)
	assert.Equal(t, ClaimNone, p.Claim)
	assert.Equal(t, "the row makes no from claim", p.Verdict)
}

// A claim rendering several fields as one line (the ration writer's)
// matches when it holds each changed field's previous value.
func TestAOneLineClaimMatchesTheFieldsItRenders(t *testing.T) {
	prev := auditAt(1, t0, []string{"limit", "window"}, []string{"100", "1h"})
	cur := auditAt(2, t0.Add(time.Minute), []string{"limit", "window", "from"}, []string{"200", "2h", "tokens <= 100 per 1h"})
	assert.Equal(t, ClaimMatches, judgeAudit(&cur, &prev, nil, time.Hour).Claim)
	cur = auditAt(2, t0.Add(time.Minute), []string{"limit", "window", "from"}, []string{"200", "2h", "tokens <= 50 per 1h"})
	assert.Equal(t, ClaimDiffers, judgeAudit(&cur, &prev, nil, time.Hour).Claim)
}

func TestAGrantIsDiffedAgainstTheTasksPreviousGrant(t *testing.T) {
	prev := GrantDetail{Id: 1, TsNs: t0.UnixNano(), Task: "task-1", Event: "approved", Destinations: []string{"a"}}
	cur := GrantDetail{Id: 2, TsNs: t0.Add(time.Minute).UnixNano(), Task: "task-1", Event: "widened", Destinations: []string{"a", "b"}}
	p := judgeGrant(&cur, &prev, time.Hour)
	assert.True(t, p.Has)
	assert.Equal(t, ClaimNone, p.Claim)
	changed := map[string]string{}
	for _, f := range p.Fields {
		changed[f.Field] = f.Change
	}
	assert.Equal(t, "changed", changed["event"])
	assert.Equal(t, "changed", changed["destinations"])
	assert.Equal(t, "same", changed["plan"])
	p = judgeGrant(&cur, nil, time.Hour)
	assert.Equal(t, ClaimNoPrevious, p.Claim)
}

func TestTheBreadcrumbOmitsAbsentLinks(t *testing.T) {
	full := &Row{Kind: "agentAction", App: "github.com/x/apps/opsdemo", Instance: 2, Task: "task-1", ParentRef: "call-1", TaskCall: "tc-3",
		Audit: nil}
	assert.Equal(t, "window opsdemo#2 › task task-1 › model call call-1 › tool call tc-3", breadcrumb(full))
	call := &Row{Kind: "llmCall", Ref: "call-9", App: "apps/chat"}
	assert.Equal(t, "window chat › model call call-9", breadcrumb(call))
	ev := &Row{Kind: kindAuditEvent, Ref: "p-1", Instance: 4, Audit: &AuditDetail{Principal: "p-1"}}
	assert.Equal(t, "person p-1 › window #4", breadcrumb(ev))
	assert.Equal(t, "", breadcrumb(&Row{Kind: "httpFetch"}))
}

func TestTheStripFoldsKeysAndStatesItsLine(t *testing.T) {
	b := []Bucket{{Bucket: 60, Key: "llmCall", N: 3}, {Bucket: 60, Key: "auditEvent", N: 1}, {Bucket: 120, Key: "auditEvent", N: 5}}
	keys, index := foldKeys(b)
	require.Equal(t, []string{"auditEvent", "llmCall"}, keys, "busiest first")
	assert.Equal(t, 1, index("llmCall"))
	assert.Equal(t, "9 rows in 2 buckets of 1m0s; busiest 1970-01-01 00:02:00 UTC with 5", stripLine(b, time.Minute))
	assert.Equal(t, "0 rows in the window", stripLine(nil, time.Minute))
	var many []Bucket
	for i, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		many = append(many, Bucket{Bucket: 0, Key: k, N: uint64(10 - i)})
	}
	keys, index = foldKeys(many)
	assert.Len(t, keys, stripKeys)
	assert.Equal(t, "other", keys[stripKeys-1])
	assert.Equal(t, stripKeys-1, index("h"), "a key past the busiest is other")
	assert.Equal(t, stripKeys-1, index("g"))
}

// A click on a strip bar narrows to the bucket ClickHouse counted, whose
// start toStartOfInterval aligns to the Unix epoch, not to year 1 as
// time.Truncate would.
func TestAStripClickNarrowsToTheBucketClicked(t *testing.T) {
	week := 7 * 24 * time.Hour
	for _, step := range []time.Duration{time.Minute, 24 * time.Hour, week, 2 * week, 4 * week} {
		s := int64(step / time.Second)
		// A bucket as ClickHouse returns it, clicked at its bar's middle
		// and near either edge.
		bucket := t0.Unix() / s * s
		for _, x := range []float64{float64(bucket) + float64(s)/2, float64(bucket) + 1, float64(bucket+s) - 1} {
			assert.Equal(t, time.Unix(bucket, 0).UTC(), bucketStart(x, step), "step %s x %v", step, x)
		}
	}
	assert.Equal(t, time.Unix(-week.Milliseconds()/1000, 0).UTC(), bucketStart(-1, week), "before the epoch it floors")
}

func TestARangeEndingNowIsReadAsASpan(t *testing.T) {
	d, ok := relativeSpan("anchor_now - INTERVAL 15 MINUTE", "anchor_now")
	assert.True(t, ok)
	assert.Equal(t, 15*time.Minute, d)
	d, ok = relativeSpan("anchor_now - INTERVAL 2 DAY", " anchor_now ")
	assert.True(t, ok)
	assert.Equal(t, 48*time.Hour, d)
	for _, unit := range []string{"days", "Days", "DAYS", "day", "Day"} {
		d, ok = relativeSpan("anchor_now - INTERVAL 7 "+unit, "anchor_now")
		assert.True(t, ok, unit)
		assert.Equal(t, 7*24*time.Hour, d, unit)
	}
	d, ok = relativeSpan("anchor_now - interval 90 Minutes", "anchor_now")
	assert.True(t, ok)
	assert.Equal(t, 90*time.Minute, d)
	_, ok = relativeSpan("toStartOfDay(anchor_now)", "anchor_now")
	assert.False(t, ok)
	_, ok = relativeSpan("anchor_now - INTERVAL 1 DAY", "toStartOfDay(anchor_now)")
	assert.False(t, ok)
}

func TestTheFilterKeyRoundTripsAndCovers(t *testing.T) {
	f := Filter{}.with(FacetKind, "auditEvent", false).with(FacetDomainAction, `a"b=c;d`, true)
	back, ok := parseFilterKey(f.key())
	require.True(t, ok)
	assert.Equal(t, f.key(), back.key())
	empty, ok := parseFilterKey("")
	assert.True(t, ok)
	assert.Empty(t, empty.Terms)
	_, ok = parseFilterKey("nonsense")
	assert.False(t, ok)

	assert.True(t, filterCovers(Filter{}, f), "no filter covers any")
	assert.False(t, filterCovers(f, Filter{}), "a narrower read does not cover a wider list")
	kinds := Filter{}.with(FacetKind, "a", false).with(FacetKind, "b", false)
	assert.True(t, filterCovers(kinds, Filter{}.with(FacetKind, "a", false)))
	assert.False(t, filterCovers(Filter{}.with(FacetKind, "a", false), kinds))
	excl := Filter{}.with(FacetStatus, "ok", true)
	assert.True(t, filterCovers(excl, excl))
	assert.True(t, filterCovers(excl, Filter{}.with(FacetStatus, "failed", false)), "keeping failed only drops ok too")
	assert.False(t, filterCovers(excl, Filter{}))

	assert.Equal(t, f.with(FacetKind, "auditEvent", true).key(), Filter{}.with(FacetDomainAction, `a"b=c;d`, true).with(FacetKind, "auditEvent", true).key(),
		"a modified click replaces the value's term")
}

func TestCasesValidate(t *testing.T) {
	for _, c := range fixtureCases {
		assert.NoError(t, c.validate(), c.String())
	}
	for _, c := range []Case{{Kind: CaseRun}, {Kind: CaseWindow, Value: "run-1"}, {Kind: CaseSubject, Value: "x"},
		{Kind: CaseEntity, Value: "noType"}, {Kind: CaseAll, Value: "x"}} {
		assert.Error(t, c.validate(), c.String())
	}
	run, n, ok := Case{Kind: CaseWindow, Value: "a:b:12"}.window()
	assert.True(t, ok)
	assert.Equal(t, "a:b", run)
	assert.Equal(t, uint64(12), n)
}
