package main

import (
	"math"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
)

// shredded is one (path, params, value) triple, the unit the canonical leeway
// JSON mapping produces. `path` has array positions replaced by "_"; `params`
// holds the elided indices in path order.
type shredded struct {
	path   string
	params []int
	kind   valueKindE
	s      string
	i      int64
	f      float64
	b      bool
}

type valueKindE uint8

const (
	valueKindNull valueKindE = iota
	valueKindBool
	valueKindInt
	valueKindFloat
	valueKindString
)

// shredder walks a decoded JSON document and emits triples. It reuses its
// buffers across documents; the caller must consume `out` before the next
// call.
type shredder struct {
	out  []shredded
	path strings.Builder
	// params for the path currently being walked.
	params []int
	// nulls counts values dropped because boxer.facts has no section that
	// can hold them (see the finding in the trial logbook).
	nulls uint64
	// keys is a scratch slice for deterministic object iteration.
	keyBuf []string
}

func (inst *shredder) reset() {
	inst.out = inst.out[:0]
	inst.params = inst.params[:0]
}

func (inst *shredder) shred(doc map[string]any) []shredded {
	inst.reset()
	inst.walkObject("", doc)
	return inst.out
}

func (inst *shredder) walkObject(prefix string, obj map[string]any) {
	// Deterministic key order: two runs of the same corpus must produce
	// byte-identical tables, or size comparisons between arms are noise.
	inst.keyBuf = inst.keyBuf[:0]
	for k := range obj {
		inst.keyBuf = append(inst.keyBuf, k)
	}
	sort.Strings(inst.keyBuf)
	keys := make([]string, len(inst.keyBuf))
	copy(keys, inst.keyBuf)
	for _, k := range keys {
		inst.walk(prefix+"/"+k, obj[k])
	}
}

func (inst *shredder) walk(path string, v any) {
	switch t := v.(type) {
	case nil:
		// boxer.facts has no null / undefined section (the canonical JSON
		// mapping does). The value is dropped and counted.
		inst.nulls++
	case bool:
		inst.emit(path, shredded{kind: valueKindBool, b: t})
	case string:
		inst.emit(path, shredded{kind: valueKindString, s: t})
	case float64:
		// json.Unmarshal gives every number as float64. Integral values go to
		// the integer section — time_us is a 53-bit-safe microsecond epoch and
		// must not round-trip through a float column.
		if t == math.Trunc(t) && math.Abs(t) < (1<<53) {
			inst.emit(path, shredded{kind: valueKindInt, i: int64(t)})
			return
		}
		inst.emit(path, shredded{kind: valueKindFloat, f: t})
	case map[string]any:
		inst.walkObject(path, t)
	case []any:
		for i, e := range t {
			inst.params = append(inst.params, i)
			inst.walk(path+"/_", e)
			inst.params = inst.params[:len(inst.params)-1]
		}
	default:
		// json.Unmarshal into `any` produces nothing else.
		inst.nulls++
	}
}

func (inst *shredder) emit(path string, s shredded) {
	s.path = path
	if len(inst.params) > 0 {
		s.params = make([]int, len(inst.params))
		copy(s.params, inst.params)
	}
	inst.out = append(inst.out, s)
}

// formatParams renders the elided array indices as the high-cardinality
// membership parameter, in path order and in the canonical form
// ([membership.AppendParams]) — one codec across every writer of the channel,
// rather than the comma-joined decimal this trial used to invent for itself.
//
// Note for a re-run: this changes the bytes in the params lane against the runs
// recorded in doc/trials/jsonbench-on-facts/runs/, which were loaded with the
// decimal form. Only attributes under an array carry params at all, so the
// difference is confined to that lane, but sizes are not byte-comparable across
// the change.
func formatParams(params []int) (raw []byte, err error) {
	if len(params) == 0 {
		return
	}
	idx := make([]uint64, len(params))
	for i, p := range params {
		if p < 0 {
			err = eb.Build().Int("index", p).Int("position", i).Errorf("negative array index")
			return
		}
		idx[i] = uint64(p)
	}
	raw, err = membership.EncodeParams(idx...)
	return
}
