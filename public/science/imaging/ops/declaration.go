package ops

import (
	"errors"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ErrRefused marks an operation that cannot run as requested: a parameter,
// an input or a combination of them the operator does not take. Every
// refusal wraps it, so a caller tells a request to correct from a failure
// to read or to store.
var ErrRefused = errors.New("operation refused")

// ParamTypeE is what a parameter's value is (ADR-0400 §SD1).
type ParamTypeE uint8

const (
	// ParamTypeNumber is a finite number.
	ParamTypeNumber ParamTypeE = iota + 1
	// ParamTypeInteger is a whole number.
	ParamTypeInteger
	// ParamTypeChoice is one of the names the parameter lists.
	ParamTypeChoice
	// ParamTypeText is free text.
	ParamTypeText
	// ParamTypeExpression is an expression in the package's grammar, held
	// in its canonical spelling once validated.
	ParamTypeExpression
)

// AllParamTypes lists the types a parameter can have.
var AllParamTypes = []ParamTypeE{ParamTypeNumber, ParamTypeInteger, ParamTypeChoice, ParamTypeText, ParamTypeExpression}

func (inst ParamTypeE) String() string {
	switch inst {
	case ParamTypeNumber:
		return "number"
	case ParamTypeInteger:
		return "integer"
	case ParamTypeChoice:
		return "choice"
	case ParamTypeText:
		return "text"
	case ParamTypeExpression:
		return "expression"
	}
	return "none"
}

// UnitPixels is the unit of a length in pixels of the input grid.
const UnitPixels = "px"

// maxTextBytes bounds a text or expression parameter.
const maxTextBytes = 4096

// Choice is one value a choice parameter takes, with a sentence saying what
// it means.
type Choice struct {
	Name string
	Doc  string
}

// Param declares one parameter of an operator (ADR-0400 §SD1): enough to
// build a dialog field, to check a value and to print help.
type Param struct {
	Name string
	Type ParamTypeE
	// Unit is the unit of a number or an integer: [UnitPixels], or empty
	// for a quantity without one.
	Unit string
	// HasRange bounds a number or an integer to [Min, Max], inclusive.
	HasRange bool
	Min, Max float64
	// Choices are the values of a choice parameter.
	Choices []Choice
	// Default is the value taken when none is given; the zero value for a
	// parameter that has none, which then is Required.
	Default Value
	// Doc is one sentence saying what the parameter does.
	Doc string
	// Required says that a request must give the parameter: it has no
	// default.
	Required bool
}

// Declaration is an operator as declared once (ADR-0400 §SD1): its
// identifier, version, summary, the inputs it takes and its parameters. A
// dialog, an app operation's arguments, the help text and the record are
// read from it.
type Declaration struct {
	// Id names the operator, lower case with hyphens: "gaussian".
	Id string
	// Version changes whenever the same request would give other samples.
	Version uint32
	// Summary is one line saying what the operator computes.
	Summary string
	// MinInputs and MaxInputs bound how many inputs it takes; MaxInputs 0
	// is no bound.
	MinInputs, MaxInputs int
	// InputsDoc is one sentence saying what the inputs are.
	InputsDoc string
	// LeavesInvalidOut says that an output sample is computed from the
	// valid samples it depends on, so that an invalid input sample does not
	// make it invalid (ADR-0400 §SD5).
	LeavesInvalidOut bool
	Params           []Param
}

var operatorIdPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// Param returns the parameter of that name, and ok false when the operator
// declares none.
func (inst Declaration) Param(name string) (p Param, ok bool) {
	i := slices.IndexFunc(inst.Params, func(p Param) bool { return p.Name == name })
	if i < 0 {
		return p, false
	}
	return inst.Params[i], true
}

// Ref is the operator's identifier and version, "gaussian@1".
func (inst Declaration) Ref() string {
	return inst.Id + "@" + strconv.FormatUint(uint64(inst.Version), 10)
}

func (inst Declaration) paramNames() []string {
	names := make([]string, len(inst.Params))
	for i, p := range inst.Params {
		names[i] = p.Name
	}
	return names
}

// Validate checks given parameter values against the declaration and
// returns their canonical form: every declared parameter, the given value
// or the default, a number with negative zero made zero and an expression
// in its canonical spelling. A name the operator does not declare, a value
// of the wrong type, outside the range or not among the choices, and a
// required parameter not given are refused, naming the parameter.
func (inst Declaration) Validate(given Params) (canonical Params, err error) {
	for name := range given {
		if _, ok := inst.Param(name); !ok {
			return nil, eb.Build().Str("operator", inst.Id).Str("param", name).Strs("params", inst.paramNames()).
				Errorf("the operator has no parameter of that name: %w", ErrRefused)
		}
	}
	canonical = make(Params, len(inst.Params))
	for _, p := range inst.Params {
		v, ok := given[p.Name]
		if !ok || v.IsZero() {
			if p.Default.IsZero() {
				b := eb.Build().Str("operator", inst.Id).Str("param", p.Name).Stringer("type", p.Type)
				if p.Type == ParamTypeChoice {
					b = b.Strs("choices", p.choiceNames())
				}
				return nil, b.Errorf("the parameter is required and has no default: %w", ErrRefused)
			}
			v = p.Default
		}
		if v, err = p.check(v); err != nil {
			return nil, eb.Build().Str("operator", inst.Id).Str("param", p.Name).Errorf("parameter value refused: %w", err)
		}
		canonical[p.Name] = v
	}
	return canonical, nil
}

// CheckParam checks one value of the parameter of that name against the
// declaration, as [Declaration.Validate] checks each, and returns it in
// canonical form: what a dialog field needs to say, as it is typed, whether
// its value will be taken. The zero value stands for the default; for a
// required parameter, which has none, it is refused. A name the operator
// does not declare and a value it does not take are refused, naming the
// parameter; every refusal wraps [ErrRefused].
func (inst Declaration) CheckParam(name string, v Value) (canonical Value, err error) {
	p, ok := inst.Param(name)
	if !ok {
		return v, eb.Build().Str("operator", inst.Id).Str("param", name).Strs("params", inst.paramNames()).
			Errorf("the operator has no parameter of that name: %w", ErrRefused)
	}
	if v.IsZero() {
		if p.Default.IsZero() {
			return v, eb.Build().Str("operator", inst.Id).Str("param", name).Errorf("the parameter is required and has no default: %w", ErrRefused)
		}
		v = p.Default
	}
	if canonical, err = p.check(v); err != nil {
		return v, eb.Build().Str("operator", inst.Id).Str("param", name).Errorf("parameter value refused: %w", err)
	}
	return canonical, nil
}

// ChoiceNames are the names a choice parameter takes, in order; nil for
// any other type.
func (inst Param) ChoiceNames() []string {
	if inst.Type != ParamTypeChoice {
		return nil
	}
	return inst.choiceNames()
}

func (inst Param) choiceNames() []string {
	names := make([]string, len(inst.Choices))
	for i, c := range inst.Choices {
		names[i] = c.Name
	}
	return names
}

// check refuses a value the parameter does not take and returns it in
// canonical form.
func (inst Param) check(v Value) (canonical Value, err error) {
	switch inst.Type {
	case ParamTypeNumber, ParamTypeInteger:
		x, ok := v.Number()
		switch {
		case !ok:
			return v, eb.Build().Stringer("type", inst.Type).Stringer("value", v).Errorf("the parameter takes a number: %w", ErrRefused)
		case !isFinite(x):
			return v, eb.Build().Float64("value", x).Errorf("the parameter takes a finite number: %w", ErrRefused)
		case inst.Type == ParamTypeInteger && !isExactInteger(x):
			return v, eb.Build().Float64("value", x).Errorf("the parameter takes a whole number: %w", ErrRefused)
		case inst.HasRange && (x < inst.Min || x > inst.Max):
			return v, eb.Build().Float64("value", x).Float64("min", inst.Min).Float64("max", inst.Max).
				Errorf("the value is outside the parameter's range: %w", ErrRefused)
		}
		if x == 0 {
			x = 0
		}
		return Number(x), nil
	case ParamTypeChoice, ParamTypeText, ParamTypeExpression:
		s, ok := v.Text()
		switch {
		case !ok:
			return v, eb.Build().Stringer("type", inst.Type).Stringer("value", v).Errorf("the parameter takes a text: %w", ErrRefused)
		case len(s) > maxTextBytes || !utf8.ValidString(s):
			return v, eb.Build().Int("bytes", len(s)).Int("maxBytes", maxTextBytes).
				Errorf("the parameter takes valid UTF-8 text of bounded length: %w", ErrRefused)
		}
		switch inst.Type {
		case ParamTypeChoice:
			if !slices.Contains(inst.choiceNames(), s) {
				return v, eb.Build().Str("value", s).Strs("choices", inst.choiceNames()).
					Errorf("the value is not one of the parameter's choices: %w", ErrRefused)
			}
		case ParamTypeExpression:
			n, perr := ParseExpression(s)
			if perr != nil {
				return v, eb.Build().Errorf("the expression does not parse: %w", errors.Join(perr, ErrRefused))
			}
			s = n.String()
		}
		return Text(s), nil
	}
	return v, eb.Build().Stringer("type", inst.Type).Errorf("the parameter's type is not one this package knows: %w", ErrRefused)
}

// checkDeclaration refuses a declaration a caller could not build a dialog
// from: an identifier not in the operator id form, no version, parameters
// without a name, type or doc, repeated names, a choice without choices, a
// default that fails its own parameter, or required and a default together.
func checkDeclaration(d Declaration) error {
	switch {
	case !operatorIdPattern.MatchString(d.Id):
		return eb.Build().Str("operator", d.Id).Errorf("an operator id is lower case words joined by hyphens")
	case d.Version == 0:
		return eb.Build().Str("operator", d.Id).Errorf("an operator has a version above zero")
	case d.Summary == "" || d.InputsDoc == "":
		return eb.Build().Str("operator", d.Id).Errorf("an operator has a summary and says what its inputs are")
	case d.MinInputs < 1 || (d.MaxInputs != 0 && d.MaxInputs < d.MinInputs):
		return eb.Build().Str("operator", d.Id).Int("min", d.MinInputs).Int("max", d.MaxInputs).
			Errorf("an operator takes at least one input, and no fewer than its minimum")
	}
	seen := make(map[string]bool, len(d.Params))
	for _, p := range d.Params {
		switch {
		case !inputNamePattern.MatchString(p.Name) || seen[p.Name]:
			return eb.Build().Str("operator", d.Id).Str("param", p.Name).Errorf("a parameter has an identifier for a name, used once")
		case !slices.Contains(AllParamTypes, p.Type) || p.Doc == "":
			return eb.Build().Str("operator", d.Id).Str("param", p.Name).Errorf("a parameter has a type and a doc sentence")
		case p.Type == ParamTypeChoice && len(p.Choices) == 0:
			return eb.Build().Str("operator", d.Id).Str("param", p.Name).Errorf("a choice parameter lists its choices")
		case p.Required == !p.Default.IsZero():
			return eb.Build().Str("operator", d.Id).Str("param", p.Name).Errorf("a parameter is either required or has a default")
		}
		seen[p.Name] = true
		if !p.Default.IsZero() {
			if _, err := p.check(p.Default); err != nil {
				return eb.Build().Str("operator", d.Id).Str("param", p.Name).Errorf("the parameter's default fails its own declaration: %w", err)
			}
		}
	}
	return nil
}

// Help is the declaration as text for a person: the operator, its inputs and
// each parameter with its type, unit, range, choices and default.
func (inst Declaration) Help() string {
	var sb strings.Builder
	sb.WriteString(inst.Ref())
	sb.WriteString(" — ")
	sb.WriteString(inst.Summary)
	sb.WriteString("\n  inputs: ")
	switch {
	case inst.MaxInputs == inst.MinInputs:
		sb.WriteString(strconv.Itoa(inst.MinInputs))
	case inst.MaxInputs == 0:
		sb.WriteString(strconv.Itoa(inst.MinInputs) + " or more")
	default:
		sb.WriteString(strconv.Itoa(inst.MinInputs) + " to " + strconv.Itoa(inst.MaxInputs))
	}
	sb.WriteString(". ")
	sb.WriteString(inst.InputsDoc)
	if inst.LeavesInvalidOut {
		sb.WriteString(" Invalid input samples are left out.")
	}
	sb.WriteString("\n")
	for _, p := range inst.Params {
		sb.WriteString("  ")
		sb.WriteString(p.Name)
		sb.WriteString(" (")
		sb.WriteString(p.Type.String())
		if p.Unit != "" {
			sb.WriteString(", " + p.Unit)
		}
		if p.HasRange {
			sb.WriteString(", " + formatNumber(p.Min) + " to " + formatNumber(p.Max))
		}
		if p.Required {
			sb.WriteString(", required")
		} else {
			sb.WriteString(", default " + p.Default.String())
		}
		sb.WriteString("): ")
		sb.WriteString(p.Doc)
		sb.WriteString("\n")
		for _, c := range p.Choices {
			sb.WriteString("    " + c.Name + " — " + c.Doc + "\n")
		}
	}
	return sb.String()
}

func formatNumber(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
