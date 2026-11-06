package widgets

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/console"
)

// =============================================================================
// console widget demo — a terminal screen with no process behind it (ADR-0359)
//
// The demo stands in for a program: it writes a banner that exercises colours,
// styles and wide glyphs, then answers typed keys as a cooked tty would —
// echo, Enter starts a new prompt, Backspace erases, Ctrl-C abandons the
// line. A real host connects a pseudo-terminal instead.
// =============================================================================

func init() {
	registry.Register(registry.Demo{
		Name:     "console",
		Category: "Text & code",
		Title:    icons.PhTerminal + " console",
		Stage:    [2]float32{900, 560},
		Kind:     registry.DemoKindUX,
		Description: "A terminal screen whose VT core (the vendored fux-vt) runs " +
			"in the client. Go writes a program's output into it and receives what " +
			"the program should be sent: keys and pastes encoded in the mode the " +
			"program asked for, focus reports, and the terminal's own replies, each " +
			"tagged so a recorder can tell them apart. Output is addressed by its " +
			"offset and kept until the client acknowledges it, so a hidden tab " +
			"loses nothing. No process runs here: the demo echoes what you type, " +
			"as a cooked tty would. Click the screen to focus it.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = newConsoleDemoState()
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoConsole(ids, state.(*consoleDemoState))
		},
		SourceFunc: demoConsole,
	})
}

type consoleDemoState struct {
	con  *console.Console
	line []rune
	esc  int // where the escape-sequence skipper in answer is
}

func newConsoleDemoState() *consoleDemoState {
	return &consoleDemoState{}
}

const consoleDemoPrompt = "\x1b[1;32mdemo\x1b[0m:\x1b[1;34m~\x1b[0m$ "

func consoleDemoBanner() string {
	var b strings.Builder
	b.WriteString("\x1b]2;console demo\x07")
	b.WriteString("\x1b[1mimzero2 console\x1b[0m — a VT core in the client, bytes in and out through Go\r\n\r\n")
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&b, "\x1b[48;5;%dm  ", i)
	}
	b.WriteString("\x1b[0m\r\n")
	for i := 16; i < 232; i += 6 {
		fmt.Fprintf(&b, "\x1b[48;5;%dm ", i)
	}
	b.WriteString("\x1b[0m\r\n")
	for i := 0; i < 48; i++ {
		fmt.Fprintf(&b, "\x1b[48;2;%d;%d;%dm ", 255-i*5, i*5, 128)
	}
	b.WriteString("\x1b[0m\r\n\r\n")
	b.WriteString("\x1b[1mbold\x1b[0m \x1b[2mdim\x1b[0m \x1b[4munderline\x1b[0m \x1b[9mstrike\x1b[0m \x1b[7minverse\x1b[0m ")
	b.WriteString("\x1b[31mred\x1b[0m \x1b[1;31mbright\x1b[0m \x1b[38;5;208m208\x1b[0m \x1b[38;2;80;200;255mtruecolor\x1b[0m\r\n")
	b.WriteString("wide: 漢字かなカナ 한국어   emoji: 👍 👍🏽 🇨🇭   combining: é ạ̈\r\n\r\n")
	b.WriteString("Type here; Enter, Backspace and Ctrl-C work as in a cooked tty.\r\n\r\n")
	b.WriteString(consoleDemoPrompt)
	return b.String()
}

func demoConsole(ids *c.WidgetIdStack, st *consoleDemoState) {
	if st.con == nil {
		// Built on the first frame, where the id stack is live (ADR-0267 W16).
		st.con = console.New(ids, "console-demo", console.Options{})
		_, _ = st.con.Write([]byte(consoleDemoBanner()))
	}
	ev := st.con.RenderFill(860, 480)
	st.answer(ev.ToProgram)
}

// The states of the escape-sequence skipper in answer.
const (
	escNone   = iota
	escStart  // after ESC
	escCsi    // CSI parameters, until a final byte
	escString // OSC, DCS or APC, until BEL or ST
	escOne    // one byte left: SS3's key, or ST's backslash
)

// answer plays a cooked tty for the bytes the console sent. Escape
// sequences (cursor keys, the terminal's replies) are skipped.
func (inst *consoleDemoState) answer(in []byte) {
	var out strings.Builder
	for _, b := range []rune(string(in)) {
		switch inst.esc {
		case escStart:
			switch b {
			case '[':
				inst.esc = escCsi
			case ']', 'P', '_':
				inst.esc = escString
			case 'O':
				inst.esc = escOne
			default:
				inst.esc = escNone
			}
			continue
		case escCsi:
			if b >= 0x40 && b <= 0x7e {
				inst.esc = escNone
			}
			continue
		case escString:
			switch b {
			case 0x07:
				inst.esc = escNone
			case 0x1b:
				inst.esc = escOne
			}
			continue
		case escOne:
			inst.esc = escNone
			continue
		}
		switch {
		case b == 0x1b:
			inst.esc = escStart
		case b == '\r':
			out.WriteString("\r\n")
			if len(inst.line) > 0 {
				fmt.Fprintf(&out, "you typed \x1b[1m%s\x1b[0m (%d characters)\r\n", string(inst.line), len(inst.line))
			}
			inst.line = inst.line[:0]
			out.WriteString(consoleDemoPrompt)
		case b == 0x7f || b == 0x08:
			if len(inst.line) > 0 {
				inst.line = inst.line[:len(inst.line)-1]
				out.WriteString("\b \b")
			}
		case b == 0x03:
			inst.line = inst.line[:0]
			out.WriteString("^C\r\n" + consoleDemoPrompt)
		case b < 0x20:
		default:
			inst.line = append(inst.line, b)
			out.WriteRune(b)
		}
	}
	if out.Len() > 0 {
		_, _ = inst.con.Write([]byte(out.String()))
	}
}
