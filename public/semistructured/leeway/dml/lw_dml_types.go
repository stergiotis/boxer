package dml

import (
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

type GoClassNamerI interface {
	ComposeSchemaFactoryName(tableName naming.StylableName) (functionName string, err error)
	ComposeEntityClassName(tableName naming.StylableName) (fullClassName string, err error)
	ComposeSectionClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error)
	ComposeAttributeClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error)
	functional.PromiseReferentialTransparentI
}

type DefaultGoClassNamer struct {
}

var _ GoClassNamerI = (*DefaultGoClassNamer)(nil)

type MultiTablePerPackageClassNamer struct {
}

var _ GoClassNamerI = (*MultiTablePerPackageClassNamer)(nil)

type TechnologySpecificBuilderI interface {
	common.CodeBuilderHolderI
}

type BufferingSerializerI interface {
	io.WriterTo
	Reset()
}

type CanonicalTypeSerializerI interface {
	GetSerializer(canonicalType canonicaltypes.PrimitiveAstNodeI) (bufser BufferingSerializerI, err error)
}

type TransferRecordsI interface {
	TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error)
}
