package bindings

import (
	"encoding/binary"
	"iter"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
)

// ConsoleRowKindE says what a console row carries. The values are the
// client's (rust/imzero2/src/imzero2/console.rs, module kind).
type ConsoleRowKindE uint8

const (
	// ConsoleRowKey holds bytes a key produced, for the program.
	ConsoleRowKey ConsoleRowKindE = 1
	// ConsoleRowPaste holds a paste, framed as the program asked, for the program.
	ConsoleRowPaste ConsoleRowKindE = 2
	// ConsoleRowFocus holds a focus report, for the program.
	ConsoleRowFocus ConsoleRowKindE = 3
	// ConsoleRowReply holds an answer the terminal produced to the program's
	// query, for the program.
	ConsoleRowReply ConsoleRowKindE = 4
	// ConsoleRowSize says the grid's size changed; see ConsoleRow.Size.
	ConsoleRowSize ConsoleRowKindE = 16
	// ConsoleRowAck says how many bytes of the stream the terminal has read;
	// see ConsoleRow.Offsets.
	ConsoleRowAck ConsoleRowKindE = 17
	// ConsoleRowTitle holds the window title the program set, as sent.
	ConsoleRowTitle ConsoleRowKindE = 18
	// ConsoleRowBell says the program rang the bell.
	ConsoleRowBell ConsoleRowKindE = 19
	// ConsoleRowClipboard holds an OSC 52 set request: the selection
	// parameter, a NUL, then the data as sent (normally base64).
	ConsoleRowClipboard ConsoleRowKindE = 20
	// ConsoleRowGap says bytes were skipped because the terminal had read
	// fewer than the offset it was fed at; see ConsoleRow.Offsets.
	ConsoleRowGap ConsoleRowKindE = 21
	// ConsoleRowNotification holds a desktop notification (OSC 9, 777 or 99), as sent.
	ConsoleRowNotification ConsoleRowKindE = 22
	// ConsoleRowCommandStarted says a shell command began (OSC 133 C).
	ConsoleRowCommandStarted ConsoleRowKindE = 23
	// ConsoleRowCommandEnded says a shell command ended (OSC 133 D); see
	// ConsoleRow.Status.
	ConsoleRowCommandEnded ConsoleRowKindE = 24
	// ConsoleRowScreen holds the screen after a change, at most ten times a
	// second; see ConsoleRow.Screen.
	ConsoleRowScreen ConsoleRowKindE = 25
)

// ConsoleScreen is the screen a ConsoleRowScreen row reports.
type ConsoleScreen struct {
	// Offset is how many bytes of the stream the screen shows.
	Offset     uint64
	Cols, Rows uint16
	// CursorCol and CursorRow are the cursor's cell, from zero; meaningful
	// when CursorShown.
	CursorCol, CursorRow uint16
	CursorShown          bool
	// Text is the visible rows, trimmed on the right, a newline between.
	Text string
}

// ConsoleRow is one row a console widget produced last frame.
type ConsoleRow struct {
	Kind ConsoleRowKindE
	// Payload is valid until the next Sync; copy it to keep it.
	Payload []byte
}

// ForProgram reports whether the row's payload is bytes for the program:
// a key, a paste, a focus report or a reply.
func (inst ConsoleRow) ForProgram() bool {
	switch inst.Kind {
	case ConsoleRowKey, ConsoleRowPaste, ConsoleRowFocus, ConsoleRowReply:
		return true
	}
	return false
}

// Size returns the columns and rows of a ConsoleRowSize row.
func (inst ConsoleRow) Size() (cols uint16, rows uint16, ok bool) {
	if inst.Kind != ConsoleRowSize || len(inst.Payload) != 4 {
		return
	}
	return binary.LittleEndian.Uint16(inst.Payload), binary.LittleEndian.Uint16(inst.Payload[2:]), true
}

// Offsets returns the stream offsets a row carries: for ConsoleRowAck the
// bytes read (in from), for ConsoleRowGap the bytes read before and the
// offset fed at.
func (inst ConsoleRow) Offsets() (from uint64, to uint64, ok bool) {
	switch {
	case inst.Kind == ConsoleRowAck && len(inst.Payload) == 8:
		return binary.LittleEndian.Uint64(inst.Payload), 0, true
	case inst.Kind == ConsoleRowGap && len(inst.Payload) == 16:
		return binary.LittleEndian.Uint64(inst.Payload), binary.LittleEndian.Uint64(inst.Payload[8:]), true
	}
	return
}

// Screen returns the screen of a ConsoleRowScreen row; the text is copied.
func (inst ConsoleRow) Screen() (s ConsoleScreen, ok bool) {
	p := inst.Payload
	if inst.Kind != ConsoleRowScreen || len(p) < 17 {
		return
	}
	s = ConsoleScreen{
		Offset:      binary.LittleEndian.Uint64(p),
		Cols:        binary.LittleEndian.Uint16(p[8:]),
		Rows:        binary.LittleEndian.Uint16(p[10:]),
		CursorCol:   binary.LittleEndian.Uint16(p[12:]),
		CursorRow:   binary.LittleEndian.Uint16(p[14:]),
		CursorShown: p[16]&1 != 0,
		Text:        string(p[17:]),
	}
	ok = true
	return
}

// Status returns the exit status of a ConsoleRowCommandEnded row, if the
// shell gave one.
func (inst ConsoleRow) Status() (status int32, ok bool) {
	if inst.Kind != ConsoleRowCommandEnded || len(inst.Payload) != 4 {
		return
	}
	return int32(binary.LittleEndian.Uint32(inst.Payload)), true
}

// GetConsoleRows returns last frame's rows of the console behind the
// handle, in the order the widget produced them; nil when it did not render
// last frame. The slice and the payloads are valid until the next Sync.
func (inst *StateManager) GetConsoleRows(h widgethandle.WidgetHandle) []ConsoleRow {
	return inst.r29Console[h.Resolve()]
}

// collectConsoleRows rebuilds r29Console from one fetch, consuming data fully.
func (inst *StateManager) collectConsoleRows(ids []uint64, kinds []uint8, lens []uint32, data iter.Seq[uint8]) {
	total := 0
	for _, l := range lens {
		total += int(l)
	}
	buf := make([]byte, 0, total)
	for b := range data {
		buf = append(buf, b)
	}
	for id, rows := range inst.r29Console {
		inst.r29Console[id] = rows[:0]
	}
	at := 0
	for i, id := range ids {
		if i >= len(kinds) || i >= len(lens) {
			break
		}
		end := at + int(lens[i])
		if end > len(buf) {
			break
		}
		inst.r29Console[id] = append(inst.r29Console[id], ConsoleRow{
			Kind:    ConsoleRowKindE(kinds[i]),
			Payload: buf[at:end:end],
		})
		at = end
	}
	for id, rows := range inst.r29Console {
		if len(rows) == 0 {
			delete(inst.r29Console, id)
		}
	}
}

// ScriptConsoleRows sets the rows GetConsoleRows returns for a widget id,
// for headless tests; ScriptReset clears them.
func (inst *StateManager) ScriptConsoleRows(id uint64, rows []ConsoleRow) {
	inst.r29Console[id] = rows
}
