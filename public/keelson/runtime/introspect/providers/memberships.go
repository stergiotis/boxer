package providers

import (
	"sort"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/gov/capmapvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/keelson/vdd"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// memberships (ADR-0171 §SD4) publishes the membership vocabulary this
// process holds, so a Ref-membership table can be read from SQL by someone
// who does not hold the registry.
//
// The gap it closes was measured rather than assumed: the jsonbench trial
// found that a Ref-membership table is unreadable without the registry —
// memberships are identified by a uint64 from a vcs-registered vocabulary,
// there is no server-side lookup, and ids therefore ride SQL text as
// literals like 6917529027641081861. The trial shipped a CLI subcommand
// whose only purpose was to print them.
//
// # Why a table and not a UDF
//
// The obvious alternative was a generated LW_MEMB_* family, following the
// LW_ASPECT_* precedent that renders a closed Go vocabulary as transform()
// bodies. It was rejected on layering: this vocabulary is
// APPLICATION-specific — readback takes an IdLookup interface, keelson's
// facts target wraps this registry, anchor uses a small map, a downstream
// consumer brings its own — while chpack and the read-back family are
// generic leeway infrastructure. Installing one application's vocabulary
// into the shared surface would make its declared set application-dependent,
// which breaks the invariant ADR-0171 §SD2's marker rests on: the marker at
// revision N means all three families are installed at revision N.
//
// An introspection table has none of that problem. It is expanded
// client-side by the keelson('…') macro, so it works against an endpoint
// carrying nothing, and it sits in the layer that already describes this
// process — beside env, apps and sql_passes, which are application facts by
// the same logic.
//
// # Both directions
//
// A table answers name→id and id→name with the same join, which is what the
// two halves of the complaint need: writing a predicate without the literal,
// and rendering a result column that came back as a number.
//
//	SELECT name FROM keelson('memberships') WHERE id = 6917529027641081861
//	SELECT id   FROM keelson('memberships') WHERE name = 'cpuLoad'
//
// # The name is the folded spelling
//
// The registry folds every natural key to LowerSpinalCase on registration,
// so a Go declaration of `naturalKey` is stored — and published here — as
// `natural-key`. That is the only spelling the registry kept, so a join
// predicate must use it. The Go-side lookup below is forgiving where this
// table cannot be: it retries through the registry's naming style, so
// LW_GET accepts either spelling.
//
// # Deliberately not here, yet
//
// Restrictions — which section a membership may appear in, on which channel,
// at what cardinality — are declared per entry and would answer "which
// channel does this membership use", a question LW_GET's chan: token asks.
// They are one-to-many per membership, so they want their own table rather
// than parallel list columns, and no consumer asks for them yet.
type membershipsProvider struct{}

func (membershipsProvider) Name() string { return "memberships" }

// Freshness is Static: the registry is populated by package initialisers
// from vcs-managed declarations and cannot change while the process runs.
func (membershipsProvider) Freshness() introspect.FreshnessClass {
	return introspect.FreshnessStatic
}

func (membershipsProvider) Schema() *arrow.Schema { return membershipsTable(nil).Schema() }

func (membershipsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := membershipRows()
	return membershipsTable(rows).Build(proj, len(rows)), nil
}

// membershipRow is one registry entry flattened for the table.
type membershipRow struct {
	name    string
	id      uint64
	virtual bool
	root    bool
	tag     uint32
	origin  string
	module  string
	parents []string
}

// membershipRegistries are the vocabularies this table and [MembershipLookup]
// answer for (ADR-0191 §SD6). Each claims its own tag value, so the id spaces
// are disjoint by construction and the union is unambiguous; the names are
// checked for collision by the provider's own test, since a duplicate would
// make the lookup order load-bearing.
//
// It is a declared list rather than a registration seam because these four are
// the vocabularies this repository writes to `boxer.facts`, and the whole point
// of the table is that a reader holding a result column full of uint64s can ask
// what they are. A list that a package had to opt into would leave exactly the
// ids nobody thought about unnameable.
//
// Two in-tree registries are deliberately absent: `factsschema/meshdemo` is a
// demo fixture, and `apps/jsonbench` is an app — this is a library, and a
// library that imports an app inverts the dependency. An out-of-tree adopter's
// vocabulary is not here either; naming it would need a seam, which is the
// deferral ADR-0191 records.
//
// They all have one shape — a human-readable natural-key registry over the
// vcs-managed contract — written out rather than shortened behind a type
// alias, which CS008 refuses.
func membershipRegistries() (regs []*registry.HumanReadableNaturalKeyRegistry[*contract.VcsManagedContract]) {
	regs = []*registry.HumanReadableNaturalKeyRegistry[*contract.VcsManagedContract]{
		vdd.KeelsonHrNkRegistry,
		vocab.NkRegistry,
		sysmvocab.NkRegistry,
		capmapvocab.NkRegistry,
	}
	return
}

// membershipRows reads the process's natural-key registries, sorted by name so
// the table is stable across runs — a registry's own iteration order is an
// implementation detail, and an introspection table that reordered itself
// between queries would be unusable as a diff target. Sorting across the union
// also means the table does not expose which registry a name came from, which
// is right: the reader's question is what an id means, not who declared it.
func membershipRows() (rows []membershipRow) {
	regs := membershipRegistries()
	total := 0
	for _, reg := range regs {
		total += reg.Length()
	}
	rows = make([]membershipRow, 0, total)
	for _, reg := range regs {
		for name, e := range reg.IterateAll() {
			rows = append(rows, membershipRow{
				name:    string(name),
				id:      uint64(e.GetId()),
				virtual: e.GetFlags().HasVirtual(),
				root:    e.IsRoot(),
				tag:     uint32(e.GetTagValue()),
				origin:  e.GetOrigin(),
				module:  e.GetModuleInfo(),
				parents: membershipParents(e),
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	return
}

// membershipParents renders an entry's parents, virtual ones included: a
// virtual parent is how the vocabulary groups related memberships (the `lw`
// family hangs off a virtual `leeway`), so omitting them would hide the
// structure a reader browses by.
func membershipParents(e registry.RegisteredNaturalKey) (out []string) {
	out = make([]string, 0, 2)
	for _, p := range e.IterateAllParents() {
		out = append(out, string(p.GetNaturalKey()))
	}
	sort.Strings(out)
	return
}

func membershipsTable(rows []membershipRow) *introspect.Table {
	return introspect.NewTable().
		String("name", func(i int) string { return rows[i].name }).
		// The id a ref lane actually carries — the value matched against the
		// lr/hr columns, and the same uint64 a generated codec embeds. It is
		// a tagged identifier, so the LW_ID_* family decodes it.
		Uint64("id", func(i int) uint64 { return rows[i].id }).
		// A virtual entry groups other memberships and never appears on the
		// wire. Matching a lane against one returns nothing, which without
		// this column reads as "no such data" rather than "wrong question".
		Bool("virtual", func(i int) bool { return rows[i].virtual }).
		Bool("root", func(i int) bool { return rows[i].root }).
		Uint64("tag_value", func(i int) uint64 { return uint64(rows[i].tag) }).
		String("origin", func(i int) string { return rows[i].origin }).
		String("module", func(i int) string { return rows[i].module }).
		StringList("parents", func(i int) []string { return rows[i].parents })
}

// MembershipLookup adapts the process registry to the membership-id lookup
// the leeway marshalling and SQL-authoring paths take
// (marshallreflect.LookupI, readback.IdLookup, constructsql.MembershipIdsI —
// one method, spelled the same in all three).
//
// It lives here rather than in vdd because it exists for the SQL surface:
// it is what lets LW_GET name a membership on a ref channel instead of
// carrying its id (ADR-0171 §SD4, ADR-0181 §SD3).
type MembershipLookup struct{}

// LookupMembership resolves a membership name to the id a ref lane carries.
// Unknown names error rather than returning zero — zero is a valid id shape,
// and a silent miss would expand into a predicate that matches nothing.
//
// It searches every registry in [membershipRegistries], in order, and answers
// from the first that knows the name. Before ADR-0191 §SD6 it consulted vdd's
// alone, which meant a `boxer.facts` query naming a runtime membership —
// `runtimeKindAppLifecycle`, the vocabulary that table is mostly made of — was
// refused, and the author had to carry the raw uint64 in the SQL text. That is
// the exact complaint ADR-0171 §SD4 exists to answer, so answering it for one
// vocabulary and not the others was a half-open door rather than a policy.
//
// The names are collision-free across the set (the provider test asserts it),
// so first-match is total order rather than precedence.
func (MembershipLookup) LookupMembership(name string) (id uint64, err error) {
	key := naming.StylableName(name)
	for _, reg := range membershipRegistries() {
		e, lerr := reg.Lookup(key)
		if lerr != nil {
			continue
		}
		id = uint64(e.GetId())
		return
	}
	err = eh.Errorf("providers: no membership named %q in any registered vocabulary; keelson('memberships') lists them", name)
	return
}
