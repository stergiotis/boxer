package ddl

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/compiletimeflags"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

var CodeGeneratorName = "ET7 DDL (" + vcs.ModuleInfo() + ")"

func checkColumnsAllValid(columns []common.PhysicalColumnDesc) {
	if compiletimeflags.ExtraChecks {
		for i, c := range columns {
			if !c.IsValid() {
				log.Panic().Int("index", i).Interface("column", c).Interface("columns", columns).Msg("slice contains invalid column")
			}
		}
	}
}

type GeneratorDriver struct {
	phys []common.PhysicalColumnDesc
}

func NewGeneratorDriver() *GeneratorDriver {
	return &GeneratorDriver{
		phys: make([]common.PhysicalColumnDesc, 0, 1024),
	}
}
func (inst *GeneratorDriver) GenerateColumnsCode(iter common.IntermediateColumnIterator, tableRowConfig common.TableRowConfigE, conv common.NamingConventionI, tech common.TechnologySpecificGeneratorI, checkEncodingAspect func(hint encodingaspects.AspectE) (ok bool, msg string)) (err error) {
	switch tableRowConfig {
	case common.TableRowConfigMultiAttributesPerRow:
		break
	default:
		err = eb.Build().Stringer("tableRowConfig", tableRowConfig).Errorf("unhandled table row config")
		return
	}

	phys := inst.phys[:0]
	for cc, cp := range iter {
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, tableRowConfig)
		if err != nil {
			err = eh.Errorf("unable to map intermediate to physical column: %w", err)
			return
		}
	}
	checkColumnsAllValid(phys)

	hintLU := make(map[encodingaspects.AspectE][]string, 32)
	for _, p := range phys {
		{ // collect encoding aspects
			var hints encodingaspects.AspectSet
			hints, err = p.GetEncodingHints()
			if err != nil {
				err = eb.Build().Stringer("physicalColumn", p).Errorf("unable to get encoding hints: %w", err)
				return
			}
			for _, hint := range hints.IterateAspects() {
				if hint != encodingaspects.AspectNone {
					hintLU[hint] = append(hintLU[hint], p.String())
				}
			}
		}
		{ // check canonical type
			var ct canonicaltypes.PrimitiveAstNodeI
			ct, err = p.GetCanonicalType()
			if err != nil {
				err = eb.Build().Stringer("physicalColumn", p).Errorf("unable to get canonical type: %w", err)
				return
			}
			typeCompatible, msg := tech.CheckTypeCompatibility(ct)
			if !typeCompatible {
				err = eb.Build().Stringer("physicalColumn", p).Str("msg", msg).Str("technologyId", tech.GetTechnology().Id).Errorf("canonical type of column is not supported in given technology")
				return
			}
		}
	}
	if checkEncodingAspect != nil {
		for hint, ns := range hintLU {
			ok, msg := checkEncodingAspect(hint)
			if !ok {
				err = eb.Build().Strs("physicalColumnNames", ns).Stringer("encodingHint", hint).Str("msg", msg).Errorf("encoding hint of column does not pass check")
				return
			}
		}
	}

	for idx, p := range phys {
		err = tech.GenerateColumnCode(idx, p)
		if err != nil {
			err = eb.Build().Stringer("physicalColumn", p).Errorf("unable to generate column: %w", err)
			return
		}
	}

	return
}
