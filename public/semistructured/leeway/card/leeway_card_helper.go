package card

import (
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

// InferDriverFromRecordBatch FIXME this is way to specific
func InferDriverFromRecordBatch(recordBatch arrow.RecordBatch, lastColNames []string) (cardDriver *streamreadaccess.Driver, same bool, colNames []string, err error) {
	n := recordBatch.NumCols()
	if n == int64(len(lastColNames)) {
		same = true
		for i := int64(0); i < n; i++ {
			same = same && lastColNames[i] == recordBatch.ColumnName(int(i))
		}
		if same {
			return
		}
	}
	var conv *ddl.HumanReadableNamingConvention
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	if err != nil {
		err = eh.Errorf("unable to create human readable naming convention: %w", err)
		return
	}

	colNames = slices.Grow(lastColNames[:0], int(n))
	for i := int64(0); i < n; i++ {
		f := recordBatch.ColumnName(int(i))
		_, err = conv.ParseColumn(f)
		if err != nil {
			log.Trace().Str("column", f).Msg("skipping non-leeway column")
			err = nil
			continue
		}
		colNames = append(colNames, f)
	}

	var tblDesc common.TableDesc
	var tableRowConfig common.TableRowConfigE
	tblDesc, tableRowConfig, err = conv.DiscoverTableFromColumnNames(colNames)
	if err != nil {
		err = eb.Build().Strs("colNames", colNames).Errorf("unable to discover table from column names: %w", err)
		return
	}
	_ = tableRowConfig

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tblDesc, tech)
	if err != nil {
		err = eh.Errorf("unable to load table into intermediate representation: %w", err)
		return
	}

	fmts := streamreadaccess.DefaultFormatters()
	cardDriver, err = streamreadaccess.NewDriverFromSchema(&tblDesc, ir, fmts, recordBatch.Schema(), conv, tableRowConfig)
	if err != nil {
		err = eh.Errorf("unable to create online API driver: %w", err)
		return
	}
	return
}
