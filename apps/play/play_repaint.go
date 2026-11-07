package play

// The repaint rule (ADR-0062): under reactive cadence a frame is drawn on
// input or on request, so the window asks for one only while something in
// it moves by itself and stops asking once it settles. Under the default
// continuous cadence the request changes nothing.

// anyRunning says whether something in the window is moving by itself: a
// run or a lane's fetch, a request to the host in flight, a job landing
// off the frame, a debounce not yet due, or Live, which keeps the window
// moving by design. The screenshot script counts until its artifact lands,
// so a capture stays continuous (ADR-0062 §SD7).
func (inst *PlayApp) anyRunning() bool {
	if inst.graph.MainLoading() || inst.requestRun || inst.liveMain || inst.frameProgress.fresh {
		return true
	}
	if (inst.AutoRun && !inst.didAutoRun) || (inst.ScreenshotPath != "" && inst.shotPhase != 3) {
		return true
	}
	if inst.sql != inst.formattedFor {
		return true // the preview's debounce lands on a later frame
	}
	if inst.projector != nil {
		switch inst.projector.Snapshot().status {
		case projectorStatusExtracting, projectorStatusRunning, projectorStatusCancelling:
			return true
		}
	}
	if inst.writeRun.running() || (inst.identityJob != nil && !inst.identityJob.isDone()) {
		return true
	}
	return inst.anyRequestBusy() || inst.anyLaneLoading()
}

// anyRequestBusy says a round-trip to the host — an open, a save, the file
// picker — is in flight.
func (inst *PlayApp) anyRequestBusy() (busy bool) {
	inst.openPlayMu.Lock()
	busy = inst.openPlayBusy
	inst.openPlayMu.Unlock()
	if busy {
		return
	}
	inst.openTallyMu.Lock()
	busy = inst.openTallyBusy
	inst.openTallyMu.Unlock()
	if busy {
		return
	}
	inst.saveAppletMu.Lock()
	busy = inst.saveAppletBusy
	inst.saveAppletMu.Unlock()
	if busy {
		return
	}
	inst.pickMu.Lock()
	busy = inst.pickInFlight
	inst.pickMu.Unlock()
	return
}

// anyLaneLoading says a lane the window or one of its panels owns has a
// fetch in flight: the same lanes Close tears down.
func (inst *PlayApp) anyLaneLoading() bool {
	lanes := []*nodeLane{inst.intermediateLane, inst.seriesScoresLane, inst.seriesSpansLane, inst.seriesLabelsLane}
	if inst.mapDriver != nil {
		lanes = append(lanes, inst.mapDriver.lane)
	}
	if inst.timeline != nil {
		lanes = append(lanes, inst.timeline.bandsLane)
	}
	if inst.kanbanDriver != nil {
		lanes = append(lanes, inst.kanbanDriver.lanesLane)
	}
	if inst.chatDriver != nil {
		lanes = append(lanes, inst.chatDriver.participantsLane, inst.chatDriver.reactionsLane)
	}
	if inst.netSource != nil {
		lanes = append(lanes, inst.netSource.edgesLane, inst.netSource.verticesLane, inst.netSource.optsLane)
	}
	if inst.vectorFieldDriver != nil {
		lanes = append(lanes, inst.vectorFieldDriver.probeLane, inst.vectorFieldDriver.optsLane, inst.vectorFieldDriver.sitesLane)
	}
	if inst.sankeyDriver != nil {
		lanes = append(lanes, inst.sankeyDriver.flowsLane, inst.sankeyDriver.nodesLane)
	}
	if inst.tsCollisions != nil {
		lanes = append(lanes, inst.tsCollisions.lane)
	}
	if inst.vocab != nil {
		lanes = append(lanes, inst.vocab.lane)
	}
	if inst.diag != nil {
		lanes = append(lanes, inst.diag.lane, inst.diag.analyzeLane)
	}
	for _, l := range inst.boundLanes {
		lanes = append(lanes, l)
	}
	for _, l := range inst.enumLanes {
		lanes = append(lanes, l)
	}
	if inst.flow != nil {
		for _, l := range inst.flow.lanes {
			lanes = append(lanes, l)
		}
	}
	for _, l := range lanes {
		if l != nil && l.peek().loading {
			return true
		}
	}
	return false
}
