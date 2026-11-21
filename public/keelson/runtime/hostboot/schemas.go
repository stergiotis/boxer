package hostboot

import (
	"context"

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/keelson/runtime/schemacheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// checkSchemas is the host's one schema check over the stored tables it
// uses (ADR-0387): the facts table — which the facts-bound stores, the
// trail and the watchbill presence among them, write with the same
// columns —, the state table, and, where the services that own them
// started, the journal's three tables and the watchbill pair. Each
// verdict lands on rt.Schemas, which keelson('schema_tables') serves, and
// a table whose verdict is not ok gets one log line. The check changes no
// backend choice; under BOXER_SCHEMA_STRICT a verdict other than ok fails
// the boot.
//
// A host whose facts store fell back to memory reaches no server and
// checks nothing.
func (rt *Runtime) checkSchemas(ctx context.Context, factsCfg chstore.Config) (err error) {
	rt.Schemas = schemacheck.NewBoard()
	if !rt.IsChStore {
		return
	}
	logger := rt.opts.Log
	exec := rt.PersistExec
	if exec == nil {
		// The persist service is off, or its backend fell back to memory:
		// the check reaches the server on its own.
		exec, err = storeexec.New(chclient.New(chclient.Config{URL: factsCfg.URL, User: factsCfg.User, Password: factsCfg.Password}, nil), nil)
		if err != nil {
			logger.Warn().Err(err).Msg("schema: no executor for the schema check; stored tables are not checked")
			err = nil
			return
		}
	}
	contracts := make([]recordstore.TableContract, 0, 8)
	if facts, ferr := chstore.SchemaContract(factsCfg); ferr != nil {
		logger.Warn().Err(ferr).Msg("schema: the facts table's contract could not be composed; it is not checked")
	} else {
		contracts = append(contracts, facts)
	}
	contracts = append(contracts, persiststore.PersistSchemaContract(""))
	if rt.PersistExec != nil {
		contracts = append(contracts, agentrunstore.Layout{}.SchemaContracts()...)
		if rt.opts.Services.Watchbill {
			contracts = append(contracts, watchbillstore.Layout{}.SchemaContracts()...)
		}
	}
	cctx, cancel := context.WithTimeout(ctx, persistOpenTimeout)
	defer cancel()
	_, err = schemacheck.Check(cctx, exec, rt.Schemas, logger, schemacheck.Strict.Get(), contracts...)
	return
}
