package dml

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
)

// BuilderPackage names the package whose typed-builder API the
// generated DML targets. Default targets arrow-go's array package; an
// API-compatible shim (e.g. for sparse RowBinary or sparse CBOR
// output) can be substituted by overriding the fields. The Alias is
// the identifier used in emitted type references — code reads
// `*<Alias>.Int64Builder`, so production sites can grep for the shim
// package name directly.
//
// RecordType is the concrete type returned by the package's
// `<Alias>.NewRecordBuilder(...).NewRecord()`. For arrow-go it is the
// interface `arrow.RecordBatch`; for shim packages it is typically a
// concrete pointer like `*<Alias>.Record`. The generator substitutes
// it into the `inst.records` slice field, the records-slice init, and
// the `TransferRecords` method signature.
type BuilderPackage struct {
	ImportPath string // e.g. "github.com/apache/arrow-go/v18/arrow/array"
	Alias      string // e.g. "array"
	RecordType string // e.g. "arrow.RecordBatch" or "*arrowrowbinary.Record"
}

// DefaultBuilderPackage targets arrow-go's array package — the
// original behaviour before BuilderPackage was parameterised.
func DefaultBuilderPackage() BuilderPackage {
	return BuilderPackage{
		ImportPath: "github.com/apache/arrow-go/v18/arrow/array",
		Alias:      "array",
		RecordType: "arrow.RecordBatch",
	}
}

type TechnologySpecificBuilderI interface {
	common.CodeBuilderHolderI
}

type TransferRecordsI interface {
	TransferRecords(recordsIn []arrow.RecordBatch) (recordsOut []arrow.RecordBatch, err error)
}

type GoClassBuilder struct {
	builder    *strings.Builder
	tech       *golang.TechnologySpecificCodeGenerator
	builderPkg BuilderPackage
	// privateControl emits the entity control surface — the frame lifecycle
	// (BeginEntity/CommitEntity/RollbackEntity), the record drain
	// (TransferRecords), SetActiveSections, the plain setters and the raw
	// builder accessor — unexported, plus exported driver functions the
	// owning store calls. External holders of the builder value cannot reach
	// the control surface (they can neither import this package to call the
	// drivers nor recover the unexported methods by casting), so a store can
	// wall frame control while still exposing the section/attribute surface
	// through Raw() (ADR-0100 SD6). The default emits everything exported.
	privateControl bool
}
