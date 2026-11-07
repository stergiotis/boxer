package fibscope

import (
	"fmt"

	"github.com/stergiotis/boxer/public/identity/fibonacci"
	"github.com/stergiotis/boxer/public/identity/identsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The operations catalog (ADR-0269): what an agent under a task grant reads
// and sets in a fibscope window. The id under inspection and the advisor's
// magnitude are the resources; the queries answer from a snapshot taken
// after the command stage and are bounded; the commands set values and never
// toggle them. Everything is a bit operation over one word, so nothing is
// asynchronous and nothing leaves the window. The Example and Invalid
// buttons go through set_id (ADR-0269 §SD8).

const (
	resId     = "id"
	resMaxExp = "max_exp"

	opGetSplit    = "get_split"
	opGetSql      = "get_sql"
	opGetTradeoff = "get_tradeoff"
	opSetId       = "set_id"
	opSetMaxExp   = "set_max_exp"

	// opsMaxClasses bounds get_tradeoff's rows; the scheme has fewer width
	// classes than this, so the bound only bites if the scheme grows.
	opsMaxClasses = 64
	// maxExpMaxIncl is the largest magnitude the advisor answers for.
	maxExpMaxIncl = uint64(1)<<60 - 1
)

// Split is get_split's result: the word and every part it decodes to.
type Split struct {
	Id         uint64 `desc:"the word under inspection"`
	Hex        string `desc:"the word as 0x-prefixed hexadecimal, 16 digits"`
	Bits       string `desc:"the word as 64 binary digits, most significant first"`
	Valid      bool   `desc:"the word carries the 11 comma and is a tagged id; the other fields are zero when not"`
	TagValue   uint64 `desc:"the category, decoded from the code"`
	TagWidth   int    `desc:"the code's width in bits, including the 11 comma"`
	Code       string `desc:"the code bits, most significant first, ending in the comma"`
	Zeckendorf string `desc:"the tag value as its sum of non-consecutive Fibonacci numbers"`
	Body       uint64 `desc:"the per-category counter in the low bits"`
	MaxBody    uint64 `desc:"the largest body this tag holds, 2^(64-width)-1"`
	RangeLo    uint64 `desc:"the smallest word with this tag"`
	RangeHi    uint64 `desc:"the largest word with this tag; ids of one tag are the contiguous range RangeLo to RangeHi"`
	Note       string `desc:"what an invalid word lacks; empty for a tagged id"`
}

// Sql is get_sql's result.
type Sql struct {
	TagValue  uint64 `desc:"the tag the filter tests"`
	Filter    string `desc:"the filter as written: LW_ID_HAS_TAG over the column id"`
	Expansion string `desc:"the statement the filter expands to, with the tag folded to a constant range"`
	Note      string `desc:"what the expansion does and what it is checked against"`
}

// GetTradeoffArgs bounds get_tradeoff.
type GetTradeoffArgs struct {
	Limit int `json:",omitzero" desc:"width classes to return, narrowest first; default and maximum 64"`
}

// WidthRow is one code width of the trade-off table.
type WidthRow struct {
	Width            int       `desc:"the code width in bits, including the comma"`
	TagValueMin      uint64    `desc:"the smallest tag value of this width"`
	TagValueMax      uint64    `desc:"the largest tag value of this width"`
	TagValues        uint64    `desc:"how many tag values have this width"`
	MaxBody          uint64    `desc:"the largest body under a tag of this width"`
	EncodingOverhead float64   `desc:"code width over the bits a plain binary encoding of the class's largest tag value needs: the price of the comma"`
	ExhaustSeconds   []float64 `desc:"seconds to fill one tag's body space at each rate of Rates, in order"`
	Recommended      bool      `desc:"the advisor picks this width for MaxExp"`
}

// Tradeoff is get_tradeoff's result.
type Tradeoff struct {
	MaxExp           uint64     `desc:"the ids expected in the busiest category, as the slider holds it"`
	RecommendedWidth int        `desc:"the widest code that still leaves room for MaxExp bodies; 0 when the advisor refuses"`
	TagValueLo       uint64     `desc:"the first tag value of the recommended width"`
	TagValueHi       uint64     `desc:"the last tag value of the recommended width"`
	AdvisorError     string     `desc:"why the advisor has no pick; empty otherwise"`
	Rates            []string   `desc:"the ingress rates ExhaustSeconds is computed at"`
	Classes          []WidthRow `desc:"the width classes, narrowest first"`
	Truncated        bool       `desc:"the scheme has more width classes than Classes holds"`
}

// SetIdArgs is set_id's argument.
type SetIdArgs struct {
	Id uint64 `desc:"the word to inspect; any 64-bit value, a word that is not a tagged id included"`
}

// SetMaxExpArgs is set_max_exp's argument.
type SetMaxExpArgs struct {
	MaxExp uint64 `desc:"ids expected in the busiest category, 1 to 2^60-1"`
}

// sqlNote states what get_sql's expansion is.
const sqlNote = "The constant-tag filter folds to a sargable range at expansion time (identsql.ExpandPass), " +
	"the same split the Go code does; the expansion is golden-tested against ClickHouse in the identity packages, not here."

// opsSnap is what queries read: a copy taken after the command stage.
type opsSnap struct {
	id     uint64
	maxExp float64
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	return opsSnap{id: inst.id, maxExp: inst.maxExp}
}

// splitOf renders a decoded word as get_split's answer.
func splitOf(d decoded) (out Split) {
	out = Split{Id: d.id, Hex: fmt.Sprintf("0x%016x", d.id), Bits: d.bits, Valid: d.valid}
	if !d.valid {
		out.Note = "the word has no adjacent 11 pair scanning from the most significant bit, so it carries no comma and is not a tagged id"
		return
	}
	out.TagValue, out.TagWidth, out.Code = uint64(d.tagValue), d.tagWidth, d.bits[:d.tagWidth]
	out.Zeckendorf = zeckExplain(d.tagValue)
	out.Body, out.MaxBody = uint64(d.body), d.maxBody
	out.RangeLo = uint64(d.tag)
	out.RangeHi = out.RangeLo | d.maxBody
	return
}

// tradeoffOf renders the advisor and the width table for maxExp.
func tradeoffOf(maxExp float64, limit int) (out Tradeoff) {
	out.MaxExp = clampMaxExp(maxExp)
	lo, hiExcl, err := fibonacci.SelectFittingTagValueRange(out.MaxExp)
	if err != nil {
		out.AdvisorError = err.Error()
	} else {
		out.RecommendedWidth = lo.GetTag().GetTagWidth()
		out.TagValueLo, out.TagValueHi = uint64(lo), uint64(hiExcl)-1
	}
	for _, r := range ingressRates {
		out.Rates = append(out.Rates, r.label)
	}
	classes := fibonacci.WidthClasses()
	n := min(len(classes), limit)
	out.Classes = make([]WidthRow, 0, n)
	for _, cl := range classes[:n] {
		row := WidthRow{Width: cl.Width, TagValueMin: uint64(cl.TagValueMinIncl), TagValueMax: uint64(cl.TagValueMaxIncl),
			TagValues: cl.TagValueCount, MaxBody: cl.MaxBodyIncl, EncodingOverhead: cl.EncodingOverhead,
			Recommended: cl.Width == out.RecommendedWidth}
		for _, r := range ingressRates {
			row.ExhaustSeconds = append(row.ExhaustSeconds, float64(cl.MaxBodyIncl)/r.hz)
		}
		out.Classes = append(out.Classes, row)
	}
	out.Truncated = len(classes) > n
	return
}

// setMaxExp sets the advisor's magnitude, refusing one outside its domain.
func (inst *App) setMaxExp(v uint64) (err error) {
	if v < 1 || v > maxExpMaxIncl {
		return app.RefuseOperation(fmt.Sprintf("max_exp is between 1 and 2^60-1 (%d)", maxExpMaxIncl))
	}
	inst.maxExp = float64(v)
	return
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resId, "the 64-bit word under inspection", func(inst *App) any { return inst.id })
	s.Restorable(resId, func(inst *App, v any) bool {
		id, ok := v.(uint64)
		if ok {
			inst.id = id
		}
		return ok
	})
	s.Editing(resId, func(inst *App) bool {
		for _, h := range inst.idHandles {
			if appops.WidgetEditing(h) {
				return true
			}
		}
		return false
	})
	s.Resource(resMaxExp, "the ids expected in the busiest category, the advisor's input", func(inst *App) any { return inst.maxExp })
	s.Restorable(resMaxExp, func(inst *App, v any) bool {
		f, ok := v.(float64)
		if ok {
			inst.maxExp = f
		}
		return ok
	})
	s.Editing(resMaxExp, func(inst *App) bool { return appops.WidgetEditing(inst.maxExpHandle) })

	appops.Query(s, app.OperationSpec{Name: opGetSplit, Version: 1,
		Summary: "read how the word splits: validity, tag value and width, code, body and its ceiling, and the same-tag range",
		Reads:   []string{resId}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out Split, err error) {
			out = splitOf(decode(sn.id))
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetSql, Version: 1,
		Summary: "read the ClickHouse filter for the word's tag and the range it expands to",
		Reads:   []string{resId}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out Sql, err error) {
			d := decode(sn.id)
			if !d.valid {
				err = app.RefuseOperation("the word is not a tagged id; SQL decode applies to tagged ids only — set_id one, or get_split says what the word lacks")
				return
			}
			tv := uint64(d.tagValue)
			out = Sql{TagValue: tv, Filter: fmt.Sprintf("LW_ID_HAS_TAG(id, %d)", tv), Note: sqlNote}
			out.Expansion, err = identsql.ExpandPass.Run(fmt.Sprintf("SELECT LW_ID_HAS_TAG(id, %d) FROM t", tv))
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetTradeoff, Version: 1,
		Summary: "read the advisor's pick for max_exp and the width classes with their capacity and time to exhaust, at most 64 rows",
		Reads:   []string{resMaxExp}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetTradeoffArgs) (out Tradeoff, err error) {
			limit := in.Limit
			switch {
			case limit < 0:
				err = app.RefuseOperation("limit is not negative")
				return
			case limit == 0 || limit > opsMaxClasses:
				limit = opsMaxClasses
			}
			out = tradeoffOf(sn.maxExp, limit)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetId, Version: 1,
		Summary: "set the word under inspection; any 64-bit value, a word that is not a tagged id included",
		Effect:  app.OperationEffectDocument, Writes: []string{resId}, Agents: true,
		Gesture: "the raw id field, or the Example and Invalid buttons", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance},
		func(inst *App, _ app.OperationCall, in SetIdArgs) (out appops.None, err error) {
			inst.id = in.Id
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetMaxExp, Version: 1,
		Summary: "set the ids expected in the busiest category, 1 to 2^60-1, which the advisor sizes the tag for",
		Effect:  app.OperationEffectDocument, Writes: []string{resMaxExp}, Agents: true,
		Gesture: "the max ids / category slider", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance},
		func(inst *App, _ app.OperationCall, in SetMaxExpArgs) (out appops.None, err error) {
			err = inst.setMaxExp(in.MaxExp)
			return
		})
	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)

// gestureOr routes the person's control through the catalog (ADR-0269
// §SD8), so the command log sees it as theirs, and applies it directly where
// no host serves the catalog (the tour, tests).
func gestureOr[In any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}
