package readaccess

import (
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
)

type GoClassBuilder struct {
	builder         *strings.Builder
	tech            *golang.TechnologySpecificCodeGenerator
	physicalColumns []common.PhysicalColumnDesc
}
type GeneratorDriver struct {
	builder          *GoClassBuilder
	validator        *common.TableValidator
	namingConvention common.NamingConventionI
	tech             common.TechnologySpecificGeneratorI
}
