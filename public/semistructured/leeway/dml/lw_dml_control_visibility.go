package dml

import (
	"fmt"
	"unicode"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
)

// lowerFirst returns s with its first rune lower-cased. It renders the
// unexported name of a control-surface method under private control.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// ctrlName renders a control-surface method name: unexported when the
// builder emits with private control (ADR-0100 SD6), exported otherwise.
// The frame lifecycle, the record drain, SetActiveSections and the plain
// setters are the control surface — a store owns them and reaches them
// through the emitted driver functions; the section getters and the
// section/attribute methods stay exported (the Raw() escape hatch).
func (inst *GoClassBuilder) ctrlName(exported string) string {
	if inst.privateControl {
		return lowerFirst(exported)
	}
	return exported
}

// composeControlDrivers emits, for a private-control builder, the exported
// free-function drivers over the now-unexported control surface. They live
// in the builder's package (internal/lowlevel for a recordstore); an
// external holder of the builder value cannot import that package to call
// them, and cannot recover the unexported methods by casting the value, so
// control stays walled by construction (ADR-0100 SD6). Every driver name is
// prefixed with the builder type so several tables can share one package
// (Go has no function overloading).
func (inst *GoClassBuilder) composeControlDrivers(clsNames gocodegen.ClassNames, plainSetterIRH *common.IntermediatePairHolder) (err error) {
	if !inst.privateControl {
		return
	}
	b := inst.builder
	icn := clsNames.InEntityClassName
	rec := inst.builderPkg.RecordType
	_, err = fmt.Fprintf(b, `
// --- store control drivers (ADR-0100 SD6): exported access to %s's
// unexported frame control, callable only from within this package. ---
func %sBeginEntity(e *%s) *%s { return e.beginEntity() }
func %sCommitEntity(e *%s) error { return e.commitEntity() }
func %sRollbackEntity(e *%s) error { return e.rollbackEntity() }
func %sTransferRecords(e *%s, recordsIn []%s) (recordsOut []%s, err error) { return e.transferRecords(recordsIn) }
func %sSetActiveSections(e *%s, idxs []int) { e.setActiveSections(idxs) }
func %sReleaseBuilder(e *%s) { e.builder.Release() }
`, icn,
		icn, icn, icn,
		icn, icn,
		icn, icn,
		icn, icn, rec, rec,
		icn, icn,
		icn, icn)
	if err != nil {
		return
	}
	// Plain-setter drivers — one per PlainItemType present, mirroring each
	// setter method's argument list (declaration for the params, use for the
	// forwarding call, so the two agree by construction).
	for _, pt := range common.AllPlainItemTypes {
		setter := itemTypeToSetterName(pt)
		if setter == "" {
			continue
		}
		irh := plainSetterIRH.DeriveSubHolder(func(cc common.IntermediateColumnContext) (keep bool) {
			return cc.PlainItemType == pt
		})
		if irh.Length() == 0 {
			continue
		}
		_, err = fmt.Fprintf(b, "func %s%s(e *%s, ", icn, setter, icn)
		if err != nil {
			return
		}
		first := true
		for cc, cp := range irh.IterateColumnProps() {
			for j := 0; j < cp.Length(); j++ {
				if !first {
					_, err = b.WriteString(", ")
					if err != nil {
						return
					}
				}
				first = false
				err = inst.composeFieldRelatedCode(structFieldOperationArgDeclaration, cc, cp, j)
				if err != nil {
					return
				}
			}
		}
		_, err = fmt.Fprintf(b, ") *%s { return e.%s(", icn, lowerFirst(setter))
		if err != nil {
			return
		}
		first = true
		for cc, cp := range irh.IterateColumnProps() {
			for j := 0; j < cp.Length(); j++ {
				if !first {
					_, err = b.WriteString(", ")
					if err != nil {
						return
					}
				}
				first = false
				err = inst.composeFieldRelatedCode(structFieldOperationArgUse, cc, cp, j)
				if err != nil {
					return
				}
			}
		}
		_, err = b.WriteString(") }\n")
		if err != nil {
			return
		}
	}
	return
}
