package trailviewer

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// The previous-state diff (ADR-0350 §SD5, proposed). An audit event that
// changes state names its entity first and carries the entity's whole
// state after it as attributes, so the state before is the previous event
// of the same (domain, entity); the diff is a field table over the two
// events' attributes, and the verdict is on the row's own `from` claim.
// Everything here is pure: the reads are the probe's.

// ChangeE is how one field moved.
type ChangeE uint8

const (
	ChangeSame ChangeE = iota
	ChangeAdded
	ChangeRemoved
	ChangeChanged
)

var changeNames = [...]string{"same", "added", "removed", "changed"}

func (inst ChangeE) String() string {
	if int(inst) < len(changeNames) {
		return changeNames[inst]
	}
	return "unknown"
}

// FieldDiff is one row of the field table.
type FieldDiff struct {
	Field  string `desc:"the attribute's key"`
	Before string `desc:"its value on the previous event; empty when it had none"`
	After  string `desc:"its value on this event; empty when it has none"`
	Change string `desc:"same, added, removed or changed"`
}

// claimKeys are the attributes that state the claim, not the state; they
// are not diffed.
var claimKeys = []string{audit.AttrFrom, audit.AttrFromBy}

// diffFields is the field table: this event's keys in its order, then the
// keys only the previous event had.
func diffFields(beforeK, beforeV, afterK, afterV []string) (out []FieldDiff) {
	at := func(ks, vs []string, k string) (string, bool) {
		i := slices.Index(ks, k)
		if i < 0 || i >= len(vs) {
			return "", false
		}
		return vs[i], true
	}
	for i, k := range afterK {
		if slices.Contains(claimKeys, k) || i >= len(afterV) {
			continue
		}
		b, had := at(beforeK, beforeV, k)
		d := FieldDiff{Field: k, Before: b, After: afterV[i]}
		switch {
		case !had:
			d.Change = ChangeAdded.String()
		case b != afterV[i]:
			d.Change = ChangeChanged.String()
		default:
			d.Change = ChangeSame.String()
		}
		out = append(out, d)
	}
	for i, k := range beforeK {
		if slices.Contains(claimKeys, k) || slices.Contains(afterK, k) || i >= len(beforeV) {
			continue
		}
		out = append(out, FieldDiff{Field: k, Before: beforeV[i], Change: ChangeRemoved.String()})
	}
	return
}

// ClaimE is the verdict on a row's `from` claim — the four states of SD5
// and the row that makes no claim.
type ClaimE uint8

const (
	// ClaimNone is a row with a previous event and no `from` attribute.
	ClaimNone ClaimE = iota
	ClaimMatches
	ClaimDiffers
	ClaimNoPrevious
	ClaimGapBetween
)

var claimNames = [...]string{"no-claim", "matches", "differs", "no-previous", "gap-between"}

func (inst ClaimE) String() string {
	if int(inst) < len(claimNames) {
		return claimNames[inst]
	}
	return "unknown"
}

// Gap is an audit-gap row: how many rows the recorder dropped and the
// window of their timestamps.
type Gap struct {
	Id   uint64    `desc:"the gap row's facts id"`
	Rows string    `desc:"how many rows were dropped, as the row says"`
	From time.Time `desc:"the first dropped row's time"`
	To   time.Time `desc:"the last dropped row's time"`
}

// gapOf reads an audit-gap row's attributes; ok is false when its window
// does not parse.
func gapOf(a *AuditDetail) (g Gap, ok bool) {
	g = Gap{Id: a.Id}
	g.Rows, _ = a.Attr("rows")
	f, _ := a.Attr(audit.AttrFrom)
	t, _ := a.Attr("to")
	var err1, err2 error
	g.From, err1 = time.Parse(time.RFC3339Nano, f)
	g.To, err2 = time.Parse(time.RFC3339Nano, t)
	ok = err1 == nil && err2 == nil
	return
}

// gapBetween is the first gap whose dropped rows fall in (lo, hi]: a row
// the recorder dropped there might have been the previous event. A gap
// whose window does not parse counts when the gap row itself is after lo.
func gapBetween(gaps []AuditDetail, lo, hi time.Time) (g Gap, ok bool) {
	for i := range gaps {
		gg, parsed := gapOf(&gaps[i])
		if !parsed {
			if gaps[i].Ts().After(lo) {
				return gg, true
			}
			continue
		}
		if gg.To.After(lo) && !gg.From.After(hi) {
			return gg, true
		}
	}
	return
}

// Previous is the previous-state read of one row and its verdict.
type Previous struct {
	// Row is the facts id the read is for.
	Row uint64
	// Has says a previous event was found within the look-back.
	Has bool
	// PrevId and PrevTs name it.
	PrevId uint64
	PrevTs time.Time
	Fields []FieldDiff
	Claim  ClaimE
	// From and FromBy are the row's claim, as written.
	From   string
	FromBy string
	Gap    Gap
	// Verdict is the claim's verdict in words.
	Verdict  string
	Lookback time.Duration
}

