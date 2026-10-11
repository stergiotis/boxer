package agentschema

import (
	"slices"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The declaration language (ADR-0313 §SD6), one relation at a time:
//
//	relation  <name>  OVER <table|view>  SCOPE <setting> = <column>
//	  section   <s>   { PASS | DROP }
//	  withhold  <s>   MEMBERSHIP IN (<registry keys…>)
//	  aspect    secret → WITHHOLD ; application-level-encryption → OPAQUE ; pseudonymized → PASS
//
// A Go literal is the declaration; there is nothing to parse. No rule
// takes a value predicate.

// Relation is one declared relation.
type Relation struct {
	// Name is the relation as the agent names it; Over the table or view
	// it reads, qualified as the server knows it.
	Name string
	Over string
	// Scope is the setting the relation is pinned by and the column it
	// binds (§SD2).
	Scope Scope
	// Sections are the columns passed or dropped; a column not named is
	// dropped, so the declaration lists what the agent may read. A
	// section may carry an expression over the base column instead of
	// the column itself, for a content-free reading (a length, a digest);
	// an expression reads base columns through the alias `base`.
	Sections []SectionRule
	// Memberships names the array column whose elements are the
	// memberships withhold rules read; empty when the relation withholds
	// nothing.
	Memberships string
	// Withhold names, per section, the memberships whose instances are
	// withheld (§SD5): the section's value is blanked on those rows and
	// the memberships move to the withheld section with this relation's
	// policy name.
	Withhold []WithholdRule
	// Aspects are the value-aspect defaults (§SD4); a field left
	// unspecified takes the package's default. They are declared and
	// resolved, not compiled: the aspect of a column is read from its
	// physical name on leeway's layouts, which the plain shape has no
	// naming for, so keelson('agent_relations') does not list them.
	// deferred: compile the aspect rules with the layouts (§SD4), under
	// the dated Update on ADR-0171 the package doc names.
	Aspects AspectDefaults
	// Summary is what the introspection table says of the relation.
	Summary string
}

// Scope pins a relation to one value of a setting (§SD2): rows whose
// Column equals the setting's value, or, through Via, rows whose Column is
// among the keys of another table filtered by the setting.
type Scope struct {
	// Setting is the custom setting's name, constrained READONLY in the
	// agent principal's profile.
	Setting string
	// Column is the column the scope binds on the relation's table.
	Column string
	// Via, when set, reaches the scope through another table: the rows
	// whose Column is among Via.Table's Via.Key where Via.Column equals the
	// setting's value.
	Via *ScopeVia
	// Except, when set, names a second setting whose value Column must
	// not equal: my_conversation is the conversation's runs other than
	// the pinned one (§SD1, "earlier runs").
	Except string
}

// settings are the settings the scope reads: its own and, when set, the
// one it excludes by.
func (inst Relation) settings() (names []string) {
	names = []string{inst.Scope.Setting}
	if inst.Scope.Except != "" {
		names = append(names, inst.Scope.Except)
	}
	return
}

// ScopeVia is a scope reached through a second table.
type ScopeVia struct {
	Table  string
	Key    string
	Column string
}

// SectionRule passes or drops one column; Expr, when set, is the SQL
// expression passed in the column's place, over the base column.
type SectionRule struct {
	Name string
	Pass bool
	Expr string
}

// WithholdRule withholds the instances of a section that carry one of the
// memberships.
type WithholdRule struct {
	Section     string   `json:"section"`
	Memberships []string `json:"memberships"`
}

// AspectE is a value aspect read from a physical name (§SD4).
type AspectE uint8

const (
	AspectSecret AspectE = iota
	AspectApplicationLevelEncryption
	AspectPseudonymized
)

// AspectActionE is what an aspect does to a value; unspecified takes the
// package default for that aspect.
type AspectActionE uint8

const (
	AspectActionUnspecified AspectActionE = iota
	AspectActionWithhold
	AspectActionOpaque
	AspectActionPass
)

func (inst AspectActionE) String() string {
	switch inst {
	case AspectActionWithhold:
		return "WITHHOLD"
	case AspectActionOpaque:
		return "OPAQUE"
	case AspectActionPass:
		return "PASS"
	}
	return "UNSPECIFIED"
}

// AspectDefaults map each aspect to its action; a field left unspecified,
// in a literal from any package, takes §SD4's default for that aspect:
// secret withheld, encrypted opaque, pseudonymized passed.
type AspectDefaults struct {
	Secret        AspectActionE
	Encrypted     AspectActionE
	Pseudonymized AspectActionE
}

// DefaultAspects are §SD4's defaults.
func DefaultAspects() AspectDefaults {
	return AspectDefaults{Secret: AspectActionWithhold, Encrypted: AspectActionOpaque, Pseudonymized: AspectActionPass}
}

// Resolved is the defaults with every unspecified field filled in.
func (inst AspectDefaults) Resolved() (out AspectDefaults) {
	out, d := inst, DefaultAspects()
	if out.Secret == AspectActionUnspecified {
		out.Secret = d.Secret
	}
	if out.Encrypted == AspectActionUnspecified {
		out.Encrypted = d.Encrypted
	}
	if out.Pseudonymized == AspectActionUnspecified {
		out.Pseudonymized = d.Pseudonymized
	}
	return
}

// Validate refuses a declaration that is not one of the four forms: every
// name an identifier, a scope setting and column named, every withhold on
// a passed section with a memberships column declared, no section twice.
func (inst Relation) Validate() (err error) {
	if !introspect.ValidTableName(inst.Name) {
		return eb.Build().Str("relation", inst.Name).Errorf("a relation name is an identifier")
	}
	if inst.Over == "" || !validReference(inst.Over) {
		return eb.Build().Str("relation", inst.Name).Str("over", inst.Over).Errorf("a relation reads one table or view")
	}
	if !introspect.ValidTableName(inst.Scope.Setting) || !introspect.ValidTableName(inst.Scope.Column) {
		return eb.Build().Str("relation", inst.Name).Errorf("a relation names its scope setting and column")
	}
	if v := inst.Scope.Via; v != nil && (!validReference(v.Table) || !introspect.ValidTableName(v.Key) || !introspect.ValidTableName(v.Column)) {
		return eb.Build().Str("relation", inst.Name).Errorf("a scope reached through a table names the table, its key and its column")
	}
	if e := inst.Scope.Except; e != "" && (!introspect.ValidTableName(e) || e == inst.Scope.Setting) {
		return eb.Build().Str("relation", inst.Name).Str("except", e).Errorf("a scope excludes by another setting")
	}
	seen := make(map[string]bool, len(inst.Sections))
	passed := make(map[string]bool, len(inst.Sections))
	for _, s := range inst.Sections {
		if !introspect.ValidTableName(s.Name) {
			return eb.Build().Str("relation", inst.Name).Str("section", s.Name).Errorf("a section name is an identifier")
		}
		if seen[s.Name] {
			return eb.Build().Str("relation", inst.Name).Str("section", s.Name).Errorf("a section is declared once")
		}
		seen[s.Name] = true
		passed[s.Name] = s.Pass
	}
	if len(inst.Sections) == 0 {
		return eb.Build().Str("relation", inst.Name).Errorf("a relation passes at least one section")
	}
	if inst.Memberships != "" && !introspect.ValidTableName(inst.Memberships) {
		return eb.Build().Str("relation", inst.Name).Errorf("the memberships column is an identifier")
	}
	for _, w := range inst.Withhold {
		if inst.Memberships == "" {
			return eb.Build().Str("relation", inst.Name).Errorf("a withhold rule needs a memberships column")
		}
		if !passed[w.Section] {
			return eb.Build().Str("relation", inst.Name).Str("section", w.Section).Errorf("a withhold rule names a passed section")
		}
		if len(w.Memberships) == 0 {
			return eb.Build().Str("relation", inst.Name).Str("section", w.Section).Errorf("a withhold rule names at least one membership")
		}
		for _, m := range w.Memberships {
			if m == "" || slices.ContainsFunc([]byte(m), func(c byte) bool { return c == '\'' || c == '\\' }) {
				return eb.Build().Str("relation", inst.Name).Str("membership", m).Errorf("a membership is a plain registry key")
			}
		}
	}
	return
}

// validReference admits `name` and `db.name`.
func validReference(ref string) bool {
	for i := 0; i < len(ref); i++ {
		if ref[i] == '.' {
			return i > 0 && introspect.ValidTableName(ref[:i]) && introspect.ValidTableName(ref[i+1:])
		}
	}
	return introspect.ValidTableName(ref)
}

// Passed are the names of the passed sections, in declaration order.
func (inst Relation) Passed() (names []string) {
	for _, s := range inst.Sections {
		if s.Pass {
			names = append(names, s.Name)
		}
	}
	return
}

// withheldFor is the set of memberships withheld on a section.
func (inst Relation) withheldFor(section string) (memberships []string) {
	for _, w := range inst.Withhold {
		if w.Section == section {
			memberships = append(memberships, w.Memberships...)
		}
	}
	slices.Sort(memberships)
	return slices.Compact(memberships)
}

// withheldAll is every membership any rule withholds.
func (inst Relation) withheldAll() (memberships []string) {
	for _, w := range inst.Withhold {
		memberships = append(memberships, w.Memberships...)
	}
	slices.Sort(memberships)
	return slices.Compact(memberships)
}
