package trail

// DropHeldForTest drops the oldest held batches until at most keep remain,
// as the backlog cap would, and returns how many batches it dropped. For
// tests that need a gap without holding MaxBacklogRows rows.
func (inst *Recorder) DropHeldForTest(keep int) (dropped int) {
	inst.flushMu.Lock()
	defer inst.flushMu.Unlock()
	for len(inst.backlog) > keep {
		b := inst.backlog[0]
		b.store.Close()
		inst.backlog = inst.backlog[1:]
		inst.gap.add(b)
		inst.dropped.Add(uint64(b.rows))
		dropped++
	}
	return
}
