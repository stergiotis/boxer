package dml

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// plainColumnSetter is the setter of one plain column, beside the grouped
// setter of the column's PlainItemType.
type plainColumnSetter struct {
	// name is the exported method name; ctrlName renders it under private
	// control.
	name string
	// column is the column's name in lowerCamelCase, for the doc comment.
	column string
	cc     common.IntermediateColumnContext
	cp     *common.IntermediateColumnProps
	j      int
}

// plainColumnSetterCollisionSuffix disambiguates a per-column setter whose
// name is already taken by a grouped setter or another entity method.
const plainColumnSetterCollisionSuffix = "Column"

// plainColumnSetters names one setter per column of a grouped setter with
// more than one column: Set<Column>, or Set<Column>Column when that name is a
// grouped setter's or SetActiveSections. A single-column group gets none — its
// grouped setter already takes the one column. taken accumulates every name
// handed out, across item types.
func plainColumnSetters(irh *common.IntermediatePairHolder, taken map[string]struct{}) (setters []plainColumnSetter, err error) {
	if irh.CountColumns() < 2 {
		return
	}
	for cc, cp := range irh.IterateColumnProps() {
		for j := 0; j < cp.Length(); j++ {
			name := "Set" + cp.Names[j].Convert(naming.UpperCamelCase).String()
			if _, has := taken[name]; has {
				name += plainColumnSetterCollisionSuffix
			}
			if _, has := taken[name]; has {
				err = eb.Build().Str("column", cp.Names[j].String()).Str("setter", name).Errorf("per-column plain setter name collides with another entity method")
				return
			}
			taken[name] = struct{}{}
			setters = append(setters, plainColumnSetter{
				name:   name,
				column: cp.Names[j].Convert(naming.LowerCamelCase).String(),
				cc:     cc,
				cp:     cp,
				j:      j,
			})
		}
	}
	return
}

// plainColumnSettersByItemType names the per-column setters of every plain
// item type that has a grouped setter, in common.AllPlainItemTypes order.
func plainColumnSettersByItemType(plainSetterIRH *common.IntermediatePairHolder) (byType map[common.PlainItemTypeE][]plainColumnSetter, err error) {
	byType = make(map[common.PlainItemTypeE][]plainColumnSetter)
	taken := reservedEntitySetterNames()
	for _, pt := range common.AllPlainItemTypes {
		if itemTypeToSetterName(pt) == "" {
			continue
		}
		irh := plainSetterIRH.DeriveSubHolder(func(cc common.IntermediateColumnContext) (keep bool) {
			return cc.PlainItemType == pt
		})
		if irh.Length() == 0 {
			continue
		}
		byType[pt], err = plainColumnSetters(irh, taken)
		if err != nil {
			return
		}
	}
	return
}

// reservedEntitySetterNames returns the exported Set* method names of an
// entity class that are not per-column setters.
func reservedEntitySetterNames() (taken map[string]struct{}) {
	taken = map[string]struct{}{"SetActiveSections": {}}
	for _, pt := range common.AllPlainItemTypes {
		if s := itemTypeToSetterName(pt); s != "" {
			taken[s] = struct{}{}
		}
	}
	return
}

// composeGroupedSetterDoc writes the doc comment of the grouped setter of an
// item type whose per-column setters are setters.
func (inst *GoClassBuilder) composeGroupedSetterDoc(setterName string, irh *common.IntermediatePairHolder, setters []plainColumnSetter) (err error) {
	b := inst.builder
	if irh.CountColumns() < 2 {
		_, err = fmt.Fprintf(b, "// %s sets the entity's %s column.\n", setterName, firstColumnName(irh))
		return
	}
	names := make([]string, 0, len(setters))
	for _, s := range setters {
		names = append(names, inst.ctrlName(s.name))
	}
	text := setterName + " sets the entity's " + strings.Join(columnNames(irh), ", ") +
		" columns, one positional argument per column in that order. Hand-written code should call the per-column setters (" +
		strings.Join(names, ", ") + ") instead: they cannot swap two arguments of the same type."
	_, err = b.WriteString(wrapDocComment(text))
	return
}

// composePlainColumnSetters writes one setter per plain column. Each checks
// the entity state and assigns its column exactly as the grouped setter does
// for that column, so any mix of grouped and per-column calls within one
// entity frame leaves the same row.
func (inst *GoClassBuilder) composePlainColumnSetters(entityClassName string, setters []plainColumnSetter) (err error) {
	b := inst.builder
	for _, s := range setters {
		name := inst.ctrlName(s.name)
		gocodegen.EmitGeneratingCodeLocation(b)
		_, err = fmt.Fprintf(b, "// %s sets the entity's %s column.\nfunc (inst *%s) %s(", name, s.column, entityClassName, name)
		if err != nil {
			return
		}
		err = inst.composeFieldRelatedCode(structFieldOperationArgDeclaration, s.cc, s.cp, s.j)
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(b, ") *%s {\n", entityClassName)
		if err != nil {
			return
		}
		err = inst.composeStateVerificationCode([]runtime.EntityStateE{runtime.EntityStateInEntity}, false, "inst")
		if err != nil {
			return
		}
		err = inst.composeFieldRelatedCode(structFieldOperationPlainAssignArg, s.cc, s.cp, s.j)
		if err != nil {
			return
		}
		_, err = b.WriteString("\n\treturn inst\n}\n")
		if err != nil {
			return
		}
	}
	return
}

// composePlainColumnSetterDrivers writes the exported driver of each
// per-column setter under private control.
func (inst *GoClassBuilder) composePlainColumnSetterDrivers(entityClassName string, setters []plainColumnSetter) (err error) {
	b := inst.builder
	for _, s := range setters {
		_, err = fmt.Fprintf(b, "func %s%s(e *%s, ", entityClassName, s.name, entityClassName)
		if err != nil {
			return
		}
		err = inst.composeFieldRelatedCode(structFieldOperationArgDeclaration, s.cc, s.cp, s.j)
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(b, ") *%s { return e.%s(", entityClassName, lowerFirst(s.name))
		if err != nil {
			return
		}
		err = inst.composeFieldRelatedCode(structFieldOperationArgUse, s.cc, s.cp, s.j)
		if err != nil {
			return
		}
		_, err = b.WriteString(") }\n")
		if err != nil {
			return
		}
	}
	return
}

func columnNames(irh *common.IntermediatePairHolder) (names []string) {
	for _, cp := range irh.IterateColumnProps() {
		for j := 0; j < cp.Length(); j++ {
			names = append(names, cp.Names[j].Convert(naming.LowerCamelCase).String())
		}
	}
	return
}

func firstColumnName(irh *common.IntermediatePairHolder) string {
	names := columnNames(irh)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// wrapDocComment renders text as a // comment wrapped at 78 columns.
func wrapDocComment(text string) string {
	const width = 78
	var sb strings.Builder
	line := "//"
	for _, w := range strings.Fields(text) {
		if len(line)+1+len(w) > width && line != "//" {
			sb.WriteString(line)
			sb.WriteByte('\n')
			line = "//"
		}
		line += " " + w
	}
	sb.WriteString(line)
	sb.WriteByte('\n')
	return sb.String()
}
