package readaccess

import (
	"fmt"
	"go/format"
	"strings"

	"github.com/stergiotis/boxer/public/code/synthesis/golang"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

func NewGoCodeGeneratorDriver(namingConvention common.NamingConventionI, tech common.TechnologySpecificGeneratorI) *GeneratorDriver {
	builder := NewGoClassBuilder()
	return &GeneratorDriver{
		builder:          builder,
		validator:        common.NewTableValidator(),
		namingConvention: namingConvention,
		tech:             tech,
	}
}
func (inst *GeneratorDriver) GenerateGoClasses(packageName string, tableName naming.StylableName, tblDesc common.TableDesc, tableRowConfig common.TableRowConfigE, clsNamer gocodegen.GoClassNamerI) (sourceCode []byte, wellFormed bool, err error) {
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
	_, err = s.WriteString("import (")
	if err != nil {
		err = eh.Errorf("unable to write imports %w", err)
		return
	}
	suppressedImports := containers.NewHashSet[string](1)
	suppressedImports.Add("time")

	addImport := func(imp string) (err error) {
		if !suppressedImports.AddEx(imp) {
			_, err = fmt.Fprintf(s, "%q\n", imp)
		}
		return
	}
	gocodegen.EmitGeneratingCodeLocation(s)
	for _, imp := range []string{
		"slices",
		"github.com/apache/arrow-go/v18/arrow",
		"github.com/apache/arrow-go/v18/arrow/array",
		"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime",
		"github.com/stergiotis/boxer/public/observability/eh/eb",
	} {
		err = addImport(imp)
		if err != nil {
			err = eh.Errorf("unable to write imports %w", err)
			return
		}
	}

	gocodegen.EmitGeneratingCodeLocation(s)
	err = builder.ComposeGoImports(ir, tableRowConfig, suppressedImports)
	if err != nil {
		err = eh.Errorf("unable to compose go imports: %w", err)
		return
	}
	_, err = s.WriteString("\n)\n")
	if err != nil {
		return
	}

	err = gocodegen.ComposeCode(builder, s, tableName, ir, inst.namingConvention, tableRowConfig, clsNamer)
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
