package console

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// screenId derives the id Render gives the console's screen under scopeKey.
func screenId(ids *c.WidgetIdStack, scopeKey string) (id uint64) {
	for range c.IdScope(ids.PrepareStr(scopeKey)) {
		id = ids.PrepareStr("screen").Derive()
	}
	return
}

func ack(n uint64) c.ConsoleRow {
	return c.ConsoleRow{Kind: c.ConsoleRowAck, Payload: binary.LittleEndian.AppendUint64(nil, n)}
}

func size(cols, rows uint16) c.ConsoleRow {
	p := binary.LittleEndian.AppendUint16(nil, cols)
	return c.ConsoleRow{Kind: c.ConsoleRowSize, Payload: binary.LittleEndian.AppendUint16(p, rows)}
}

func TestAFreshConsoleRendersUnderTheDiscardChannel(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	con := New(ids, "", Options{})
	ev := con.Render(0, 0)
	require.Empty(t, ev.ToProgram)
	require.False(t, ev.Resized)
	ev = con.RenderFill(640, 400)
	require.Empty(t, ev.Records)
}

func TestOutputIsKeptUntilAcknowledgedAndInputComesBack(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()
	con := New(ids, "shell", Options{})
	id := screenId(ids, "shell")
	sm.ScriptReset()

	_, err := con.Write([]byte("hello"))
	require.NoError(t, err)
	con.Render(400, 300)
	require.Equal(t, 5, con.Pending(), "unacknowledged output stays")

	sm.ScriptReset()
	sm.ScriptConsoleRows(id, []c.ConsoleRow{
		size(80, 24),
		ack(3),
		{Kind: c.ConsoleRowKey, Payload: []byte("ls\r")},
		{Kind: c.ConsoleRowReply, Payload: []byte("\x1b[?62c")},
		{Kind: c.ConsoleRowTitle, Payload: []byte("~/src")},
	})
	ev := con.Render(400, 300)
	require.Equal(t, 2, con.Pending(), "the acknowledged three bytes are dropped")
	require.Equal(t, []byte("ls\r\x1b[?62c"), ev.ToProgram)
	require.True(t, ev.Resized)
	require.Equal(t, uint16(80), ev.Cols)
	require.Equal(t, uint16(24), ev.Rows)
	require.True(t, ev.TitleChanged)
	require.Equal(t, "~/src", ev.Title)
	require.Len(t, ev.Records, 5)

	sm.ScriptReset()
	sm.ScriptConsoleRows(id, []c.ConsoleRow{size(80, 24), ack(5)})
	ev = con.Render(400, 300)
	require.False(t, ev.Resized, "the same size again is no resize")
	require.Equal(t, 0, con.Pending())
	require.Empty(t, ev.ToProgram)
}

func TestWriteBlocksUntilTheClientCatchesUp(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()
	con := New(ids, "shell", Options{MaxPending: 4})
	id := screenId(ids, "shell")
	sm.ScriptReset()

	_, err := con.Write([]byte("abcd"))
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		_, _ = con.Write([]byte("ef"))
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Write did not block past MaxPending")
	case <-time.After(50 * time.Millisecond):
	}
	sm.ScriptConsoleRows(id, []c.ConsoleRow{ack(4)})
	con.Render(0, 0)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Write stayed blocked after the acknowledgement")
	}
	require.Equal(t, 2, con.Pending())
}

func TestCloseReleasesABlockedWrite(t *testing.T) {
	ids := c.NewWidgetIdStack()
	con := New(ids, "shell", Options{MaxPending: 1})
	_, err := con.Write([]byte("x"))
	require.NoError(t, err)
	errs := make(chan error, 1)
	go func() {
		_, err := con.Write([]byte("y"))
		errs <- err
	}()
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, con.Close())
	require.ErrorIs(t, <-errs, ErrClosed)
}

func TestRowAccessorsDecodeTheirPayloads(t *testing.T) {
	cols, rows, ok := size(120, 40).Size()
	require.True(t, ok)
	require.Equal(t, [2]uint16{120, 40}, [2]uint16{cols, rows})
	n, _, ok := ack(77).Offsets()
	require.True(t, ok)
	require.Equal(t, uint64(77), n)
	status, ok := c.ConsoleRow{Kind: c.ConsoleRowCommandEnded, Payload: binary.LittleEndian.AppendUint32(nil, 2)}.Status()
	require.True(t, ok)
	require.Equal(t, int32(2), status)
	_, ok = c.ConsoleRow{Kind: c.ConsoleRowCommandEnded}.Status()
	require.False(t, ok, "a shell may give no status")
}

func TestTheLastReportedScreenIsKept(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()
	con := New(ids, "shell", Options{})
	id := screenId(ids, "shell")
	sm.ScriptReset()
	_, ok := con.Screen()
	require.False(t, ok, "no screen before the first report")

	p := binary.LittleEndian.AppendUint64(nil, 12)
	for _, v := range []uint16{80, 24, 2, 1} {
		p = binary.LittleEndian.AppendUint16(p, v)
	}
	p = append(append(p, 1), "$ ls\n$"...)
	sm.ScriptConsoleRows(id, []c.ConsoleRow{size(80, 24), {Kind: c.ConsoleRowScreen, Payload: p}})
	ev := con.Render(400, 300)
	require.True(t, ev.ScreenChanged)
	sc, ok := con.Screen()
	require.True(t, ok)
	require.Equal(t, c.ConsoleScreen{Offset: 12, Cols: 80, Rows: 24, CursorCol: 2, CursorRow: 1, CursorShown: true, Text: "$ ls\n$"}, sc)

	sm.ScriptReset()
	sm.ScriptConsoleRows(id, []c.ConsoleRow{ack(12)})
	ev = con.Render(400, 300)
	require.False(t, ev.ScreenChanged)
	sc, ok = con.Screen()
	require.True(t, ok)
	require.Equal(t, "$ ls\n$", sc.Text, "the screen stands until the next report")
}
