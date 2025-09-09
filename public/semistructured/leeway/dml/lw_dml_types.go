package dml

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

type TechnologySpecificBuilderI interface {
	common.CodeBuilderHolderI
}

type TransferRecordsI interface {
	TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error)
}
