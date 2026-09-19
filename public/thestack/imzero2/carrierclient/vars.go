package carrierclient

import (
	"math"
	"regexp"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// vars.go is the state `read` binds and `expect` checks (ADR-0248 §SD1–SD2).
//
// It is deliberately not a language. A name is bound to what a regular
// expression captured; the only operations are a comparison with a constant,
// a difference of two names, and adding a name to a pointer coordinate. A
// scene that needs more than that does its arithmetic in Go, over the same
// executor, with this type as the way its readings come back out.

// Vars holds the names a run has bound.
type Vars struct {
	m map[string]binding
}

type binding struct {
	text string
	num  float64
	// isNum records that text parsed as a number; a capture like "false" is
	// still bound, and only a numeric comparison of it is an error.
	isNum bool
}

// NewVars returns an empty set of bindings.
func NewVars() *Vars { return &Vars{m: make(map[string]binding)} }

// Bind sets a name to a captured text, parsing it as a number when it is one.
func (inst *Vars) Bind(name string, text string) {
	b := binding{text: text}
	if f, err := strconv.ParseFloat(text, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
		b.num, b.isNum = f, true
	}
	inst.m[name] = b
}

// Text returns a bound name as the text that was captured.
func (inst *Vars) Text(name string) (text string, err error) {
	b, ok := inst.m[name]
	if !ok {
		return "", eb.Build().Str("name", name).Errorf("name is not bound — no earlier read captured it")
	}
	return b.text, nil
}

// Number returns a bound name as a number.
func (inst *Vars) Number(name string) (num float64, err error) {
	b, ok := inst.m[name]
	if !ok {
		return 0, eb.Build().Str("name", name).Errorf("name is not bound — no earlier read captured it")
	}
	if !b.isNum {
		return 0, eb.Build().Str("name", name).Str("text", b.text).Errorf("bound name is not a number")
	}
	return b.num, nil
}

// bindMatch binds every named group of pattern against text, and reports
// whether the pattern matched at all.
func (inst *Vars) bindMatch(re *regexp.Regexp, text string) (matched bool) {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return false
	}
	for i, name := range re.SubexpNames() {
		if i > 0 && name != "" {
			inst.Bind(name, m[i])
		}
	}
	return true
}

// compilePattern compiles a `read` pattern and insists it binds something: a
// read that captures no name is a `wait` spelled wrongly, and a silent one.
func compilePattern(pattern string) (re *regexp.Regexp, err error) {
	if pattern == "" {
		return nil, eh.Errorf("read needs a \"pattern\"")
	}
	if re, err = regexp.Compile(pattern); err != nil {
		return nil, eb.Build().Str("pattern", pattern).Errorf("unable to compile the read pattern: %w", err)
	}
	for _, name := range re.SubexpNames() {
		if name != "" {
			return re, nil
		}
	}
	return nil, eb.Build().Str("pattern", pattern).
		Errorf("read pattern has no named group — write (?P<name>…) for what it should bind")
}

// expect runs one `expect` step against the bindings.
func (inst *Vars) expect(st Step) (err error) {
	if st.Of == "" {
		return eh.Errorf("expect needs a bound name in \"of\"")
	}
	if st.Is != "" || st.Matches != "" {
		if st.Minus != "" {
			return eh.Errorf("expect: \"minus\" is a numeric difference and cannot be combined with \"is\" or \"matches\"")
		}
		var text string
		if text, err = inst.Text(st.Of); err != nil {
			return err
		}
		if st.Is != "" && text != st.Is {
			return expectFailed(st.Of, strconv.Quote(text), strconv.Quote(st.Is))
		}
		if st.Matches != "" {
			var re *regexp.Regexp
			if re, err = regexp.Compile(st.Matches); err != nil {
				return eb.Build().Str("matches", st.Matches).Errorf("unable to compile the expect pattern: %w", err)
			}
			if !re.MatchString(text) {
				return expectFailed(st.Of, strconv.Quote(text), "/"+st.Matches+"/")
			}
		}
		return nil
	}
	if st.Eq == nil && st.Approx == nil && st.Min == nil && st.Max == nil {
		return eh.Errorf("expect needs a comparison: eq, approx (with tol), min, max, is or matches")
	}
	var v float64
	if v, err = inst.Number(st.Of); err != nil {
		return err
	}
	what := st.Of
	if st.Minus != "" {
		var sub float64
		if sub, err = inst.Number(st.Minus); err != nil {
			return err
		}
		v -= sub
		what = st.Of + " - " + st.Minus
	}
	fail := func(expected string) error {
		return expectFailed(what, fmtFloat(v), expected)
	}
	if st.Eq != nil && v != *st.Eq {
		return fail("= " + fmtFloat(*st.Eq))
	}
	if st.Approx != nil && math.Abs(v-*st.Approx) > st.Tol {
		return fail(fmtFloat(*st.Approx) + " +/- " + fmtFloat(st.Tol))
	}
	if st.Min != nil && v < *st.Min {
		return fail(">= " + fmtFloat(*st.Min))
	}
	if st.Max != nil && v > *st.Max {
		return fail("<= " + fmtFloat(*st.Max))
	}
	return nil
}

// expectFailed carries what was read and what was expected as fields; a
// caller that shows the failure to a person renders them with
// eh.FormatErrorPlain, which is what the scene runner does.
func expectFailed(of, read, expected string) error {
	return eb.Build().Str("of", of).Str("read", read).Str("expected", expected).Errorf("expect failed")
}

// fmtFloat prints twelve significant digits: enough for any readout, and few
// enough that a difference of two decimals does not print its binary tail.
func fmtFloat(f float64) string { return strconv.FormatFloat(f, 'g', 12, 64) }

// offset adds the numbers bound to xFrom / yFrom to a point. Empty names add
// nothing, so a step without them is unchanged.
func (inst *Vars) offset(st Step, x, y float32) (ox, oy float32, err error) {
	ox, oy = x, y
	if st.XFrom != "" {
		var d float64
		if d, err = inst.Number(st.XFrom); err != nil {
			return 0, 0, err
		}
		ox += float32(d)
	}
	if st.YFrom != "" {
		var d float64
		if d, err = inst.Number(st.YFrom); err != nil {
			return 0, 0, err
		}
		oy += float32(d)
	}
	return ox, oy, nil
}
