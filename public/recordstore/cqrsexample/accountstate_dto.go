package cqrsexample

// AccountState is the snapshot component: the folded write-model state
// as of sequence AsOf, written under the aggregate's sibling snapshot
// key ("acct/<n>/snap", latest-wins via Latest). Rehydration restores it
// and replays only the events after AsOf. See opened_dto.go for the
// component-file layout rationale.
type AccountState struct {
	_       struct{} `kind:"acctState"`
	ID      string   `lw:",id"`
	Owner   string   `lw:"ledgerSnapOwner,snapOwner"`
	Balance uint64   `lw:"ledgerSnapBalance,snapBalance,unit"`
	Closed  bool     `lw:"ledgerSnapClosed,snapClosed"`
	AsOf    uint64   `lw:"ledgerSnapAsOf,snapAsOf,unit"`
}
