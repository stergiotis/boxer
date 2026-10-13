package hostboot

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/headstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor/anchorsql"
)

// anchorStoreTimeout bounds provisioning the organisation's table at boot.
const anchorStoreTimeout = 10 * time.Second

// orgAnchorStore is the organisation's anchor store where
// BOXER_ANCHOR_STORE_URL names one and its table provisions, the memory
// store otherwise (ADR-0314 §SD2, update of 2026-10-13). Its receipts name
// this run when the host appends for itself.
func orgAnchorStore(ctx context.Context, runId string, logger zerolog.Logger) (st anchor.StoreI) {
	url := headstore.OrgStoreURL.Get()
	if url == "" {
		return anchor.NewMemStore()
	}
	exec, err := storeexec.New(chclient.New(chclient.Config{URL: url, User: headstore.OrgStoreUser.Get(), Password: headstore.OrgStorePassword.Get()}, nil), nil)
	if err != nil {
		logger.Warn().Err(err).Str("url", url).Msg("anchor: the organisation's store cannot be reached; heads are anchored in memory, not durably")
		return anchor.NewMemStore()
	}
	org := anchorsql.New(exec, headstore.OrgStoreTable.Get(), runId)
	pctx, cancel := context.WithTimeout(ctx, anchorStoreTimeout)
	defer cancel()
	if err = org.Provision(pctx); err != nil {
		logger.Warn().Err(err).Str("url", url).Msg("anchor: the organisation's store refused its table; heads are anchored in memory, not durably")
		return anchor.NewMemStore()
	}
	logger.Info().Str("url", url).Str("table", headstore.OrgStoreTable.Get()).Msg("anchor: heads are anchored in the organisation's store")
	return org
}
