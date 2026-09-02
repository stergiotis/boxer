package gocodegen

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// The canonical-wire class names (ADR-0210 SD6). They follow the two existing
// families: the read-access namer prefixes with ReadAccess and the DML namer
// with InEntity, so this one prefixes with CanonWire — CanonWireEncoder<Table>,
// CanonWireTagger<Table>I, CanonWireSlot<Table>E.
//
// A slot is named after the sections it joins, concatenated in signature order:
// a standalone section gives CanonWireSlotTestTableGeo, a co-section group of
// geo and h3 gives CanonWireSlotPlaceGeoH3. The names are unique because
// section names are, and the slot ordinal is only used as a fallback for a slot
// that somehow joins no named section.

// composeCanonWireSlotSuffix joins a slot's section names into the tail of a
// slot or signature constant's name.
func composeCanonWireSlotSuffix(slotOrdinal int, sectionNames []naming.StylableName) (suffix string, err error) {
	var sb strings.Builder
	for _, n := range sectionNames {
		if !n.IsValid() {
			err = eb.Build().Stringer("sectionName", n).Errorf("sectionName is invalid")
			return
		}
		sb.WriteString(n.Convert(naming.UpperCamelCase).String())
	}
	suffix = sb.String()
	if suffix == "" {
		suffix = fmt.Sprintf("Slot%02d", slotOrdinal)
	}
	return
}

func checkTableName(tableName naming.StylableName) (err error) {
	if !tableName.IsValid() {
		err = eb.Build().Stringer("tableName", tableName).Errorf("tableName is invalid")
	}
	return
}

func (inst *DefaultGoClassNamer) ComposeCanonWireEncoderClassName(tableName naming.StylableName) (className string, err error) {
	return "CanonWireEncoder", nil
}

func (inst *DefaultGoClassNamer) ComposeCanonWireDecoderClassName(tableName naming.StylableName) (className string, err error) {
	return "CanonWireDecoder", nil
}

func (inst *DefaultGoClassNamer) ComposeCanonWireTaggerInterfaceName(tableName naming.StylableName) (interfaceName string, err error) {
	return "CanonWireTaggerI", nil
}

func (inst *DefaultGoClassNamer) ComposeCanonWireDispatcherInterfaceName(tableName naming.StylableName) (interfaceName string, err error) {
	return "CanonWireDispatcherI", nil
}

func (inst *DefaultGoClassNamer) ComposeCanonWireSlotEnumName(tableName naming.StylableName) (enumName string, err error) {
	return "CanonWireSlotE", nil
}

func (inst *DefaultGoClassNamer) ComposeCanonWireSlotConstName(tableName naming.StylableName, slotOrdinal int, sectionNames []naming.StylableName) (constName string, err error) {
	suffix, err := composeCanonWireSlotSuffix(slotOrdinal, sectionNames)
	if err != nil {
		return
	}
	return "CanonWireSlot" + suffix, nil
}

func (inst *DefaultGoClassNamer) ComposeCanonWireSignatureConstName(tableName naming.StylableName, slotOrdinal int, sectionNames []naming.StylableName) (constName string, err error) {
	suffix, err := composeCanonWireSlotSuffix(slotOrdinal, sectionNames)
	if err != nil {
		return
	}
	return "CanonWireSignature" + suffix, nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeCanonWireEncoderClassName(tableName naming.StylableName) (className string, err error) {
	if err = checkTableName(tableName); err != nil {
		return
	}
	return "CanonWireEncoder" + tableName.Convert(naming.UpperCamelCase).String(), nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeCanonWireDecoderClassName(tableName naming.StylableName) (className string, err error) {
	if err = checkTableName(tableName); err != nil {
		return
	}
	return "CanonWireDecoder" + tableName.Convert(naming.UpperCamelCase).String(), nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeCanonWireTaggerInterfaceName(tableName naming.StylableName) (interfaceName string, err error) {
	if err = checkTableName(tableName); err != nil {
		return
	}
	return "CanonWireTagger" + tableName.Convert(naming.UpperCamelCase).String() + "I", nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeCanonWireDispatcherInterfaceName(tableName naming.StylableName) (interfaceName string, err error) {
	if err = checkTableName(tableName); err != nil {
		return
	}
	return "CanonWireDispatcher" + tableName.Convert(naming.UpperCamelCase).String() + "I", nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeCanonWireSlotEnumName(tableName naming.StylableName) (enumName string, err error) {
	if err = checkTableName(tableName); err != nil {
		return
	}
	return "CanonWireSlot" + tableName.Convert(naming.UpperCamelCase).String() + "E", nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeCanonWireSlotConstName(tableName naming.StylableName, slotOrdinal int, sectionNames []naming.StylableName) (constName string, err error) {
	if err = checkTableName(tableName); err != nil {
		return
	}
	suffix, err := composeCanonWireSlotSuffix(slotOrdinal, sectionNames)
	if err != nil {
		return
	}
	return "CanonWireSlot" + tableName.Convert(naming.UpperCamelCase).String() + suffix, nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeCanonWireSignatureConstName(tableName naming.StylableName, slotOrdinal int, sectionNames []naming.StylableName) (constName string, err error) {
	if err = checkTableName(tableName); err != nil {
		return
	}
	suffix, err := composeCanonWireSlotSuffix(slotOrdinal, sectionNames)
	if err != nil {
		return
	}
	return "CanonWireSignature" + tableName.Convert(naming.UpperCamelCase).String() + suffix, nil
}
