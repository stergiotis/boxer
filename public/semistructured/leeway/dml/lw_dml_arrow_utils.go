package dml

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
)

func WriteArrowRecords[E TransferRecordsI](ent E, records []arrow.Record, w *ipc.FileWriter, w2 *pqarrow.FileWriter) (recordsOut []arrow.Record, err error) {
	recordsOut, err = ent.TransferRecords(records)
	if err != nil {
		err = eh.Errorf("unable to transfer records: %w", err)
		return
	}
	if w != nil {
		for i, r := range recordsOut {
			if r == nil {
				log.Warn().Int("idx", i).Msg("encountered nil record, skipping")
				continue
			}
			err = w.Write(r)
			if err != nil {
				err = eh.Errorf("unable to write record to arrow ipc file writer: %w", err)
				return
			}
		}
	} else if w2 != nil {
		for i, r := range recordsOut {
			if r == nil {
				log.Warn().Int("idx", i).Msg("encountered nil record, skipping")
				continue
			}
			err = w2.WriteBuffered(r)
			if err != nil {
				err = eh.Errorf("unable to write record to parquet file writer: %w", err)
				return
			}
		}
	}
	var rows int64
	for _, r := range recordsOut {
		if r == nil {
			continue
		}
		rows += r.NumRows()
		r.Release()
	}
	log.Info().Int("records", len(recordsOut)).Int64("rows", rows).Msg("wrote records")
	clear(recordsOut)
	recordsOut = recordsOut[:0]
	return
}