// judgeAudit is SD5 for an audit event: cur, the previous event of its
// (domain, entity) or nil, and the run's audit-gap rows that might fall
// between.
func judgeAudit(cur *AuditDetail, prev *AuditDetail, gaps []AuditDetail, look time.Duration) (p Previous) {
	p = Previous{Row: cur.Id, From: cur.From, FromBy: cur.FromBy, Lookback: look}
	lo := cur.Ts().Add(-look)
	if prev != nil {
		p.Has, p.PrevId, p.PrevTs = true, prev.Id, prev.Ts()
		p.Fields = diffFields(prev.AttrKeys, prev.AttrValues, cur.AttrKeys, cur.AttrValues)
		lo = prev.Ts()
	} else {
		p.Fields = diffFields(nil, nil, cur.AttrKeys, cur.AttrValues)
	}
	var claimFound bool
	_, claimFound = cur.Attr(audit.AttrFrom)
	switch g, gap := gapBetween(gaps, lo, cur.Ts()); {
	case gap:
		p.Claim, p.Gap = ClaimGapBetween, g
	case prev == nil:
		p.Claim = ClaimNoPrevious
	case !claimFound:
		p.Claim = ClaimNone
	case claimMatches(cur.From, prev, cur):
		p.Claim = ClaimMatches
	default:
		p.Claim = ClaimDiffers
	}
	p.Verdict = p.verdictText(prev)
	return
}

// judgeGrant is SD5 for an AgentGrant: the previous grant row of the same
// task, diffed field by field. A grant carries its state after by design
// and makes no `from` claim.
func judgeGrant(cur *GrantDetail, prev *GrantDetail, look time.Duration) (p Previous) {
	p = Previous{Row: cur.Id, Lookback: look}
	ck, cv := cur.fields()
	if prev == nil {
		p.Fields = diffFields(nil, nil, ck, cv)
		p.Claim = ClaimNoPrevious
	} else {
		pk, pv := prev.fields()
		p.Has, p.PrevId, p.PrevTs = true, prev.Id, time.Unix(0, prev.TsNs).UTC()
		p.Fields = diffFields(pk, pv, ck, cv)
		p.Claim = ClaimNone
	}
	p.Verdict = p.verdictText(nil)
	return
}

// claimMatches checks the claim against the previous event: it matches
// when it equals the previous value of a field this event changed, or —
// for a claim that renders several fields as one line, as the ration
// writer's does — when it contains every changed field's previous value.
// With no field changed it matches any previous value.
func claimMatches(claim string, prev *AuditDetail, cur *AuditDetail) bool {
	var changed []string
	for _, d := range diffFields(prev.AttrKeys, prev.AttrValues, cur.AttrKeys, cur.AttrValues) {
		if d.Change == ChangeChanged.String() || d.Change == ChangeRemoved.String() {
			changed = append(changed, d.Before)
		}
	}
	if len(changed) == 0 {
		for i, k := range prev.AttrKeys {
			if !slices.Contains(claimKeys, k) && i < len(prev.AttrValues) && prev.AttrValues[i] == claim {
				return true
			}
		}
		return false
	}
	if slices.Contains(changed, claim) {
		return true
	}
	for _, b := range changed {
		if b == "" || !strings.Contains(claim, b) {
			return false
		}
	}
	return true
}

// verdictText is the claim's verdict in the words of SD5.
func (inst Previous) verdictText(prev *AuditDetail) (s string) {
	switch inst.Claim {
	case ClaimMatches:
		s = "matches the previous event"
	case ClaimDiffers:
		s = "differs from the previous event: the row claims from " + strconv.Quote(inst.From)
		var was []string
		for _, d := range inst.Fields {
			if d.Change == ChangeChanged.String() || d.Change == ChangeRemoved.String() {
				was = append(was, d.Field+"="+d.Before)
			}
		}
		if len(was) > 3 {
			was = append(was[:3], "…")
		}
		if len(was) > 0 {
			s += ", the previous event had " + strings.Join(was, ", ")
		} else if prev != nil {
			s += ", which no field of the previous event holds"
		}
	case ClaimNoPrevious:
		s = "no previous event within the look-back (" + humanDuration(inst.Lookback) + ")"
		if inst.FromBy != "" {
			s += "; the row says its state before came from " + inst.FromBy
		}
	case ClaimGapBetween:
		s = "a gap row lies between: " + inst.Gap.Rows + " rows dropped"
		if !inst.Gap.From.IsZero() {
			s += " from " + inst.Gap.From.Format(time.RFC3339) + " to " + inst.Gap.To.Format(time.RFC3339)
		}
		s += ", so the previous event may be among them"
	default:
		s = "the row makes no from claim"
		if !inst.Has {
			s = "no previous event within the look-back (" + humanDuration(inst.Lookback) + ")"
		}
	}
	return
}

// humanDuration renders a look-back in days where it is whole days.
func humanDuration(d time.Duration) string {
	const day = 24 * time.Hour
	if d >= day && d%day == 0 {
		return strconv.FormatInt(int64(d/day), 10) + " days"
	}
	return d.String()
}
