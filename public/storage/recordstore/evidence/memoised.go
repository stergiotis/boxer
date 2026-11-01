package evidence

// Memoised wraps rows so that each row's canonical item, and each of its
// vault items, is computed once however many laws read it: Verify and
// VerifyHeads over the same rows read every item twice, and a store's
// item is a scratch re-entry and a drain. The wrapping is the caller's
// choice because a row is otherwise a live view of its entity — a test
// that changes the entity and verifies again relies on that — and a
// memoised row is the entity as it was when first read. A memoised row
// answers Head as its row does. The rows are read by one goroutine at a
// time.
func Memoised(rows []RowI) (out []RowI) {
	out = make([]RowI, len(rows))
	for i, r := range rows {
		out[i] = &memoRow{RowI: r}
	}
	return
}

type memoRow struct {
	RowI
	item     []byte
	itemErr  error
	itemDone bool
	vault    map[string]memoItem
}

type memoItem struct {
	item []byte
	err  error
}

var _ HeadRowI = (*memoRow)(nil)

func (inst *memoRow) CanonicalItem() ([]byte, error) {
	if !inst.itemDone {
		inst.item, inst.itemErr = inst.RowI.CanonicalItem()
		inst.itemDone = true
	}
	return inst.item, inst.itemErr
}

func (inst *memoRow) VaultItem(component string) ([]byte, error) {
	if m, done := inst.vault[component]; done {
		return m.item, m.err
	}
	item, err := inst.RowI.VaultItem(component)
	if inst.vault == nil {
		inst.vault = make(map[string]memoItem)
	}
	inst.vault[component] = memoItem{item: item, err: err}
	return item, err
}

func (inst *memoRow) Head() (ordinal uint64, digest string, ok bool) {
	if h, is := inst.RowI.(HeadRowI); is {
		return h.Head()
	}
	return
}
