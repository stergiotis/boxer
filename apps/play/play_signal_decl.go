package play

// play_signal_decl.go is the one place a panel-written signal is declared
// (ADR-0232 §SD9): its name, the type its writer encodes for, and what a query
// referencing it does before anything has written it.
//
// Four registrations read this table — the chrome's declared types, the
// empty-default rule, a tab's Writes list and the tab-marks expansion — where
// each used to carry its own copy of part of it. A panel that gains a signal
// adds one row here; a name that appears in a tab's Writes without a row is a
// build-time gap the declaration test catches.
//
// What stays on TabSpec is WHICH names a tab writes, because that genuinely
// varies per tab: `selection` is published by nine panes and belongs to none of
// them. What moves here is what a signal IS, which does not vary by writer.

// signalSeedE says what a referenced reserved signal does before its writer
// has run.
type signalSeedE uint8

const (
	// seedBlocks holds the Run until the panel writes: the numeric signals
	// have no safe empty literal, and their panels seed them on render, so a
	// query that references one is genuinely waiting for a value.
	seedBlocks signalSeedE = iota
	// seedEmpty lets the query run from the first frame against the empty
	// string, whose meaning — "nothing selected" — is a valid filter the
	// server accepts. Without it a query reading `{selection_country:String}`
	// is refused until the first click, and there is nothing else to fill it.
	seedEmpty
	// seedZero is seedEmpty for a numeric slot, where "" is not a literal the
	// server would accept. A zero pin beside an empty `gv_pin_id` is a valid
	// "no drop yet" (ADR-0231 §SD8).
	seedZero
	// seedEmptyArray is seedEmpty for an array slot: `[]`, the empty
	// selection, rather than a missing value.
	seedEmptyArray
	// seedEpoch is seedZero for a time slot: the epoch, which no step of a
	// field is at, so a query filtering on it returns nothing until the
	// pane has written the step on display (ADR-0250 §SD6).
	seedEpoch
)

// raw is the literal an unwritten signal with this seed resolves to, and
// whether it has one at all.
func (inst signalSeedE) raw() (raw string, ok bool) {
	switch inst {
	case seedEmpty:
		return "", true
	case seedZero:
		return "0", true
	case seedEmptyArray:
		return "[]", true
	case seedEpoch:
		return "1970-01-01 00:00:00.000", true
	}
	return "", false
}

// reservedSignal is one panel-written signal: the name a query references, the
// type its writer encodes for, and its seed. Owner names the tab that writes
// it where exactly one does, and is empty for the selection family, which
// every selecting pane publishes.
type reservedSignal struct {
	Name  SignalID
	Type  string
	Seed  signalSeedE
	Owner string
}

