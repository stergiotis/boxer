package sqlvocab

import (
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// WhereE is the population a function belongs to — where it runs, which is
// what predicts how a call fails and what a user does about it (ADR-0174 §SD1).
//
// It is a bitset because one name can genuinely be more than one thing: the
// `LW_ID_*` family is installable as a server UDF *and* expanded client-side,
// and saying only one of the two would make the other answer wrong.
type WhereE uint8

const (
	// WhereServer is a SQL UDF: it exists only where it was installed, and
	// its absence is a provisioning fact.
	WhereServer WhereE = 1 << 0
	// WhereClient is a macro expanded before the statement ships, so it works
	// against any endpoint — including one carrying no UDFs.
	WhereClient WhereE = 1 << 1
	// WhereHost is computed in the hosting application over the rows a
	// sub-query returns; the server never sees the name.
	WhereHost WhereE = 1 << 2
)

// AllWheres is every population, in presentation order.
var AllWheres = []WhereE{WhereServer, WhereClient, WhereHost}

func (inst WhereE) String() (s string) {
	parts := make([]string, 0, 3)
	if inst&WhereServer != 0 {
		parts = append(parts, "server")
	}
	if inst&WhereClient != 0 {
		parts = append(parts, "client")
	}
	if inst&WhereHost != 0 {
		parts = append(parts, "host")
	}
	if len(parts) == 0 {
		return "unknown"
	}
	s = strings.Join(parts, "+")
	return
}

// Function is one callable name as its declaring roster publishes it.
type Function struct {
	Name   string
	Params []Param
	Doc    string
	// Where is the population, as a bitset — see [WhereE].
	Where WhereE
	// Family groups entries within a population and names their provenance.
	// It is a label, set at the wiring site rather than by the roster, because
	// what a family is called is a presentation decision.
	Family string
	// Available is false for a name the vocabulary reserves but does not
	// implement — it refuses rather than travelling.
	Available bool
	// Dependencies are server-side functions this entry's CLIENT-side
	// expansion emits (ADR-0174 §SD6). A client macro is portable only when
	// what it expands into is.
	Dependencies []string
}

// Call renders the entry as a call template, which is what an Insert action
// puts in the buffer.
func (inst Function) Call() (s string) {
	var b strings.Builder
	b.WriteString(inst.Name)
	b.WriteByte('(')
	for i := range inst.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(inst.Params[i].Name)
	}
	b.WriteByte(')')
	s = b.String()
	return
}

// Registry is the union of the rosters a build declares.
//
// Hosts populate it explicitly at their wiring site — beside where they
// register passes and components — rather than by package init, for ADR-0189
// §SD7's reason: a registry filled by init has a link-set-dependent extent, and
// an authoring surface that knows about fewer functions in one binary than in
// another is worse than one line of wiring.
//
// The zero value is not usable; call [NewRegistry].
type Registry struct {
	mu     sync.RWMutex
	byFold map[string][]Function
	order  []Function
}

// Default is the registry a host registers into unless it is building its own.
var Default = NewRegistry()

// NewRegistry returns an empty registry.
func NewRegistry() (inst *Registry) {
	inst = &Registry{byFold: make(map[string][]Function, 64)}
	return
}

