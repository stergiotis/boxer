package headstore

import "github.com/stergiotis/boxer/public/config/env"

// The organisation's anchor store (ADR-0314 §SD2, update of 2026-10-13):
// a ClickHouse server outside any person's control, holding the heads in
// one table through anchorsql. Unset, the host anchors in memory and its
// receipts say they are not durable. Setting the endpoint is the
// deployment's statement that the server is the organisation's: the host
// marks every receipt from it durable and cannot check the claim.
var (
	OrgStoreURL = env.NewString(env.Spec{
		Name:        "BOXER_ANCHOR_STORE_URL",
		Description: "HTTP endpoint of the organisation's ClickHouse anchor store (ADR-0314 §SD2); unset anchors heads in memory, not durably",
		Category:    env.CategoryDatabase,
	})
	OrgStoreUser = env.NewString(env.Spec{
		Name:        "BOXER_ANCHOR_STORE_USER",
		Default:     "default",
		Description: "user for BOXER_ANCHOR_STORE_URL",
		Category:    env.CategoryDatabase,
	})
	OrgStorePassword = env.NewString(env.Spec{
		Name:        "BOXER_ANCHOR_STORE_PASSWORD",
		Description: "password for BOXER_ANCHOR_STORE_URL",
		Category:    env.CategoryDatabase,
		Sensitive:   true,
	})
	OrgStoreTable = env.NewString(env.Spec{
		Name:        "BOXER_ANCHOR_STORE_TABLE",
		Default:     "boxer.anchorhead",
		Description: "database-qualified table the anchor store holds the heads in; created where absent",
		Category:    env.CategoryDatabase,
	})
)
