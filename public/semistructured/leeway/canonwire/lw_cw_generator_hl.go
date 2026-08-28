package canonwire

import (
	"fmt"
	"go/format"
	"strings"

	"github.com/stergiotis/boxer/public/code/synthesis/golang"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

var CodeGeneratorName = "Leeway canonwire (" + vcs.ModuleInfo() + ")"

func NewGoCodeGeneratorDriver(namingConvention common.NamingConventionI, tech common.TechnologySpecificGeneratorI) *GeneratorDriver {
	return &GeneratorDriver{
		builder:          NewGoClassBuilder(),
		validator:        common.NewTableValidator(),
		namingConvention: namingConvention,
		tech:             tech,
	}
}

// GenerateGoClasses emits the canonical-wire classes for one table into one Go
// file: the slot signature constants, the slot enum, the tagger and dispatcher
// interfaces with their built-in ordinal implementations, the encoder over the
// table's generated readaccess classes and the decoder into its generated dml
// builders (ADR-0207 SD5, SD6).
//
// The file is emitted into packageName, which must be the package the table's
// readaccess *and* dml classes were generated into — the codec calls both by
// name.
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

	// The intermediate representation is loaded for its checks alone: it is
	// what refuses a table the technology cannot carry, and the encoder must
	// not be emitted for a table whose readaccess classes will not exist. The
	// wire form itself is keyed on canonical types, so nothing below reads it.
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tblDesc, inst.tech)
	if err != nil {
		err = eh.Errorf("unable to load table to intermediate representation: %w", err)
		return
	}

	builder := inst.builder
	builder.SetCodeBuilder(s)

	_, err = s.WriteString("package " + packageName + "\n")
	if err != nil {
		err = eh.Errorf("unable to write package name %w", err)
		return
	}
	_, err = fmt.Fprintf(s, `
import (
	"io"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	cwruntime "github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// The imports a table without plain sections, without memberships or without a
// temporal column would otherwise leave unused. The generated code is one file
// per table and the set it touches depends on the table's shape.
var (
	_ = time.Time{}
	_ = common.PlainItemTypeNone
	_ = mappingplan.MembershipChannelLowCardRef
	_ = eb.Build
)
`)
	if err != nil {
		err = eh.Errorf("unable to write imports %w", err)
		return
	}

	err = builder.ComposeCodec(tableName, &tblDesc, clsNamer)
	if err != nil {
		err = eh.Errorf("unable to compose go code: %w", err)
		return
	}

	sourceCode = unsafeperf.UnsafeStringToBytes(s.String())
	{ // try formatting source code
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
