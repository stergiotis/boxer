package dml

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
)

type TechnologySpecificBuilderI interface {
	common.CodeBuilderHolderI
}

type TransferRecordsI interface {
	TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error)
}
type GoClassBuilder struct {
	builder *strings.Builder
	tech    *golang.TechnologySpecificCodeGenerator
}
