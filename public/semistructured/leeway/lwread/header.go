package lwread

import (
	"strconv"
	"strings"
)

// Qualify prefixes the name of every attribute whose name two sections give
// with its section, as section·name, so a name says one thing across the
// batch. It is idempotent.
func (inst *Model) Qualify() {
	sections := map[string]map[string]bool{}
	for _, r := range inst.Records {
		for _, a := range r.Attributes {
			if a.Qualified {
				continue
			}
			if sections[a.Name] == nil {
				sections[a.Name] = map[string]bool{}
			}
			sections[a.Name][a.Section] = true
		}
	}
	for ri := range inst.Records {
		for ai := range inst.Records[ri].Attributes {
			a := &inst.Records[ri].Attributes[ai]
			if !a.Qualified && len(sections[a.Name]) > 1 {
				a.Name, a.Qualified = a.Section+"·"+a.Name, true
			}
		}
	}
}

// AttributeSpec is one attribute as the batch holds it.
type AttributeSpec struct {
	Name    string
	Section string
	Plain   bool
	// Columns are the value columns the reading shows, as column:type, a
	// list or set type suffixed with [] or {}.
	Columns []string
	// Records is how many records carry the attribute.
	Records int
	// Handle reads the attribute in SQL: LW_GET('section', name or ref id),
	// LW_GET_LIST for a list column, with 'col:<column>' when the section
	// has several value columns. Empty for a plain column, whose handle is
	// its column's, and for an attribute no membership names or whose name
	// carries parameters.
	Handle string
}

// Header summarises the batch by attribute, in the order attributes are
// first seen.
func (inst *Model) Header() (specs []AttributeSpec) {
	idx := map[string]int{}
	for _, r := range inst.Records {
		seen := map[int]bool{}
		for _, a := range r.Attributes {
			key := a.Section + "\x00" + a.Name
			i, ok := idx[key]
			if !ok {
				i = len(specs)
				idx[key] = i
				s := AttributeSpec{Name: a.Name, Section: a.Section, Plain: a.Plain, Handle: handle(&a)}
				for _, v := range a.Values {
					col := v.Column + ":" + v.Type
					switch v.Shape {
					case ShapeList:
						col += "[]"
					case ShapeSet:
						col += "{}"
					}
					s.Columns = append(s.Columns, col)
				}
				specs = append(specs, s)
			}
			if !seen[i] {
				seen[i] = true
				specs[i].Records++
			}
		}
	}
	return
}

// handle is the LW_GET expression reading a, or empty.
func handle(a *Attribute) string {
	m := a.Named
	if a.Plain || m == nil || m.Params != "" || len(a.Values) == 0 {
		return ""
	}
	fn := "LW_GET"
	if a.Values[0].Shape != ShapeScalar {
		fn = "LW_GET_LIST"
	}
	member := sqlString(m.Name)
	if m.IsRef {
		member = strconv.FormatUint(m.Ref, 10)
	}
	h := fn + "(" + sqlString(a.Section) + ", " + member
	if a.SectionColumns > 1 {
		h += ", " + sqlString("col:"+a.Values[0].Column)
	}
	return h + ")"
}

// sqlString is s as a ClickHouse string literal.
func sqlString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}