// Register adds every function, in the order given.
//
// It is all-or-nothing: one bad entry adds none of them, so a failed Register
// leaves the registry as it was. That matters because the caller is a wiring
// site whose next line is another Register, and a half-applied roster would
// make the vocabulary depend on which entry failed.
//
// Refused: an unnamed function, a function belonging to no population, a
// parameter whose domain is [DomainUnspecified], a ref-dependent domain
// pointing at no sibling (or a non-ref domain pointing at one), and the same
// name registered twice for overlapping populations. Names compare
// case-insensitively, as ClickHouse resolves them.
func (inst *Registry) Register(fns ...Function) (err error) {
	staged := make([]Function, 0, len(fns))
	seen := make(map[string]WhereE, len(fns))
	for i := range fns {
		f := fns[i]
		if f.Name == "" {
			err = eb.Build().Int("index", i).Errorf("function has no name")
			return
		}
		if f.Where == 0 {
			err = eb.Build().Str("name", f.Name).
				Errorf("function belongs to no population; a name whose WHERE is unsaid cannot be reported as missing or portable")
			return
		}
		err = validateParams(f)
		if err != nil {
			return
		}
		fold := strings.ToLower(f.Name)

		inst.mu.RLock()
		prev := inst.byFold[fold]
		inst.mu.RUnlock()
		for _, p := range prev {
			if p.Where&f.Where != 0 {
				err = eb.Build().Str("name", f.Name).Str("where", f.Where.String()).
					Str("alreadyDeclaredBy", p.Family).
					Errorf("the same name is declared twice for a population it already occupies")
				return
			}
		}
		if occupied, dup := seen[fold]; dup && occupied&f.Where != 0 {
			err = eb.Build().Str("name", f.Name).Str("where", f.Where.String()).
				Errorf("the same name appears twice in one Register call for a population it already occupies")
			return
		}
		seen[fold] |= f.Where
		staged = append(staged, f)
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()
	// Re-check under the write lock: staging read with RLock, so a concurrent
	// Register could have claimed a name in between.
	for i := range staged {
		fold := strings.ToLower(staged[i].Name)
		for _, p := range inst.byFold[fold] {
			if p.Where&staged[i].Where != 0 {
				err = eb.Build().Str("name", staged[i].Name).
					Errorf("the same name is declared twice for a population it already occupies")
				return
			}
		}
	}
	for i := range staged {
		fold := strings.ToLower(staged[i].Name)
		inst.byFold[fold] = append(inst.byFold[fold], staged[i])
		inst.order = append(inst.order, staged[i])
	}
	return
}

// MustRegister is Register for a wiring site that cannot proceed without it.
func (inst *Registry) MustRegister(fns ...Function) {
	err := inst.Register(fns...)
	if err != nil {
		panic(err)
	}
}

func validateParams(f Function) (err error) {
	for j := range f.Params {
		d := f.Params[j].Domain
		if d.Kind == DomainUnspecified {
			err = eb.Build().Str("name", f.Name).Int("ordinal", j).Str("param", f.Params[j].Name).
				Errorf("parameter declares no domain; say DomainExpr if anything may stand there")
			return
		}
		if d.Kind.IsRefDependent() {
			if d.Ref < 0 || d.Ref >= len(f.Params) {
				err = eb.Build().Str("name", f.Name).Int("ordinal", j).
					Str("domain", d.Kind.String()).Int("ref", d.Ref).
					Errorf("domain depends on a sibling argument that the signature does not have")
				return
			}
			if d.Ref == j {
				err = eb.Build().Str("name", f.Name).Int("ordinal", j).
					Errorf("domain depends on itself")
				return
			}
			continue
		}
		if d.Ref != NoRef {
			err = eb.Build().Str("name", f.Name).Int("ordinal", j).
				Str("domain", d.Kind.String()).Int("ref", d.Ref).
				Errorf("domain names a sibling argument but does not read one; use NoRef")
			return
		}
	}
	return
}

// Lookup answers with every declaration of a name, comparing
// case-insensitively as ClickHouse resolves function names.
//
// It is a slice because one name can belong to more than one population — see
// [Function.Where]. The declarations of a name agree on their signature; they
// differ in where the name runs and what that means for a user.
func (inst *Registry) Lookup(name string) (fns []Function, ok bool) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	fns, ok = inst.byFold[strings.ToLower(name)]
	return
}

// Signature is the one declaration a completion engine needs: the parameters,
// from whichever declaration of the name carries them.
func (inst *Registry) Signature(name string) (f Function, ok bool) {
	fns, ok := inst.Lookup(name)
	if !ok || len(fns) == 0 {
		ok = false
		return
	}
	for i := range fns {
		if len(fns[i].Params) > 0 {
			f = fns[i]
			return
		}
	}
	f = fns[0]
	return
}

// All returns every registered function in registration order, which is the
// order the wiring site meant them to be presented in.
func (inst *Registry) All() (fns []Function) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	fns = make([]Function, len(inst.order))
	copy(fns, inst.order)
	return
}

// Len is All without the copy, for a caller asking only whether anything was
// wired.
func (inst *Registry) Len() (n int) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	n = len(inst.order)
	return
}

// Reset empties the registry. For tests and for a host rebuilding its wiring;
// a served registry is written once at startup.
func (inst *Registry) Reset() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.byFold = make(map[string][]Function, 64)
	inst.order = nil
}

// Register adds to [Default].
func Register(fns ...Function) error { return Default.Register(fns...) }

// Lookup resolves a name in [Default].
func Lookup(name string) ([]Function, bool) { return Default.Lookup(name) }

// All returns every function registered in [Default].
func All() []Function { return Default.All() }
