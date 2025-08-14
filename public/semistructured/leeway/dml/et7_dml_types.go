package dml

import (
	"io"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

type GoClassNamerI interface {
	ComposeSchemaFactoryName(tableName common.StylableName) (functionName string, err error)
	ComposeEntityClassName(tableName common.StylableName) (fullClassName string, err error)
	ComposeSectionClassName(tableName common.StylableName, sectionName common.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error)
	ComposeAttributeClassName(tableName common.StylableName, sectionName common.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error)
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
	GetSerializer(canonicalType canonicalTypes.PrimitiveAstNodeI) (bufser BufferingSerializerI, err error)
}

type ArrowValueAdder struct {
	s *strings.Builder
}

var _ common.CodeBuilderHolderI = (*ArrowValueAdder)(nil)

type TransferRecordsI interface {
	TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error)
}
