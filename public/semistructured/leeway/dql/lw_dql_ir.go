package dql

import (
	"iter"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

type InformationRetrieval struct {
	conv           common.NamingConventionI
	ir             *common.IntermediateTableRepresentation
	tableRowConfig common.TableRowConfigE

	phys    []common.PhysicalColumnDesc
	records []ColumnRecord
}
type ColumnRecord struct {
	Index          int                              `cbor:"index"`
	ColumnContext  common.IntermediateColumnContext `cbor:"columnContext"`
	Name           naming.StylableName              `cbor:"name"`
	Role           common.ColumnRoleE               `cbor:"role"`
	CanonicalType  canonicaltypes.PrimitiveAstNodeI `cbor:"canonicalType"`
	EncodingHints  encodingaspects.AspectSet        `cbor:"encodingHints"`
	ValueSemantics valueaspects.AspectSet           `cbor:"valueSemantics"`
	PhysicalColumn common.PhysicalColumnDesc        `cbor:"physicalColumn"`
}

func NewInformationRetrieval(conv common.NamingConventionI) *InformationRetrieval {
	return &InformationRetrieval{
		conv:    conv,
		ir:      nil,
		phys:    make([]common.PhysicalColumnDesc, 0, 16),
		records: make([]ColumnRecord, 0, 16),
	}
}
func (inst *InformationRetrieval) Reset() {
	inst.ir = nil
	inst.tableRowConfig = 0
	inst.phys = inst.phys[:0]
	inst.records = inst.records[:0]
}

var ErrInvariantViolation = eh.Errorf("an invariance has been violated")

func (inst *InformationRetrieval) LoadTable(ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE) (err error) {
	i := 0
	conv := inst.conv
	inst.Reset()
	records := slices.Grow(inst.records[:0], ir.TotalLength())
	phys := slices.Grow(inst.phys[:0], ir.TotalLength())
	var r ColumnRecord
	for cc, cp := range ir.IterateColumnProps() {
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, tableRowConfig)
		r.ColumnContext = cc
		if err != nil {
			err = eh.Errorf("unable to map to physical column: %w", err)
			return
		}
		l := cp.Length()
		if len(phys) != i+l {
			err = eb.Build().Int("idx", i).Int("physicalColumns", len(phys)).Int("cpLength", cp.Length()).Errorf("invariance violation: expecting one physical column per column: %w", ErrInvariantViolation)
			return
		}
		for j := 0; j < l; j++ {
			r.Index = i
			r.Name = cp.Names[j]
			r.Role = cp.Roles[j]
			r.CanonicalType = cp.CanonicalType[j]
			r.EncodingHints = cp.EncodingHints[j]
			r.ValueSemantics = cp.ValueSemantics[j]
			r.PhysicalColumn = phys[i]
			records = append(records, r)
			i++
		}
	}
	inst.ir = ir
	inst.tableRowConfig = tableRowConfig
	inst.phys = phys
	inst.records = records
	return
}

func (inst *InformationRetrieval) IterateAll() iter.Seq[ColumnRecord] {
	return slices.Values(inst.records)
}