// reservedSignals is the declaration. Order is for reading only; every lookup
// goes through the map built below.
var reservedSignals = []reservedSignal{
	// The Map's viewport (ADR-0096 §SD6): the six slots its raster template
	// reads back, published once the view settles. They block rather than
	// seed — there is no viewport that means "anywhere".
	{Name: "vp_min_x", Type: "UInt32", Seed: seedBlocks, Owner: "map"},
	{Name: "vp_max_x", Type: "UInt32", Seed: seedBlocks, Owner: "map"},
	{Name: "vp_min_y", Type: "UInt32", Seed: seedBlocks, Owner: "map"},
	{Name: "vp_max_y", Type: "UInt32", Seed: seedBlocks, Owner: "map"},
	{Name: "vp_w", Type: "UInt32", Seed: seedBlocks, Owner: "map"},
	{Name: "vp_h", Type: "UInt32", Seed: seedBlocks, Owner: "map"},

	// The Timeline's extent (slice 5d), seeded by the panel on render.
	{Name: signalTimelineMin, Type: "DateTime64(3, 'UTC')", Seed: seedBlocks, Owner: "timeline"},
	{Name: signalTimelineMax, Type: "DateTime64(3, 'UTC')", Seed: seedBlocks, Owner: "timeline"},

	// The World's clicked country.
	{Name: signalSelectionCountry, Type: "String", Seed: seedEmpty, Owner: "world"},

	// The Graphview tab's gesture seam (ADR-0231 §SD8). Every one is seeded
	// when the tab first renders — strings empty, arrays empty, numbers zero,
	// the camera from its first fit — so a query referencing any of them runs
	// from the first frame. That is the difference from the Map's viewport: a
	// pin of zero beside an empty `gv_pin_id` is a valid "no drop yet", where
	// a viewport of zero would draw a raster of nowhere.
	{Name: signalGvHover, Type: "String", Seed: seedEmpty, Owner: "graphview"},
	{Name: signalGvSelection, Type: "Array(String)", Seed: seedEmptyArray, Owner: "graphview"},
	{Name: signalGvFocus, Type: "String", Seed: seedEmpty, Owner: "graphview"},
	{Name: signalGvContext, Type: "String", Seed: seedEmpty, Owner: "graphview"},
	{Name: signalGvEdgeSource, Type: "String", Seed: seedEmpty, Owner: "graphview"},
	{Name: signalGvEdgeTarget, Type: "String", Seed: seedEmpty, Owner: "graphview"},
	{Name: signalGvEdgeID, Type: "String", Seed: seedEmpty, Owner: "graphview"},
	{Name: signalGvPinID, Type: "String", Seed: seedEmpty, Owner: "graphview"},
	{Name: signalGvPinX, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvPinY, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvBgX, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvBgY, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvMinX, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvMaxX, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvMinY, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvMaxY, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvZoom, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvAuraHidden, Type: "Array(String)", Seed: seedEmptyArray, Owner: "graphview"},
	// The located forms (ADR-0231 §SD3), written only while some vertex
	// carried `lat`/`lon`; seeded like the rest, so a query may reference
	// them whether or not this result is geographic.
	{Name: signalGvPinLat, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvPinLon, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvMinLat, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvMaxLat, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvMinLon, Type: "Float64", Seed: seedZero, Owner: "graphview"},
	{Name: signalGvMaxLon, Type: "Float64", Seed: seedZero, Owner: "graphview"},

	// The Vector field pane's display time and settled view (ADR-0250 §SD6).
	// Seeded, for the Graphview's reason and against the Map's: the pane
	// learns of its CTE from a Run, so a buffer whose sink reads `vf_t` could
	// never run if the name blocked until the pane had written it. The seeds
	// select nothing — the epoch, an empty box — and the pane overwrites them
	// once its field is described.
	{Name: signalVfT, Type: "DateTime64(3, 'UTC')", Seed: seedEpoch, Owner: "vectorfield"},
	{Name: signalVfMinLat, Type: "Float64", Seed: seedZero, Owner: "vectorfield"},
	{Name: signalVfMaxLat, Type: "Float64", Seed: seedZero, Owner: "vectorfield"},
	{Name: signalVfMinLon, Type: "Float64", Seed: seedZero, Owner: "vectorfield"},
	{Name: signalVfMaxLon, Type: "Float64", Seed: seedZero, Owner: "vectorfield"},

	// The selection family (slice 5b). No owner: the row cursor is written by
	// every pane whose rows ARE result rows, and the three companions are
	// stamped by the dispatcher on its behalf (selectionStamper) or published
	// directly by the panels whose rows are not (the graph tabs, ADR-0129
	// §SD4).
	{Name: signalSelection, Type: "Int64", Seed: seedBlocks},
	{Name: signalSelectionNode, Type: "String", Seed: seedEmpty},
	{Name: signalSelectionID, Type: "UInt64", Seed: seedBlocks},
	{Name: signalSelectionKey, Type: "String", Seed: seedEmpty},
}

// reservedSignalIndex is the declaration by name, built once.
var reservedSignalIndex = func() map[SignalID]reservedSignal {
	out := make(map[SignalID]reservedSignal, len(reservedSignals))
	for _, s := range reservedSignals {
		out[s.Name] = s
	}
	return out
}()

// signalsWrittenBy lists the signals a tab owns, in declaration order — what a
// TabSpec puts in its Writes instead of restating the names. A tab that also
// publishes a shared name (the selection family) lists that one itself.
func signalsWrittenBy(tab string) (out []SignalID) {
	for _, s := range reservedSignals {
		if s.Owner == tab {
			out = append(out, s.Name)
		}
	}
	return
}

// reservedSignalTypes maps the panel-written signal names to the types their
// writers encode for. The chrome types rows the buffer does not declare with
// it, cross-checks buffer declarations for conflicts against it, and reads the
// seed below off the same table.
func reservedSignalTypes() (out map[string]string) {
	out = make(map[string]string, len(reservedSignals))
	for _, s := range reservedSignals {
		out[string(s.Name)] = s.Type
	}
	return
}

// signalSeedRaw is the literal a referenced reserved signal resolves to when
// nothing has written it yet, and whether it has one. An unreserved name never
// does: it is an ordinary signal and the Run gate owns it.
//
// The literal has to suit the declared type — "" for a String, "0" for a
// number, "[]" for an array — because it reaches the server as the param's
// value, where a String's "" would be rejected for a Float64 slot.
func signalSeedRaw(name string) (raw string, ok bool) {
	return reservedSignalIndex[SignalID(name)].Seed.raw()
}

// signalHasSeed reports whether a referenced reserved signal runs from the
// first frame rather than gating the Run.
func signalHasSeed(name string) bool {
	_, ok := signalSeedRaw(name)
	return ok
}
