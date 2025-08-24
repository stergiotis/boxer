package dml

import (
	"go/format"
	"strings"

	"github.com/stergiotis/boxer/public/code/synthesis/golang"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

type GeneratorDriver struct {
	builder          *GoClassBuilder
	validator        *common.TableValidator
	namingConvention common.NamingConventionI
	tech             common.TechnologySpecificGeneratorI
}

func NewGoCodeGeneratorDriver(namingConvention common.NamingConventionI, tech common.TechnologySpecificGeneratorI) *GeneratorDriver {
	builder := NewGoClassBuilder()
	return &GeneratorDriver{
		builder:          builder,
		validator:        common.NewTableValidator(),
		namingConvention: namingConvention,
		tech:             tech,
	}
}
func (inst *GeneratorDriver) GenerateGoClasses(packageName string, tableName naming.StylableName, tblDesc common.TableDesc, tableRowConfig common.TableRowConfigE, namingStyle GoClassNamerI) (sourceCode []byte, wellFormed bool, err error) {
	s := &strings.Builder{}
	_, err = golang.AddCodeGenComment(s, CodeGeneratorName)
	if err != nil {
		err = eh.Errorf("unable to add codegen name: %w", err)
		return
	}
	err = inst.validator.ValidateTable(&tblDesc)
	if err != nil {
		err = eh.Errorf("table does not validate: %w", err)
		return
	}

	builder := inst.builder
	builder.SetCodeBuilder(s)
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tblDesc, inst.tech)
	if err != nil {
		err = eh.Errorf("unable to load table to intermediate representation: %w", err)
		return
	}

	_, err = s.WriteString("package " + packageName + "\n")
	if err != nil {
		err = eh.Errorf("unable to write package name %w", err)
		return
	}
	_, err = s.WriteString(`
import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	_ "github.com/apache/arrow-go/v18/arrow/ipc"
	_ "github.com/apache/arrow-go/v18/arrow/math"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
`)
	if err != nil {
		err = eh.Errorf("unable to write imports %w", err)
		return
	}
	err = builder.ComposeGoImports(ir, tableRowConfig)
	if err != nil {
		err = eh.Errorf("unable to compose go imports: %w", err)
		return
	}
	_, err = s.WriteString("\n)\n")
	if err != nil {
		return
	}

	err = builder.ComposeCode(tableName, ir, inst.namingConvention, tableRowConfig, namingStyle)
	if err != nil {
		err = eh.Errorf("unable to compose go code: %w", err)
		return
	}

	sourceCode = unsafeperf.UnsafeStringToByte(s.String()) // s is not reachable anymore
	{                                                      // try formatting source code
		var formatted []byte
		formatted, err = format.Source(sourceCode)
		if err != nil {
			formatted = sourceCode
			err = nil
		} else {
			sourceCode = formatted
			wellFormed = true
		}
	}
	return
}
