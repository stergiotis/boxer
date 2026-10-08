package introspect

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ArgTypeE is the type of a provider's named argument (ADR-0290 §SD1). The
// set is the scalars a statement can spell as a literal or bind as a query
// parameter; a value's text is parsed as ClickHouse would read it.
type ArgTypeE uint8

const (
	ArgTypeInt64 ArgTypeE = iota
	ArgTypeUInt64
	ArgTypeFloat64
	ArgTypeString
	ArgTypeBool
)

func (t ArgTypeE) String() (s string) {
	switch t {
	case ArgTypeInt64:
		s = "Int64"
	case ArgTypeUInt64:
		s = "UInt64"
	case ArgTypeFloat64:
		s = "Float64"
	case ArgTypeString:
		s = "String"
	case ArgTypeBool:
		s = "Bool"
	default:
		s = "unknown"
	}
	return
}

// ArgSpec declares one named argument a provider takes.
type ArgSpec struct {
	// Name is the argument's name in `keelson('t', name = …)`; it must be a
	// valid table-name-shaped identifier.
	Name string
	Type ArgTypeE
	// Required arguments must be supplied; an optional one takes Default,
	// which is the value's text as a statement would spell it.
	Required bool
	Default  string
}

// ArgsProviderI is a Provider whose rows are defined by named arguments
// (ADR-0290 §SD1). Snapshot, without arguments, is what a call that
// supplies none reads: it sees every argument at its default, and errors
// when one is required.
type ArgsProviderI interface {
	Provider
	// Args declares the arguments, in the order a listing shows them.
	Args() []ArgSpec
	// SnapshotArgs materialises the rows for args, which ResolveArgs has
	// already checked against Args. Ownership and proj are as for Snapshot.
	SnapshotArgs(proj Projection, args Args) (arrow.RecordBatch, error)
}

// Args are a call's resolved, typed argument values.
type Args struct {
	vals map[string]argVal
}

type argVal struct {
	t ArgTypeE
	i int64
	u uint64
	f float64
	s string
	b bool
}

// Int64 returns an ArgTypeInt64 argument; zero when absent or of another type.
func (inst Args) Int64(name string) (v int64) { return inst.vals[name].i }

// UInt64 returns an ArgTypeUInt64 argument; zero when absent or of another type.
func (inst Args) UInt64(name string) (v uint64) { return inst.vals[name].u }

// Float64 returns an ArgTypeFloat64 argument; zero when absent or of another type.
func (inst Args) Float64(name string) (v float64) { return inst.vals[name].f }

// String returns an ArgTypeString argument; empty when absent or of another type.
func (inst Args) String(name string) (v string) { return inst.vals[name].s }

// Bool returns an ArgTypeBool argument; false when absent or of another type.
func (inst Args) Bool(name string) (v bool) { return inst.vals[name].b }

// Key is a canonical spelling of the values, ordered by name, so two calls
// that resolve to the same values have the same key.
func (inst Args) Key() (key string) {
	names := make([]string, 0, len(inst.vals))
	for n := range inst.vals {
		names = append(names, n)
	}
	slices.Sort(names)
	var sb strings.Builder
	for i, n := range names {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(n)
		sb.WriteByte('=')
		v := inst.vals[n]
		switch v.t {
		case ArgTypeInt64:
			sb.WriteString(strconv.FormatInt(v.i, 10))
		case ArgTypeUInt64:
			sb.WriteString(strconv.FormatUint(v.u, 10))
		case ArgTypeFloat64:
			sb.WriteString(strconv.FormatFloat(v.f, 'g', -1, 64))
		case ArgTypeString:
			sb.WriteString(strconv.Quote(v.s))
		case ArgTypeBool:
			sb.WriteString(strconv.FormatBool(v.b))
		}
	}
	return sb.String()
}

// ResolveArgs checks raw — argument name to value text — against specs and
// types each value: an undeclared or missing required argument, or a value
// its type cannot read, fails naming it. Optional arguments absent from raw
// take their Default.
func ResolveArgs(specs []ArgSpec, raw map[string]string) (args Args, err error) {
	args.vals = make(map[string]argVal, len(specs))
	for name := range raw {
		if !slices.ContainsFunc(specs, func(s ArgSpec) bool { return s.Name == name }) {
			err = eb.Build().Str("arg", name).Errorf("introspect: the table takes no argument by this name")
			return
		}
	}
	for _, s := range specs {
		text, ok := raw[s.Name]
		if !ok {
			if s.Required {
				err = eb.Build().Str("arg", s.Name).Errorf("introspect: a required argument is missing")
				return
			}
			text = s.Default
		}
		var v argVal
		v, err = parseArg(s.Type, text)
		if err != nil {
			err = eb.Build().Str("arg", s.Name).Str("type", s.Type.String()).Str("value", text).Errorf("introspect: argument value does not read as its type: %w", err)
			return
		}
		args.vals[s.Name] = v
	}
	return
}

func parseArg(t ArgTypeE, text string) (v argVal, err error) {
	v.t = t
	switch t {
	case ArgTypeInt64:
		v.i, err = strconv.ParseInt(text, 10, 64)
	case ArgTypeUInt64:
		v.u, err = strconv.ParseUint(text, 10, 64)
	case ArgTypeFloat64:
		v.f, err = strconv.ParseFloat(text, 64)
		if err == nil && math.IsNaN(v.f) {
			err = eb.Build().Errorf("NaN is not an argument value")
		}
	case ArgTypeString:
		v.s = text
	case ArgTypeBool:
		switch strings.ToLower(text) {
		case "1", "true":
			v.b = true
		case "0", "false":
			v.b = false
		default:
			err = eb.Build().Errorf("not a Bool")
		}
	default:
		err = eb.Build().Errorf("unknown argument type")
	}
	return
}

// SnapshotCall materialises what a call of p with raw arguments reads: an
// ArgsProviderI's rows for the resolved arguments, or, for a provider that
// takes none, its plain snapshot — which a call with arguments is refused.
func SnapshotCall(p Provider, proj Projection, raw map[string]string) (batch arrow.RecordBatch, err error) {
	ap, takes := p.(ArgsProviderI)
	if !takes {
		if len(raw) > 0 {
			err = eb.Build().Str("table", p.Name()).Errorf("introspect: the table takes no arguments")
			return
		}
		return p.Snapshot(proj)
	}
	var args Args
	args, err = ResolveArgs(ap.Args(), raw)
	if err != nil {
		err = eb.Build().Str("table", p.Name()).Errorf("%w", err)
		return
	}
	return ap.SnapshotArgs(proj, args)
}

// SnapshotCallFile is SnapshotCall encoded as an Arrow IPC file, as
// SnapshotFile is for Snapshot.
func SnapshotCallFile(p Provider, proj Projection, raw map[string]string) (b []byte, err error) {
	batch, err := SnapshotCall(p, proj, raw)
	if err != nil {
		return nil, err
	}
	defer batch.Release()
	return EncodeFile(batch)
}
